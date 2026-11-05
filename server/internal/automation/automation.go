// Package automation runs project rules: when something happens to an issue, or
// on a schedule, and the conditions hold, apply these actions (ADR-0020). Actions
// are the tracker's own operations, called through the same services and policy as
// a person's request, with the rights of the person who saved the rule. Rules
// cannot reach outside Lumen.
package automation

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // the release image has no zoneinfo

	"github.com/villetacore/lumen/server/internal/domain"
	"github.com/villetacore/lumen/server/internal/service"
)

// Trigger types.
const (
	TriggerIssueCreated  = "issue_created"
	TriggerStatusChanged = "status_changed"
	TriggerFieldChanged  = "field_changed"
	TriggerCommentAdded  = "comment_added"
	TriggerSchedule      = "schedule"
)

// Action types.
const (
	ActionSetStatus      = "set_status"
	ActionSetPriority    = "set_priority"
	ActionSetAssignee    = "set_assignee"
	ActionAddLabel       = "add_label"
	ActionRemoveLabel    = "remove_label"
	ActionSetDue         = "set_due"
	ActionAddComment     = "add_comment"
	ActionNotify         = "notify"
	ActionCreateSubissue = "create_subissue"
)

// Schedule fires at a wall-clock time on chosen weekdays (1 Monday … 7 Sunday;
// none means every day) in a named time zone.
type Schedule struct {
	At       string `json:"at"` // HH:MM
	Weekdays []int  `json:"weekdays,omitempty"`
	TZ       string `json:"tz"`
}

// Trigger says when a rule is considered.
type Trigger struct {
	Type     string    `json:"type"`
	From     string    `json:"from,omitempty"`  // status_changed: previous status
	To       string    `json:"to,omitempty"`    // status_changed, field_changed: new value
	Field    string    `json:"field,omitempty"` // field_changed
	Schedule *Schedule `json:"schedule,omitempty"`
}

// Condition narrows the issues a rule applies to; all must hold.
type Condition struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value string `json:"value,omitempty"`
}

// Action is one operation on the issue.
type Action struct {
	Type  string `json:"type"`
	Value string `json:"value,omitempty"`
}

// Definition is what a person writes; it is sealed as one value.
type Definition struct {
	Name       string      `json:"name"`
	Trigger    Trigger     `json:"trigger"`
	Conditions []Condition `json:"conditions"`
	Actions    []Action    `json:"actions"`
}

// Rule is a stored automation.
type Rule struct {
	Definition
	ID        string `json:"id"`
	Project   string `json:"project"`
	Enabled   bool   `json:"enabled"`
	Owner     string `json:"owner"`
	OwnerID   string `json:"-"`
	NextRun   string `json:"next_run,omitempty"`
	Revision  int64  `json:"revision"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`

	afterEvent int64
}

// Run statuses.
const (
	StatusRunning   = "running"
	StatusDone      = "done"
	StatusFailed    = "failed"
	StatusBlocked   = "blocked"   // the owner has no write access any more
	StatusThrottled = "throttled" // the fuse against rule loops tripped
	StatusUnknown   = "unknown"   // interrupted by a restart; not retried
)

// Run is one row of the log. Issue is empty for the summary of a scheduled run.
type Run struct {
	ID         int64  `json:"id"`
	RuleID     string `json:"rule_id"`
	RuleName   string `json:"rule_name,omitempty"`
	Project    string `json:"project"`
	Occurrence string `json:"occurrence"`
	Issue      string `json:"issue,omitempty"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
	Actions    int    `json:"actions"`
	Matched    int    `json:"matched"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at,omitempty"`
}

const (
	maxConditions = 20
	maxActions    = 20
	maxValue      = 4000
)

var (
	clockRe = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)
	// Usernames only: a rule cannot be made to hide markup in a recipient.
	userRe = regexp.MustCompile(`^[\p{L}\p{N}_][\p{L}\p{N}_.@-]*$`)
)

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", service.ErrInvalid, fmt.Sprintf(format, args...))
}

var changeableFields = map[string]bool{
	"assignee": true, "priority": true, "labels": true, "sprint": true,
	"due": true, "issue_type": true, "epic": true, "parent": true,
}

// Validate checks a definition against its project before it is saved, so a
// wrong status is an error in the form and not a failure at 3 a.m.
func (d *Definition) Validate(p *domain.TrackerProject) error {
	d.Name = strings.TrimSpace(d.Name)
	if d.Name == "" || len([]rune(d.Name)) > 200 {
		return invalid("у правила должно быть название до 200 символов")
	}
	status := func(id string) error {
		if id == "" {
			return nil
		}
		if _, ok := p.Status(id); !ok {
			return invalid("в проекте %s нет статуса %q", p.Key, id)
		}
		return nil
	}
	t := &d.Trigger
	switch t.Type {
	case TriggerIssueCreated, TriggerCommentAdded:
	case TriggerStatusChanged:
		if err := status(t.From); err != nil {
			return err
		}
		if err := status(t.To); err != nil {
			return err
		}
	case TriggerFieldChanged:
		if !changeableFields[t.Field] {
			return invalid("за полем %q следить нельзя", t.Field)
		}
	case TriggerSchedule:
		if t.Schedule == nil {
			return invalid("расписание не задано")
		}
		if !clockRe.MatchString(t.Schedule.At) {
			return invalid("время расписания — в виде ЧЧ:ММ")
		}
		if _, err := time.LoadLocation(t.Schedule.TZ); err != nil || t.Schedule.TZ == "" {
			return invalid("неизвестный часовой пояс %q", t.Schedule.TZ)
		}
		for _, w := range t.Schedule.Weekdays {
			if w < 1 || w > 7 {
				return invalid("день недели — от 1 до 7")
			}
		}
	default:
		return invalid("неизвестный триггер %q", t.Type)
	}
	if t.Type != TriggerSchedule {
		t.Schedule = nil
	}

	if len(d.Conditions) > maxConditions {
		return invalid("условий — не больше %d", maxConditions)
	}
	for _, c := range d.Conditions {
		if err := c.validate(p); err != nil {
			return err
		}
	}
	if len(d.Actions) == 0 {
		return invalid("добавьте хотя бы одно действие")
	}
	if len(d.Actions) > maxActions {
		return invalid("действий — не больше %d", maxActions)
	}
	for _, a := range d.Actions {
		if err := a.validate(p); err != nil {
			return err
		}
	}
	return nil
}

func (c Condition) validate(p *domain.TrackerProject) error {
	if len(c.Value) > maxValue {
		return invalid("слишком длинное значение условия")
	}
	switch c.Field {
	case "due":
		switch c.Op {
		case "overdue", "empty", "not_empty":
		case "within":
			if n, err := strconv.Atoi(c.Value); err != nil || n < 0 || n > 3650 {
				return invalid("срок — целое число дней")
			}
		default:
			return invalid("для срока нет условия %q", c.Op)
		}
		return nil
	case "status":
		if _, ok := p.Status(c.Value); !ok && (c.Op == "is" || c.Op == "is_not") {
			return invalid("в проекте %s нет статуса %q", p.Key, c.Value)
		}
	case "category", "priority", "assignee", "issue_type", "label", "sprint", "epic", "parent":
	default:
		return invalid("неизвестное поле условия %q", c.Field)
	}
	switch c.Op {
	case "is", "is_not":
		if c.Value == "" {
			return invalid("у условия нет значения")
		}
	case "empty", "not_empty":
	default:
		return invalid("неизвестное условие %q", c.Op)
	}
	return nil
}

func (a Action) validate(p *domain.TrackerProject) error {
	if len(a.Value) > maxValue {
		return invalid("слишком длинное значение действия")
	}
	v := strings.TrimSpace(a.Value)
	switch a.Type {
	case ActionSetStatus:
		if _, ok := p.Status(v); !ok {
			return invalid("в проекте %s нет статуса %q", p.Key, v)
		}
	case ActionSetPriority:
		switch v {
		case "", "highest", "high", "medium", "low", "lowest":
		default:
			return invalid("неизвестный приоритет %q", v)
		}
	case ActionSetAssignee:
		if v != "" && v != "@reporter" && !userRe.MatchString(v) {
			return invalid("исполнитель — логин или @reporter")
		}
	case ActionAddLabel, ActionRemoveLabel:
		if v == "" || strings.ContainsAny(v, ",\n") {
			return invalid("метка — одно слово или фраза без запятых")
		}
	case ActionSetDue:
		if v == "" {
			return nil
		}
		if n, err := strconv.Atoi(v); err != nil || n < 0 || n > 3650 {
			return invalid("срок — число дней от сегодня")
		}
	case ActionAddComment, ActionCreateSubissue:
		if v == "" {
			return invalid("пустой текст действия")
		}
	case ActionNotify:
		switch v {
		case "assignee", "reporter", "watchers":
		default:
			name, ok := strings.CutPrefix(v, "@")
			if !ok || !userRe.MatchString(name) {
				return invalid("кому сообщить: assignee, reporter, watchers или @логин")
			}
		}
	default:
		return invalid("неизвестное действие %q", a.Type)
	}
	return nil
}

// Next is the first firing strictly after t.
func (s *Schedule) Next(t time.Time) (time.Time, error) {
	loc, err := time.LoadLocation(s.TZ)
	if err != nil {
		return time.Time{}, err
	}
	at, err := time.Parse("15:04", s.At)
	if err != nil {
		return time.Time{}, err
	}
	days := map[int]bool{}
	for _, w := range s.Weekdays {
		days[w] = true
	}
	local := t.In(loc)
	for i := 0; i <= 8; i++ {
		// time.Date moves a wall-clock time a DST change skips forward, and picks
		// one of the two a change repeats, so a day never fires twice.
		c := time.Date(local.Year(), local.Month(), local.Day()+i, at.Hour(), at.Minute(), 0, 0, loc)
		wd := int(c.Weekday())
		if wd == 0 {
			wd = 7
		}
		if c.After(t) && (len(days) == 0 || days[wd]) {
			return c, nil
		}
	}
	return time.Time{}, fmt.Errorf("schedule has no next firing")
}

// location is where "today" is for a rule's date conditions and actions.
func (d *Definition) location() *time.Location {
	if s := d.Trigger.Schedule; s != nil {
		if loc, err := time.LoadLocation(s.TZ); err == nil {
			return loc
		}
	}
	return time.Local
}

// Matches reports whether all conditions hold for the issue on day today.
func Matches(conds []Condition, is *domain.Issue, today time.Time) bool {
	for _, c := range conds {
		if !c.matches(is, today) {
			return false
		}
	}
	return true
}

func (c Condition) matches(is *domain.Issue, today time.Time) bool {
	if c.Field == "due" {
		return c.matchDue(is, today)
	}
	if c.Field == "label" {
		has := false
		for _, l := range is.Labels {
			has = has || l == c.Value
		}
		switch c.Op {
		case "is":
			return has
		case "is_not":
			return !has
		case "empty":
			return len(is.Labels) == 0
		case "not_empty":
			return len(is.Labels) > 0
		}
		return false
	}
	var v string
	switch c.Field {
	case "status":
		v = is.Status
	case "category":
		v = string(is.StatusCategory)
	case "priority":
		v = is.Priority
	case "assignee":
		v = is.Assignee
	case "issue_type":
		v = is.Type
	case "sprint":
		v = is.Sprint
	case "epic":
		v = is.Epic
	case "parent":
		v = is.Parent
	}
	switch c.Op {
	case "is":
		return v == c.Value
	case "is_not":
		return v != c.Value
	case "empty":
		return v == ""
	case "not_empty":
		return v != ""
	}
	return false
}

func (c Condition) matchDue(is *domain.Issue, today time.Time) bool {
	switch c.Op {
	case "empty":
		return is.Due.IsZero()
	case "not_empty":
		return !is.Due.IsZero()
	}
	// A finished issue is not late, and it is not coming up either.
	if is.Due.IsZero() || is.StatusCategory == domain.CategoryDone {
		return false
	}
	due := is.Due.String()[:10]
	day := today.Format("2006-01-02")
	switch c.Op {
	case "overdue":
		return due < day
	case "within":
		n, err := strconv.Atoi(c.Value)
		return err == nil && due >= day && due <= today.AddDate(0, 0, n).Format("2006-01-02")
	}
	return false
}

// expand fills a comment or title from the issue. It is plain substitution, so a
// rule has no way to run anything.
func expand(text string, is *domain.Issue) string {
	return strings.NewReplacer(
		"{{key}}", is.Key, "{{title}}", is.Title, "{{status}}", is.Status,
		"{{assignee}}", is.Assignee, "{{reporter}}", is.Reporter, "{{project}}", is.Project,
	).Replace(text)
}
