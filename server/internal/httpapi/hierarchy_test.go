package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/villetacore/lumen/server/internal/automation"
	"github.com/villetacore/lumen/server/internal/domain"
)

// The tree of projects, the tree of pages and automations, each through HTTP with
// real tokens: the same access rules hold on every new route.
func TestProjectTreeAcrossChannels(t *testing.T) {
	f := newOIDCFixture(t, policyVault)
	f.expect("boss", http.MethodPost, "/api/v1/tracker/projects", map[string]any{"key": "OPS", "title": "Эксплуатация"}, http.StatusCreated)
	var child domain.TrackerProject
	body := f.expect("boss", http.MethodPost, "/api/v1/tracker/projects", map[string]any{"key": "NET", "title": "Сеть", "parent": "OPS"}, http.StatusCreated)
	json.Unmarshal(body, &child)
	if child.Parent != "OPS" {
		t.Fatalf("child = %+v", child)
	}
	f.expect("alice", http.MethodPost, "/api/v1/issues", map[string]any{"project": "NET", "title": "Коммутатор"}, http.StatusCreated)
	f.expect("alice", http.MethodPost, "/api/v1/issues", map[string]any{"project": "OPS", "title": "Регламент"}, http.StatusCreated)

	var page struct {
		Items []domain.Issue `json:"items"`
	}
	json.Unmarshal(f.expect("alice", http.MethodGet, "/api/v1/issues?project=OPS&subprojects=true", nil, http.StatusOK), &page)
	if len(page.Items) != 2 {
		t.Fatalf("subtree = %+v", page.Items)
	}

	// Closing the parent closes the subtree. A nested project is not a side door.
	f.expect("boss", http.MethodPut, "/api/v1/tracker/projects/OPS/members", map[string]any{"kind": "user", "name": "alice", "role": "admin"}, http.StatusOK)
	for _, path := range []string{
		"/api/v1/tracker/projects/NET", "/api/v1/issues/NET-1", "/api/v1/tracker/projects/NET/automations",
		"/api/v1/tracker/projects/NET/automation-runs", "/api/v1/tracker/projects/NET/members",
	} {
		f.expect("bob", http.MethodGet, path, nil, http.StatusNotFound)
		f.expect("alice", http.MethodGet, path, nil, http.StatusOK)
	}
	if _, out := f.call("bob", http.MethodGet, "/api/v1/tracker/projects", nil); strings.Contains(string(out), "NET") {
		t.Fatalf("hidden project listed: %s", out)
	}
	if _, out := f.call("bob", http.MethodGet, "/api/v1/issues?project=OPS&subprojects=true", nil); strings.Contains(string(out), "NET-1") {
		t.Fatalf("hidden issue listed: %s", out)
	}
	var acc struct {
		Open          bool   `json:"open"`
		InheritedFrom string `json:"inherited_from"`
	}
	json.Unmarshal(f.expect("alice", http.MethodGet, "/api/v1/tracker/projects/NET/members", nil, http.StatusOK), &acc)
	if acc.Open || acc.InheritedFrom != "OPS" {
		t.Fatalf("access = %+v", acc)
	}

	// An outsider cannot attach to it, and a cycle is refused.
	f.expect("bob", http.MethodPost, "/api/v1/tracker/projects", map[string]any{"key": "MY", "title": "Мой", "parent": "OPS"}, http.StatusNotFound)
	f.expect("alice", http.MethodPatch, "/api/v1/tracker/projects/OPS", map[string]any{"parent": "NET"}, http.StatusUnprocessableEntity)
	// Taking NET out of the closed parent changes who sees it: a viewer cannot.
	f.expect("vic", http.MethodPatch, "/api/v1/tracker/projects/WEB", map[string]any{"title": "Х"}, http.StatusForbidden)
	f.expect("alice", http.MethodPatch, "/api/v1/tracker/projects/NET", map[string]any{"parent": ""}, http.StatusOK)
	f.expect("bob", http.MethodGet, "/api/v1/tracker/projects/NET", nil, http.StatusOK)
}

func TestPageTreeAcrossChannels(t *testing.T) {
	f := newOIDCFixture(t, policyVault)
	f.expect("boss", http.MethodPut, "/api/v1/tracker/projects/SEC/members", map[string]any{"kind": "user", "name": "alice", "role": "member"}, http.StatusOK)

	create := func(user, title, parent string) domain.Note {
		var n domain.Note
		json.Unmarshal(f.expect(user, http.MethodPost, "/api/v1/notes", map[string]any{"title": title, "content_kind": "document", "parent": parent}, http.StatusCreated), &n)
		return n
	}
	var project domain.TrackerProject
	json.Unmarshal(f.expect("alice", http.MethodGet, "/api/v1/tracker/projects/SEC", nil, http.StatusOK), &project)
	top := create("alice", "Раздел", "note:"+project.NoteID)
	sub := create("alice", "Подраздел", "note:"+top.ID)
	open := create("bob", "Открытая страница", "")

	// A page under a closed project's page is closed, however deep.
	f.expect("bob", http.MethodGet, "/api/v1/notes/"+sub.Path, nil, http.StatusNotFound)
	f.expect("bob", http.MethodPost, "/api/v1/pages/move", map[string]any{"id": sub.ID}, http.StatusNotFound)
	if _, out := f.call("bob", http.MethodGet, "/api/v1/notes?content_kind=document", nil); strings.Contains(string(out), "Подраздел") {
		t.Fatalf("hidden page listed: %s", out)
	}
	// An outsider cannot hang their page on something they cannot see.
	f.expect("bob", http.MethodPost, "/api/v1/pages/move", map[string]any{"id": open.ID, "parent": "note:" + top.ID}, http.StatusNotFound)
	// A viewer cannot move anything.
	f.expect("vic", http.MethodPost, "/api/v1/pages/move", map[string]any{"id": open.ID}, http.StatusForbidden)

	// Moving into the closed project hides the page from those outside.
	f.expect("alice", http.MethodPost, "/api/v1/pages/move", map[string]any{"id": open.ID, "parent": "note:" + top.ID, "before": sub.ID}, http.StatusOK)
	f.expect("bob", http.MethodGet, "/api/v1/notes/"+open.Path, nil, http.StatusNotFound)

	// Deleting a page in the middle does not leak what was under it.
	f.expect("alice", http.MethodDelete, "/api/v1/notes/"+top.Path, nil, http.StatusNoContent)
	f.expect("bob", http.MethodGet, "/api/v1/notes/"+sub.Path, nil, http.StatusNotFound)
	f.expect("alice", http.MethodGet, "/api/v1/notes/"+sub.Path, nil, http.StatusOK)
}

func TestAutomationsAcrossChannels(t *testing.T) {
	f := newOIDCFixture(t, policyVault)
	ctx := context.Background()
	if err := f.api.automations.Prepare(ctx); err != nil {
		t.Fatal(err)
	}
	def := map[string]any{
		"name":    "Готовые в архив",
		"trigger": map[string]any{"type": "status_changed", "to": "done"},
		"actions": []map[string]any{{"type": "add_label", "value": "архив"}, {"type": "notify", "value": "@bob"}},
	}
	// Reads for any project reader; writes for its admin only.
	f.expect("bob", http.MethodPost, "/api/v1/tracker/projects/WEB/automations", def, http.StatusForbidden)
	f.expect("vic", http.MethodPost, "/api/v1/tracker/projects/WEB/automations", def, http.StatusForbidden)
	var rule automation.Rule
	json.Unmarshal(f.expect("boss", http.MethodPost, "/api/v1/tracker/projects/WEB/automations", def, http.StatusCreated), &rule)
	f.expect("boss", http.MethodPost, "/api/v1/tracker/projects/WEB/automations", map[string]any{
		"name": "Плохое", "trigger": map[string]any{"type": "status_changed", "to": "нет"}, "actions": def["actions"],
	}, http.StatusUnprocessableEntity)

	var list struct {
		Items   []automation.Rule `json:"items"`
		CanEdit bool              `json:"can_edit"`
	}
	json.Unmarshal(f.expect("bob", http.MethodGet, "/api/v1/tracker/projects/WEB/automations", nil, http.StatusOK), &list)
	if len(list.Items) != 1 || list.CanEdit {
		t.Fatalf("list as member = %+v", list)
	}
	f.expect("bob", http.MethodDelete, "/api/v1/automations/"+rule.ID, nil, http.StatusForbidden)
	f.expect("bob", http.MethodPost, "/api/v1/automations/"+rule.ID+"/run", nil, http.StatusForbidden)
	f.expect("boss", http.MethodPost, "/api/v1/automations/"+rule.ID+"/run", nil, http.StatusUnprocessableEntity) // not a schedule

	// A rule in a project closed to the caller answers as if it did not exist.
	json.Unmarshal(f.expect("boss", http.MethodPost, "/api/v1/tracker/projects/SEC/automations", def, http.StatusCreated), &rule)
	f.expect("boss", http.MethodPut, "/api/v1/tracker/projects/SEC/members", map[string]any{"kind": "user", "name": "alice", "role": "member"}, http.StatusOK)
	f.expect("bob", http.MethodDelete, "/api/v1/automations/"+rule.ID, nil, http.StatusNotFound)
	f.expect("alice", http.MethodDelete, "/api/v1/automations/"+rule.ID, nil, http.StatusForbidden)

	// End to end: a person moves a ticket, the worker applies the rule as its owner.
	f.expect("bob", http.MethodPatch, "/api/v1/issues/WEB-1", map[string]any{"status": "done"}, http.StatusOK)
	if err := f.api.automations.Step(ctx); err != nil {
		t.Fatal(err)
	}
	var is domain.Issue
	json.Unmarshal(f.expect("bob", http.MethodGet, "/api/v1/issues/WEB-1", nil, http.StatusOK), &is)
	if len(is.Labels) != 1 || is.Labels[0] != "архив" {
		t.Fatalf("issue = %+v", is)
	}
	var runs struct {
		Items []automation.Run `json:"items"`
	}
	json.Unmarshal(f.expect("bob", http.MethodGet, "/api/v1/tracker/projects/WEB/automation-runs", nil, http.StatusOK), &runs)
	if len(runs.Items) != 1 || runs.Items[0].Status != automation.StatusDone || runs.Items[0].RuleName != "Готовые в архив" {
		t.Fatalf("runs = %+v", runs)
	}
	var inbox struct {
		Items []struct {
			Kind string `json:"kind"`
			Rule string `json:"rule"`
		} `json:"items"`
	}
	json.Unmarshal(f.expect("bob", http.MethodGet, "/api/v1/notifications", nil, http.StatusOK), &inbox)
	found := false
	for _, n := range inbox.Items {
		found = found || (n.Kind == "automation" && n.Rule == "Готовые в архив")
	}
	if !found {
		t.Fatalf("inbox = %+v", inbox)
	}
	// The journal shows it was a rule, under the owner's name.
	var hist struct {
		Items []struct {
			Field, Source, Actor string
		} `json:"items"`
	}
	json.Unmarshal(f.expect("bob", http.MethodGet, "/api/v1/issues/WEB-1/history", nil, http.StatusOK), &hist)
	attributed := false
	for _, e := range hist.Items {
		attributed = attributed || (e.Field == "labels" && e.Source == "automation" && e.Actor == "boss")
	}
	if !attributed {
		t.Fatalf("history = %+v", hist)
	}
}
