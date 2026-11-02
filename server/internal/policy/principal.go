// Package policy decides who may read and change what (ADR-0012). HTTP, search,
// SSE and services all ask it; no channel decides on its own.
package policy

import (
	"context"
	"errors"
	"strings"
)

// ErrForbidden is returned for a write the principal may not make. Reads of
// invisible objects report "not found" instead, so existence does not leak.
var ErrForbidden = errors.New("access denied")

// Role orders permissions: each role includes the ones below it.
type Role int

const (
	RoleNone Role = iota
	RoleViewer
	RoleMember
	RoleAdmin
)

func (r Role) String() string {
	switch r {
	case RoleViewer:
		return "viewer"
	case RoleMember:
		return "member"
	case RoleAdmin:
		return "admin"
	}
	return "none"
}

// ParseRole reads "viewer", "member" or "admin".
func ParseRole(s string) (Role, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "viewer":
		return RoleViewer, true
	case "member":
		return RoleMember, true
	case "admin":
		return RoleAdmin, true
	}
	return RoleNone, false
}

// Principal is who makes a request.
type Principal struct {
	ID       string   // stable subject; "local" or "token" for service access
	Username string   // preferred_username; matches "user:<name>" members
	Name     string   // for display
	Groups   []string // without the leading "/"
	Role     Role     // role in the workspace
}

type ctxKey struct{}

// With returns a context carrying p.
func With(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// From returns the principal of ctx, or nil.
func From(ctx context.Context) *Principal {
	p, _ := ctx.Value(ctxKey{}).(*Principal)
	return p
}

// Service is the principal of local mode and of the static token: the owner
// of a single-user installation.
func Service(id string) *Principal {
	return &Principal{ID: id, Username: id, Name: id, Role: RoleAdmin}
}

// RoleMapping turns identity-provider roles and groups into a workspace role.
type RoleMapping struct {
	Admin  []string
	Viewer []string
}

// WorkspaceRole applies the mapping. Without admin roles configured every
// admitted user is an administrator, which is how Lumen behaved before
// roles existed (ADR-0012 §2).
func (m RoleMapping) WorkspaceRole(have []string) Role {
	if len(m.Admin) == 0 {
		return RoleAdmin
	}
	set := map[string]bool{}
	for _, h := range have {
		set[strings.TrimPrefix(h, "/")] = true
	}
	for _, r := range m.Admin {
		if set[r] {
			return RoleAdmin
		}
	}
	for _, r := range m.Viewer {
		if set[r] {
			return RoleViewer
		}
	}
	return RoleMember
}
