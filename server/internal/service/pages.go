package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/villetacore/lumen/server/internal/domain"
	"github.com/villetacore/lumen/server/internal/index"
	"github.com/villetacore/lumen/server/internal/markdown"
	"github.com/villetacore/lumen/server/internal/policy"
	"github.com/villetacore/lumen/server/internal/vault"
)

// The page tree is the "parent" and "rank" keys of document notes (ADR-0020 §1).
// Files never move, so wikilinks and paths survive every change of the tree.

// maxPageDepth must stay below policy.parentDepth: access follows the parent chain.
const maxPageDepth = 32

// PageMove places a page: under Parent ("" is the top level, "note:<id>" or
// "task:<id>" otherwise), between the siblings Before (directly above) and After
// (directly below); with neither it goes last.
type PageMove struct {
	Parent   string
	Before   string
	After    string
	Revision int64
}

func pageParent(n *domain.Note) string {
	p, _ := n.Frontmatter["parent"].(string)
	return p
}

func pageRank(n *domain.Note) string {
	r, _ := n.Frontmatter["rank"].(string)
	return r
}

// sortPages orders siblings: ranked pages first, the rest by title, the way
// documents written before the tree existed have always read.
func sortPages(list []*domain.Note) {
	sort.SliceStable(list, func(i, j int) bool {
		a, b := pageRank(list[i]), pageRank(list[j])
		if (a == "") != (b == "") {
			return a != ""
		}
		if a != b {
			return a < b
		}
		return strings.ToLower(list[i].Title) < strings.ToLower(list[j].Title)
	})
}

// pages lists documents under parent; "" means the top level.
func (s *Notes) pages(ctx context.Context, parent string) ([]*domain.Note, error) {
	list, _, err := s.store.Notes(ctx, domain.NoteQuery{ContentKind: "document", Parent: parent})
	if err != nil {
		return nil, err
	}
	if parent == "" {
		top := list[:0:0]
		for _, n := range list {
			if pageParent(n) == "" {
				top = append(top, n)
			}
		}
		list = top
	}
	sortPages(list)
	return list, nil
}

// lastPageRank is the rank of a new page at the end of its siblings. It stays
// empty among unranked siblings: a rank would lift the page above them all.
func (s *Notes) lastPageRank(ctx context.Context, parent string) (string, error) {
	sib, err := s.pages(ctx, parent)
	if err != nil {
		return "", err
	}
	if len(sib) == 0 {
		return markdown.RankBetween("", ""), nil
	}
	last := pageRank(sib[len(sib)-1])
	if last == "" {
		return "", nil
	}
	return markdown.RankBetween(last, ""), nil
}

// MovePage changes a page's parent and position.
func (s *Notes) MovePage(ctx context.Context, id string, in PageMove) (*domain.Note, error) {
	vaultWrites.Lock()
	defer vaultWrites.Unlock()
	n, err := s.store.Note(ctx, id)
	if err != nil {
		return nil, err
	}
	if k, _ := n.Frontmatter["content_kind"].(string); k != "document" {
		return nil, fmt.Errorf("%w: в дереве страниц только документы", ErrInvalid)
	}
	if err := s.policy.CheckNote(ctx, n, policy.RoleMember); err != nil {
		return nil, err
	}
	if in.Revision != 0 && in.Revision != n.Revision {
		return nil, fmt.Errorf("%w: have %d, want %d", ErrConflict, n.Revision, in.Revision)
	}
	parent, err := s.canonicalParent(ctx, in.Parent)
	if err != nil {
		return nil, err
	}
	if err := s.checkPageParent(ctx, n, parent); err != nil {
		return nil, err
	}
	if parent != pageParent(n) {
		if err := s.policy.CheckCreate(ctx, n.Path, parent); err != nil {
			return nil, err
		}
	}

	all, err := s.pages(ctx, parent)
	if err != nil {
		return nil, err
	}
	sib := make([]*domain.Note, 0, len(all))
	for _, o := range all {
		if o.Path != n.Path {
			sib = append(sib, o)
		}
	}
	at := len(sib)
	for i, o := range sib {
		if in.After != "" && o.ID == in.After {
			at = i
			break
		}
		if in.Before != "" && o.ID == in.Before {
			at = i + 1
		}
	}
	var prev, next string
	if at > 0 {
		prev = pageRank(sib[at-1])
	}
	if at < len(sib) {
		next = pageRank(sib[at])
	}
	rank := ""
	clean := (at == 0 || prev != "") && (at == len(sib) || next != "") && (prev == "" || next == "" || prev < next)
	if clean {
		rank = markdown.RankBetween(prev, next)
	} else {
		// Neighbours without a usable rank: give the whole level one, once.
		rank, err = s.renumber(ctx, sib, at)
		if err != nil {
			return nil, err
		}
	}
	if err := s.patchFrontmatter(ctx, n.Path, map[string]string{"parent": parent, "rank": rank}); err != nil {
		return nil, err
	}
	return s.store.Note(ctx, n.Path)
}

// renumber writes evenly spread ranks to siblings, leaving a slot at position at
// and returning the rank for it.
func (s *Notes) renumber(ctx context.Context, sib []*domain.Note, at int) (string, error) {
	rank, slot := "", ""
	for i := 0; i <= len(sib); i++ {
		rank = markdown.RankBetween(rank, "")
		if i == at {
			slot = rank
			rank = markdown.RankBetween(rank, "")
		}
		if i == len(sib) {
			break
		}
		if pageRank(sib[i]) != rank {
			if err := s.patchFrontmatter(ctx, sib[i].Path, map[string]string{"rank": rank}); err != nil {
				return "", err
			}
		}
	}
	return slot, nil
}

func (s *Notes) patchFrontmatter(ctx context.Context, rel string, kv map[string]string) error {
	data, err := s.vault.Read(rel)
	if err != nil {
		return err
	}
	eol, lines := splitLines(data)
	for _, k := range sortedKeys(kv) {
		lines = markdown.SetFrontmatterValue(lines, k, kv[k])
	}
	if err := s.vault.Write(rel, []byte(strings.Join(lines, eol))); err != nil {
		return err
	}
	s.ix.Forget(rel)
	return s.ix.ReindexPath(ctx, rel)
}

// canonicalParent resolves a parent reference to its stable form, as create does.
func (s *Notes) canonicalParent(ctx context.Context, ref string) (string, error) {
	if ref == "" {
		return "", nil
	}
	kind, id, ok := strings.Cut(ref, ":")
	if !ok || id == "" {
		return "", fmt.Errorf("%w: invalid parent", ErrInvalid)
	}
	switch kind {
	case "note":
		p, err := s.store.Note(ctx, id)
		if err != nil {
			return "", err
		}
		return "note:" + p.ID, nil
	case "task":
		t, err := s.store.Task(ctx, id)
		if err != nil {
			return "", err
		}
		if t.BlockID == "" {
			return "", fmt.Errorf("%w: task requires a stable ID", ErrInvalid)
		}
		return "task:" + t.ID, nil
	}
	return "", fmt.Errorf("%w: invalid parent kind", ErrInvalid)
}

// checkPageParent refuses a cycle and a tree deeper than access checks can follow.
func (s *Notes) checkPageParent(ctx context.Context, n *domain.Note, parent string) error {
	depth := 1
	seen := map[string]bool{}
	for ref := parent; ref != ""; {
		kind, id, _ := strings.Cut(ref, ":")
		if kind != "note" {
			depth++
			break
		}
		p, err := s.store.Note(ctx, id)
		if err != nil {
			break // a hidden or deleted ancestor cannot contain this page
		}
		if p.Path == n.Path {
			return fmt.Errorf("%w: страницу нельзя вложить в её же подстраницу", ErrInvalid)
		}
		if seen[p.Path] {
			break
		}
		seen[p.Path] = true
		depth++
		ref = pageParent(p)
	}
	h, err := s.pageHeight(ctx, n, map[string]bool{})
	if err != nil {
		return err
	}
	if depth+h > maxPageDepth {
		return fmt.Errorf("%w: вложенность страниц — не больше %d уровней", ErrInvalid, maxPageDepth)
	}
	return nil
}

func (s *Notes) pageHeight(ctx context.Context, n *domain.Note, seen map[string]bool) (int, error) {
	seen[n.Path] = true
	kids, _, err := s.store.Notes(ctx, domain.NoteQuery{Parent: "note:" + n.ID})
	if err != nil {
		return 0, err
	}
	best := 0
	for _, k := range kids {
		if seen[k.Path] {
			continue
		}
		h, err := s.pageHeight(ctx, k, seen)
		if err != nil {
			return 0, err
		}
		if h+1 > best {
			best = h + 1
		}
	}
	return best, nil
}

// checkNoSubprojects refuses to delete a project with subprojects: they inherit
// its access list, and without it a closed subtree would open up.
func (s *Notes) checkNoSubprojects(ctx context.Context, n *domain.Note) error {
	tp, ok := markdown.ParseProject(n)
	if !ok {
		return nil
	}
	var parents map[string]string
	if s.policy != nil {
		var err error
		if parents, err = s.policy.ProjectParents(ctx); err != nil {
			return err
		}
	} else {
		parents = map[string]string{}
		notes, _, err := s.store.Notes(ctx, domain.NoteQuery{Type: domain.TypeProject})
		if err != nil {
			return err
		}
		for _, o := range notes {
			if p, ok := markdown.ParseProject(o); ok {
				parents[p.Key] = p.Parent
			}
		}
	}
	for child, up := range parents {
		if up == tp.Key {
			return fmt.Errorf("%w: сначала перенесите подпроект %s", ErrInvalid, child)
		}
	}
	return nil
}

// reparentChildren hands the notes under a removed note to a new parent. Callers
// hold vaultWrites. It reads through store, so a child hidden from the caller keeps
// its link and is protected by its own project.
func reparentChildren(ctx context.Context, v *vault.Vault, ix *Indexer, store index.Store, id, newParent string) error {
	kids, _, err := store.Notes(ctx, domain.NoteQuery{Parent: "note:" + id})
	if err != nil {
		return err
	}
	for _, k := range kids {
		data, err := v.Read(k.Path)
		if err != nil {
			return err
		}
		eol, lines := splitLines(data)
		lines = markdown.SetFrontmatterValue(lines, "parent", newParent)
		if err := v.Write(k.Path, []byte(strings.Join(lines, eol))); err != nil {
			return err
		}
		ix.Forget(k.Path)
		if err := ix.ReindexPath(ctx, k.Path); err != nil {
			return err
		}
	}
	return nil
}
