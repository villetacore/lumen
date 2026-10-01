package service

import (
	"context"
	"time"

	"github.com/alexpyslar/lumen/server/internal/domain"
	"github.com/alexpyslar/lumen/server/internal/index"
)

// Views turns the named UI views into task queries. Views are queries, not
// stored entities (see docs/DATA_MODEL.md).
type Views struct{ store index.Store }

func NewViews(st index.Store) *Views { return &Views{store: st} }

// ResolveView expands a view name onto an existing query.
func ResolveView(name string, q domain.TaskQuery, now time.Time) domain.TaskQuery {
	today := domain.Date{T: time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())}
	open := []domain.TaskStatus{domain.StatusTodo, domain.StatusDoing}

	switch name {
	case "inbox":
		q.Status = open
		if q.PathPrefix == "" {
			q.PathPrefix = "00-inbox/"
		}
	case "today":
		q.Status = open
		q.ScheduledTo = today
		q.Sort = []domain.SortKey{{Field: "priority"}, {Field: "due"}}
	case "overdue":
		q.Status = open
		q.Overdue = true
	case "week":
		q.Status = open
		// The week starts on Monday.
		offset := (int(now.Weekday()) + 6) % 7
		start := today.T.AddDate(0, 0, -offset)
		q.ScheduledFrom = domain.Date{T: start}
		q.ScheduledTo = domain.Date{T: start.AddDate(0, 0, 6)}
	case "upcoming":
		q.Status = open
		yes := true
		q.HasDue = &yes
		q.DueAfter = today
	case "someday":
		q.Status = open
		no := false
		q.HasDue = &no
	case "done":
		q.Status = []domain.TaskStatus{domain.StatusDone}
		q.Sort = []domain.SortKey{{Field: "due", Desc: true}}
	case "all", "":
		// no extra constraints
	}
	return q
}

// Agenda groups the calendar range the UI shows next to the task list.
type Agenda struct {
	From   time.Time      `json:"from"`
	To     time.Time      `json:"to"`
	Events []domain.Event `json:"events"`
}

func (v *Views) Calendar(ctx context.Context, from, to time.Time) (Agenda, error) {
	ev, err := v.store.Events(ctx, from, to)
	if err != nil {
		return Agenda{}, err
	}
	return Agenda{From: from, To: to, Events: ev}, nil
}
