package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/alexpyslar/lumen/server/internal/domain"
	"github.com/alexpyslar/lumen/server/internal/policy"
	"github.com/alexpyslar/lumen/server/internal/proposals"
	"github.com/alexpyslar/lumen/server/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Confirmed writes (ADR-0013 §6). An MCP tool records a proposal with a
// preview; the same person applies it in Lumen. Nothing in this file lets a
// model or an MCP client apply a change itself.

type (
	createIssueArgs struct {
		Project     string   `json:"project" jsonschema:"ключ проекта"`
		Title       string   `json:"title"`
		Type        string   `json:"issue_type,omitempty" jsonschema:"task, bug, story, epic или тип проекта"`
		Priority    string   `json:"priority,omitempty" jsonschema:"highest, high, medium, low, lowest"`
		Assignee    string   `json:"assignee,omitempty" jsonschema:"логин исполнителя"`
		Labels      []string `json:"labels,omitempty"`
		Due         string   `json:"due,omitempty" jsonschema:"срок YYYY-MM-DD"`
		Sprint      string   `json:"sprint,omitempty" jsonschema:"id запланированного или активного спринта"`
		Description string   `json:"description,omitempty" jsonschema:"Markdown"`
		RequestID   string   `json:"request_id,omitempty" jsonschema:"свой id запроса: повтор с ним вернёт то же предложение"`
	}
	updateIssueArgs struct {
		Key         string    `json:"key"`
		Title       *string   `json:"title,omitempty"`
		Priority    *string   `json:"priority,omitempty"`
		Assignee    *string   `json:"assignee,omitempty" jsonschema:"логин; пустая строка снимает исполнителя"`
		Labels      *[]string `json:"labels,omitempty"`
		Due         *string   `json:"due,omitempty" jsonschema:"YYYY-MM-DD; пустая строка снимает срок"`
		Sprint      *string   `json:"sprint,omitempty" jsonschema:"id спринта; пустая строка — в бэклог"`
		Description *string   `json:"description,omitempty" jsonschema:"новое описание целиком (Markdown)"`
		RequestID   string    `json:"request_id,omitempty"`
	}
	statusArgs struct {
		Key       string `json:"key"`
		Status    string `json:"status" jsonschema:"id статуса из list_projects"`
		From      string `json:"from,omitempty" jsonschema:"заполняется сервером"`
		RequestID string `json:"request_id,omitempty"`
	}
	commentArgs struct {
		Key       string `json:"key"`
		Body      string `json:"body" jsonschema:"текст комментария, Markdown; @логин упоминает человека"`
		RequestID string `json:"request_id,omitempty"`
	}
	proposalOut struct {
		ProposalID string `json:"proposal_id"`
		State      string `json:"state" jsonschema:"pending — ждёт подтверждения человеком"`
		Preview    string `json:"preview"`
		Result     string `json:"result,omitempty" jsonschema:"после применения: ключ тикета"`
		Error      string `json:"error,omitempty"`
		ConfirmAt  string `json:"confirm_at" jsonschema:"где человек подтверждает изменение"`
	}
	proposalIDIn struct {
		ID string `json:"proposal_id"`
	}
)

func (a *API) confirmURL() string { return strings.TrimSuffix(a.cfg.PublicURL, "/") + "/approvals" }

func (a *API) proposalView(p proposals.Proposal) proposalOut {
	return proposalOut{ProposalID: p.ID, State: string(p.State), Preview: p.Preview, Result: p.Result, Error: p.Error, ConfirmAt: a.confirmURL()}
}

func show(v string) string {
	if v == "" {
		return "—"
	}
	return v
}

// propose validates the change as the caller and stores it.
func (a *API) propose(ctx context.Context, kind, target, baseHash, requestID string, args any, preview string) (proposals.Proposal, error) {
	if a.proposals == nil {
		return proposals.Proposal{}, errors.New("подтверждения недоступны: нет операционной БД")
	}
	p := policy.From(ctx)
	raw, err := json.Marshal(args)
	if err != nil {
		return proposals.Proposal{}, err
	}
	out, err := a.proposals.Create(ctx, proposals.Proposal{
		Requester: p.Username, PrincipalID: p.ID, Kind: kind, Target: target, BaseHash: baseHash,
		Args: raw, Preview: preview, RequestID: requestID,
	})
	if err == nil {
		a.bus.Publish(service.Event{Type: "proposal", User: p.Username})
	}
	return out, err
}

func (a *API) noteHash(ctx context.Context, is *domain.Issue) string {
	if n, err := a.store.Note(ctx, is.Path); err == nil {
		return n.Hash
	}
	return ""
}

func (a *API) proposeCreate(ctx context.Context, in createIssueArgs) (proposals.Proposal, error) {
	p, err := a.issues.Project(ctx, in.Project)
	if err != nil {
		return proposals.Proposal{}, err
	}
	if err := a.policy.CheckProject(ctx, p.Key, policy.RoleMember); err != nil {
		return proposals.Proposal{}, err
	}
	if strings.TrimSpace(in.Title) == "" {
		return proposals.Proposal{}, errors.New("нужно название тикета")
	}
	in.Project = p.Key
	var b strings.Builder
	fmt.Fprintf(&b, "Создать тикет в %s: «%s»", p.Key, strings.TrimSpace(in.Title))
	for _, f := range [][2]string{{"Тип", in.Type}, {"Приоритет", in.Priority}, {"Исполнитель", in.Assignee},
		{"Метки", strings.Join(in.Labels, ", ")}, {"Срок", in.Due}, {"Спринт", in.Sprint}} {
		if f[1] != "" {
			fmt.Fprintf(&b, "\n%s: %s", f[0], f[1])
		}
	}
	if d := strings.TrimSpace(in.Description); d != "" {
		fmt.Fprintf(&b, "\nОписание:\n%s", d)
	}
	return a.propose(ctx, "create_issue", p.Key, "", in.RequestID, in, b.String())
}

func (a *API) proposeUpdate(ctx context.Context, in updateIssueArgs) (proposals.Proposal, error) {
	is, err := a.issues.Get(ctx, in.Key)
	if err != nil {
		return proposals.Proposal{}, err
	}
	if err := a.policy.CheckProject(ctx, is.Project, policy.RoleMember); err != nil {
		return proposals.Proposal{}, err
	}
	var lines []string
	diff := func(label, old string, v *string) {
		if v != nil && strings.TrimSpace(*v) != old {
			lines = append(lines, fmt.Sprintf("%s: %s → %s", label, show(old), show(strings.TrimSpace(*v))))
		}
	}
	due := ""
	if !is.Due.IsZero() {
		due = is.Due.String()
	}
	diff("Название", is.Title, in.Title)
	diff("Приоритет", is.Priority, in.Priority)
	diff("Исполнитель", is.Assignee, in.Assignee)
	diff("Срок", due, in.Due)
	diff("Спринт", is.Sprint, in.Sprint)
	if in.Labels != nil && strings.Join(*in.Labels, ", ") != strings.Join(is.Labels, ", ") {
		lines = append(lines, fmt.Sprintf("Метки: %s → %s", show(strings.Join(is.Labels, ", ")), show(strings.Join(*in.Labels, ", "))))
	}
	if in.Description != nil && strings.TrimSpace(*in.Description) != is.Description {
		lines = append(lines, "Описание заменить на:\n"+strings.TrimSpace(*in.Description))
	}
	if len(lines) == 0 {
		return proposals.Proposal{}, errors.New("ничего не меняется")
	}
	in.Key = is.Key
	preview := fmt.Sprintf("%s «%s»\n%s", is.Key, is.Title, strings.Join(lines, "\n"))
	return a.propose(ctx, "update_issue", is.Key, a.noteHash(ctx, is), in.RequestID, in, preview)
}

func (a *API) proposeStatus(ctx context.Context, in statusArgs) (proposals.Proposal, error) {
	is, err := a.issues.Get(ctx, in.Key)
	if err != nil {
		return proposals.Proposal{}, err
	}
	p, err := a.issues.Project(ctx, is.Project)
	if err != nil {
		return proposals.Proposal{}, err
	}
	if err := a.policy.CheckProject(ctx, p.Key, policy.RoleMember); err != nil {
		return proposals.Proposal{}, err
	}
	to, ok := p.Status(in.Status)
	if !ok {
		return proposals.Proposal{}, fmt.Errorf("в проекте %s нет статуса %q", p.Key, in.Status)
	}
	if in.Status == is.Status {
		return proposals.Proposal{}, errors.New("тикет уже в этом статусе")
	}
	if !p.CanMove(is.Status, in.Status) {
		return proposals.Proposal{}, fmt.Errorf("workflow не разрешает переход %s → %s", is.Status, in.Status)
	}
	from, _ := p.Status(is.Status)
	in.Key, in.From = is.Key, is.Status
	preview := fmt.Sprintf("%s «%s»\nСтатус: %s → %s", is.Key, is.Title, show(from.Name), to.Name)
	return a.propose(ctx, "change_status", is.Key, "", in.RequestID, in, preview)
}

func (a *API) proposeComment(ctx context.Context, in commentArgs) (proposals.Proposal, error) {
	is, err := a.issues.Get(ctx, in.Key)
	if err != nil {
		return proposals.Proposal{}, err
	}
	if err := a.policy.CheckProject(ctx, is.Project, policy.RoleMember); err != nil {
		return proposals.Proposal{}, err
	}
	body := strings.TrimSpace(in.Body)
	if body == "" {
		return proposals.Proposal{}, errors.New("пустой комментарий")
	}
	in.Key = is.Key
	return a.propose(ctx, "add_comment", is.Key, "", in.RequestID, in, fmt.Sprintf("Комментарий к %s «%s»:\n%s", is.Key, is.Title, body))
}

// applyProposal runs a confirmed proposal with the confirming person's
// rights. A change made to the issue since the preview makes it stale.
func (a *API) applyProposal(ctx context.Context, id string) (proposals.Proposal, error) {
	who := policy.From(ctx)
	p, err := a.proposals.Begin(ctx, who.Username, who.ID, id)
	if err != nil {
		return p, err
	}
	finish := func(state proposals.State, result string, applyErr error) (proposals.Proposal, error) {
		msg := ""
		if applyErr != nil {
			msg = toolError(applyErr).Error()
		}
		if err := a.proposals.Finish(ctx, p.ID, state, result, msg); err != nil {
			return p, err
		}
		a.bus.Publish(service.Event{Type: "proposal", User: who.Username})
		return a.proposals.Get(ctx, who.Username, p.ID)
	}
	switch p.Kind {
	case "create_issue":
		var in createIssueArgs
		if err := json.Unmarshal(p.Args, &in); err != nil {
			return finish(proposals.Failed, "", err)
		}
		is, err := a.issues.Create(ctx, service.IssueInput{Project: in.Project, Title: in.Title, Type: in.Type, Priority: in.Priority,
			Assignee: in.Assignee, Labels: in.Labels, Due: in.Due, Sprint: in.Sprint, Description: in.Description}, who.Username)
		if err != nil {
			return finish(proposals.Failed, "", err)
		}
		return finish(proposals.Applied, is.Key, nil)
	case "update_issue":
		var in updateIssueArgs
		if err := json.Unmarshal(p.Args, &in); err != nil {
			return finish(proposals.Failed, "", err)
		}
		is, err := a.issues.Get(ctx, in.Key)
		if err != nil {
			return finish(proposals.Failed, "", err)
		}
		if a.noteHash(ctx, is) != p.BaseHash {
			return finish(proposals.Stale, "", errors.New("тикет изменился после предпросмотра; попросите заново"))
		}
		_, err = a.issues.Update(ctx, in.Key, service.IssueUpdate{Title: in.Title, Priority: in.Priority, Assignee: in.Assignee,
			Labels: in.Labels, Due: in.Due, Sprint: in.Sprint, Description: in.Description})
		if err != nil {
			return finish(proposals.Failed, "", err)
		}
		return finish(proposals.Applied, in.Key, nil)
	case "change_status":
		var in statusArgs
		if err := json.Unmarshal(p.Args, &in); err != nil {
			return finish(proposals.Failed, "", err)
		}
		is, err := a.issues.Get(ctx, in.Key)
		if err != nil {
			return finish(proposals.Failed, "", err)
		}
		if is.Status != in.From {
			return finish(proposals.Stale, "", fmt.Errorf("статус уже %s; попросите заново", is.Status))
		}
		if _, err := a.issues.Move(ctx, in.Key, service.MoveInput{Status: in.Status}); err != nil {
			return finish(proposals.Failed, "", err)
		}
		return finish(proposals.Applied, in.Key, nil)
	case "add_comment":
		var in commentArgs
		if err := json.Unmarshal(p.Args, &in); err != nil {
			return finish(proposals.Failed, "", err)
		}
		if _, _, err := a.issues.AddComment(ctx, in.Key, in.Body, who.Username); err != nil {
			return finish(proposals.Failed, "", err)
		}
		return finish(proposals.Applied, in.Key, nil)
	}
	return finish(proposals.Failed, "", fmt.Errorf("unknown proposal kind %q", p.Kind))
}

var proposeHint = &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: new(bool), IdempotentHint: true, OpenWorldHint: new(bool)}

func (a *API) addMCPWriteTools(s *mcp.Server) {
	note := " Изменение не применяется сразу: человек подтверждает его в Lumen (confirm_at). Проверяйте результат через get_proposal."
	addPropose(a, s, "propose_create_issue", "Предложить новый тикет."+note, a.proposeCreate)
	addPropose(a, s, "propose_update_issue", "Предложить изменить поля тикета (только указанные)."+note, a.proposeUpdate)
	addPropose(a, s, "propose_change_status", "Предложить перевести тикет в другой статус workflow."+note, a.proposeStatus)
	addPropose(a, s, "propose_comment", "Предложить комментарий к тикету."+note, a.proposeComment)

	mcp.AddTool(s, &mcp.Tool{Name: "get_proposal", Description: "Состояние предложенного изменения: pending, applied, rejected, expired, stale, failed.", Annotations: readOnly},
		func(ctx context.Context, req *mcp.CallToolRequest, in proposalIDIn) (*mcp.CallToolResult, proposalOut, error) {
			ctx, p, err := mcpContext(ctx, req)
			if err != nil {
				return nil, proposalOut{}, err
			}
			if a.proposals == nil {
				return nil, proposalOut{}, errors.New("подтверждения недоступны")
			}
			pr, err := a.proposals.Get(ctx, p.Username, in.ID)
			if err != nil {
				return nil, proposalOut{}, errors.New("предложение не найдено")
			}
			return nil, a.proposalView(pr), nil
		})
}

func addPropose[In any](a *API, s *mcp.Server, name, desc string, fn func(context.Context, In) (proposals.Proposal, error)) {
	mcp.AddTool(s, &mcp.Tool{Name: name, Description: desc, Annotations: proposeHint},
		func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, proposalOut, error) {
			ctx, _, err := mcpContext(ctx, req)
			if err != nil {
				return nil, proposalOut{}, err
			}
			p, err := fn(ctx, in)
			if err != nil {
				return nil, proposalOut{}, toolError(err)
			}
			return nil, a.proposalView(p), nil
		})
}

// HTTP side: the person sees and decides on their proposals.

func (a *API) proposalUser(w http.ResponseWriter, r *http.Request) (*policy.Principal, bool) {
	p := policy.From(r.Context())
	if p == nil || p.Username == "" || a.proposals == nil {
		writeJSON(w, http.StatusOK, listEnvelope[proposals.Proposal]{Items: []proposals.Proposal{}})
		return nil, false
	}
	return p, true
}

func (a *API) handleListProposals(w http.ResponseWriter, r *http.Request) {
	p, ok := a.proposalUser(w, r)
	if !ok {
		return
	}
	list, err := a.proposals.List(r.Context(), p.Username, r.URL.Query().Get("pending") == "true")
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, listEnvelope[proposals.Proposal]{Items: list, Total: len(list)})
}

func (a *API) handleDecideProposal(approve bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := policy.From(r.Context())
		if p == nil || p.Username == "" || a.proposals == nil {
			writeError(w, http.StatusNotFound, "not_found", "нет предложения", nil)
			return
		}
		var (
			out proposals.Proposal
			err error
		)
		if approve {
			out, err = a.applyProposal(r.Context(), r.PathValue("id"))
		} else {
			out, err = a.proposals.Reject(r.Context(), p.Username, p.ID, r.PathValue("id"))
			if err == nil {
				a.bus.Publish(service.Event{Type: "proposal", User: p.Username})
			}
		}
		switch {
		case errors.Is(err, proposals.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "нет предложения", nil)
		case errors.Is(err, proposals.ErrState):
			writeError(w, http.StatusConflict, "proposal_state", err.Error(), nil)
		case err != nil:
			fail(w, err)
		default:
			writeJSON(w, http.StatusOK, out)
		}
	}
}
