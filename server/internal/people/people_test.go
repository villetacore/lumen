package people

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/villetacore/lumen/server/internal/policy"
	"github.com/villetacore/lumen/server/internal/state/statetest"
)

func open(t *testing.T) *Directory {
	t.Helper()
	st := statetest.Open(t)
	t.Cleanup(func() { st.Close() })
	return New(st.DB())
}

func TestTouchRecordsAndRefreshes(t *testing.T) {
	ctx := context.Background()
	d := open(t)
	now := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	nowFunc = func() time.Time { return now }
	t.Cleanup(func() { nowFunc = time.Now })

	alice := &policy.Principal{ID: "s-1", Username: "alice", Name: "Алиса", Groups: []string{"/designers", "staff", "/designers"}, Role: policy.RoleMember}
	if err := d.Touch(ctx, alice, "alice@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := d.Touch(ctx, policy.Service("local"), ""); err != nil {
		t.Fatal(err)
	}
	users, _ := d.Users(ctx, "")
	if len(users) != 1 || users[0].Username != "alice" || strings.Join(users[0].Groups, ",") != "designers,staff" ||
		users[0].Role != "member" || users[0].FirstSeen != "2026-11-02T10:00:00Z" {
		t.Fatalf("users = %+v", users)
	}

	// Same data a minute later: no write, last_seen stays.
	now = now.Add(time.Minute)
	d.Touch(ctx, alice, "alice@example.com")
	if u, _ := d.Users(ctx, ""); u[0].LastSeen != "2026-11-02T10:00:00Z" {
		t.Fatalf("throttled touch wrote: %+v", u[0])
	}
	// Groups changed in Keycloak: written at once.
	alice.Groups = []string{"staff"}
	alice.Role = policy.RoleAdmin
	d.Touch(ctx, alice, "alice@example.com")
	u, _ := d.Users(ctx, "")
	if strings.Join(u[0].Groups, ",") != "staff" || u[0].Role != "admin" || u[0].LastSeen != "2026-11-02T10:01:00Z" || u[0].FirstSeen != "2026-11-02T10:00:00Z" {
		t.Fatalf("after change = %+v", u[0])
	}
	groups, _ := d.Groups(ctx)
	if len(groups) != 1 || groups[0].Name != "staff" || groups[0].Users != 1 {
		t.Fatalf("groups = %+v", groups)
	}
}

func TestSearchAndUsernameTakeover(t *testing.T) {
	ctx := context.Background()
	d := open(t)
	d.Touch(ctx, &policy.Principal{ID: "s-1", Username: "alice", Name: "Алиса Иванова"}, "")
	d.Touch(ctx, &policy.Principal{ID: "s-2", Username: "bob", Name: "Боб"}, "bob@corp.test")
	if u, _ := d.Users(ctx, "иван"); len(u) != 1 || u[0].Username != "alice" {
		t.Fatalf("search by name: %+v", u)
	}
	if u, _ := d.Users(ctx, "CORP"); len(u) != 1 || u[0].Username != "bob" {
		t.Fatalf("search by email: %+v", u)
	}
	// The account was recreated in Keycloak: a new subject with the same username.
	d.Touch(ctx, &policy.Principal{ID: "s-9", Username: "alice", Name: "Алиса"}, "")
	if u, _ := d.Users(ctx, "alice"); len(u) != 1 || u[0].Name != "Алиса" {
		t.Fatalf("takeover: %+v", u)
	}
}
