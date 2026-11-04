package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/villetacore/lumen/server/internal/domain"
)

type inbox struct {
	Items []struct {
		ID    int64  `json:"id"`
		Kind  string `json:"kind"`
		Issue string `json:"issue"`
		Actor string `json:"actor"`
		Title string `json:"title"`
		Read  bool   `json:"read"`
	} `json:"items"`
	Unread int `json:"unread"`
}

func (f *oidcFixture) inbox(user string) inbox {
	var in inbox
	json.Unmarshal(f.expect(user, "GET", "/api/v1/notifications", nil, 200), &in)
	return in
}

func kinds(in inbox) string {
	var out []string
	for _, n := range in.Items {
		out = append(out, n.Kind+":"+n.Issue+":"+n.Actor)
	}
	return strings.Join(out, ",")
}

func TestTeamworkNotifications(t *testing.T) {
	f := newOIDCFixture(t, policyVault)
	f.expect("boss", "PUT", "/api/v1/tracker/projects/SEC/members", map[string]any{"kind": "user", "name": "alice", "role": "member"}, 200)

	// bob files an issue for alice and mentions vic.
	var is domain.Issue
	json.Unmarshal(f.expect("bob", "POST", "/api/v1/issues", map[string]any{
		"project": "WEB", "title": "Макет", "assignee": "alice", "description": "Нужна помощь @vic и `@nobody`",
	}, 201), &is)
	if got := kinds(f.inbox("alice")); got != "assigned:"+is.Key+":bob" {
		t.Fatalf("alice = %s", got)
	}
	if got := kinds(f.inbox("vic")); got != "mentioned:"+is.Key+":bob" {
		t.Fatalf("vic = %s", got)
	}
	// The same request again changes nothing and notifies nobody.
	f.expect("bob", "PATCH", "/api/v1/issues/"+is.Key, map[string]any{"assignee": "alice"}, 200)
	f.expect("bob", "PATCH", "/api/v1/issues/"+is.Key, map[string]any{"description": "Нужна помощь @vic и `@nobody`"}, 200)
	if n := len(f.inbox("alice").Items) + len(f.inbox("vic").Items); n != 2 {
		t.Fatalf("a repeated request added notifications: %d", n)
	}

	// alice comments and mentions bob: bob (reporter) gets the mention, not
	// a second "commented"; alice herself gets nothing.
	f.expect("alice", "POST", "/api/v1/issues/"+is.Key+"/comments", map[string]any{"body": "@bob посмотри"}, 201)
	f.expect("alice", "POST", "/api/v1/issues/"+is.Key+"/move", map[string]any{"status": "in-progress"}, 200)
	if got := kinds(f.inbox("bob")); got != "status:"+is.Key+":alice,mentioned:"+is.Key+":alice" {
		t.Fatalf("bob = %s", got)
	}
	if got := kinds(f.inbox("alice")); got != "assigned:"+is.Key+":bob" {
		t.Fatalf("alice notified about her own actions: %s", got)
	}

	// A mention inside a closed project does not reach — or reveal it to — bob.
	f.expect("alice", "POST", "/api/v1/issues/SEC-1/comments", map[string]any{"body": "@bob, это секрет"}, 201)
	bob := f.inbox("bob")
	if strings.Contains(kinds(bob), "SEC") || bob.Unread != 2 {
		t.Fatalf("bob sees SEC: %s unread=%d", kinds(bob), bob.Unread)
	}
	raw, _ := json.Marshal(bob)
	if strings.Contains(string(raw), "Закрытый") {
		t.Fatalf("SEC title leaked: %s", raw)
	}

	// History: who changed what, visible only with the issue itself.
	var hist struct {
		Items []struct {
			Kind, Field, New, Actor, Source string
		} `json:"items"`
	}
	json.Unmarshal(f.expect("vic", "GET", "/api/v1/issues/"+is.Key+"/history", nil, 200), &hist)
	var lines []string
	for _, e := range hist.Items {
		lines = append(lines, e.Kind+":"+e.Field+":"+e.New+"@"+e.Source+"/"+e.Actor)
	}
	if got := strings.Join(lines, ","); got != "created::@lumen/bob,changed:comments:1@lumen/alice,changed:status:in-progress@lumen/alice" {
		t.Fatalf("history = %s", got)
	}
	f.expect("bob", "GET", "/api/v1/issues/SEC-1/history", nil, 404)
	f.expect("alice", "GET", "/api/v1/issues/SEC-1/history", nil, 200)

	// My issues.
	var mine listEnvelope[domain.Issue]
	json.Unmarshal(f.expect("alice", "GET", "/api/v1/issues?assignee=me", nil, 200), &mine)
	if mine.Total != 1 || mine.Items[0].Key != is.Key {
		t.Fatalf("alice's issues = %+v", mine.Items)
	}
	json.Unmarshal(f.expect("bob", "GET", "/api/v1/issues?watcher=me", nil, 200), &mine)
	if mine.Total != 1 || mine.Items[0].Key != is.Key {
		t.Fatalf("bob watches = %+v", mine.Items)
	}

	// Reading.
	f.expect("bob", "POST", "/api/v1/notifications/read", map[string]any{"ids": []int64{bob.Items[0].ID}}, 204)
	if got := f.inbox("bob").Unread; got != 1 {
		t.Fatalf("unread after one = %d", got)
	}
	f.expect("vic", "POST", "/api/v1/notifications/read", nil, 204) // someone else's "all" does not touch bob's
	if got := f.inbox("bob").Unread; got != 1 {
		t.Fatalf("vic read bob's notifications: %d", got)
	}
	f.expect("bob", "POST", "/api/v1/notifications/read", nil, 204)
	if got := f.inbox("bob").Unread; got != 0 {
		t.Fatalf("unread after all = %d", got)
	}

	// SSE: vic gets her own notification, not bob's.
	req, _ := http.NewRequest("GET", f.srv.URL+"/api/v1/events", nil)
	req.Header.Set("Authorization", "Bearer "+f.tokens["vic"])
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := f.srv.Client().Do(req.WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	time.Sleep(100 * time.Millisecond)
	f.expect("alice", "POST", "/api/v1/issues/"+is.Key+"/comments", map[string]any{"body": "@bob ещё"}, 201)
	f.expect("alice", "POST", "/api/v1/issues/"+is.Key+"/comments", map[string]any{"body": "@vic и тебе"}, 201)
	changes := 0
	sc := bufio.NewScanner(res.Body)
	for sc.Scan() {
		line := sc.Text()
		if line == "event: issue.changed" {
			changes++
		}
		if line == "event: notification" {
			if changes < 2 {
				t.Fatalf("vic got a notification after %d changes: bob's leaked", changes)
			}
			break
		}
	}
	if sc.Err() != nil {
		t.Fatal(sc.Err())
	}
}
