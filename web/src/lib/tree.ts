import { useCallback, useEffect, useState } from "react";

export type TreeNode<T> = { item: T; depth: number; children: TreeNode<T>[] };

/**
 * buildTree nests items by their parent id. An unknown parent (hidden or deleted)
 * makes an item a root. So does a cycle, which only a hand edit of the files can
 * make: every item is shown exactly once, whatever the data says.
 */
export function buildTree<T>(
  items: T[], id: (t: T) => string, parent: (t: T) => string | undefined, compare?: (a: T, b: T) => number,
): TreeNode<T>[] {
  const known = new Set(items.map(id));
  const kids = new Map<string, T[]>();
  const roots: T[] = [];
  for (const it of items) {
    const p = parent(it);
    if (p && known.has(p) && p !== id(it)) kids.set(p, [...(kids.get(p) ?? []), it]);
    else roots.push(it);
  }
  const seen = new Set<string>();
  const grow = (list: T[], depth: number): TreeNode<T>[] => {
    const sorted = compare ? [...list].sort(compare) : list;
    return sorted.filter((it) => !seen.has(id(it))).map((it) => {
      seen.add(id(it));
      return { item: it, depth, children: grow(kids.get(id(it)) ?? [], depth + 1) };
    });
  };
  const out = grow(roots, 0);
  for (const it of items) {
    if (!seen.has(id(it))) out.push(...grow([it], 0));
  }
  return out;
}

/** flattenTree lists what is on screen: children of collapsed nodes are left out. */
export function flattenTree<T>(nodes: TreeNode<T>[], collapsed: Set<string>, id: (t: T) => string): TreeNode<T>[] {
  const out: TreeNode<T>[] = [];
  const walk = (list: TreeNode<T>[]) => {
    for (const n of list) {
      out.push(n);
      if (!collapsed.has(id(n.item))) walk(n.children);
    }
  };
  walk(nodes);
  return out;
}

/** ancestorsOf walks up parent keys, nearest last. A cycle ends the walk. */
export function ancestorsOf<T extends { key: string; parent?: string }>(all: T[], parent?: string): T[] {
  const byKey = new Map(all.map((p) => [p.key, p]));
  const out: T[] = [];
  const seen = new Set<string>();
  for (let k = parent; k && !seen.has(k); k = byKey.get(k)?.parent) {
    seen.add(k);
    const p = byKey.get(k);
    if (!p) break;
    out.unshift(p);
  }
  return out;
}

/** descendantsOf is a node and all under it: what a move must not target. */
export function descendantsOf<T>(items: T[], root: string, id: (t: T) => string, parent: (t: T) => string | undefined): Set<string> {
  const out = new Set([root]);
  for (let grew = true; grew;) {
    grew = false;
    for (const it of items) {
      const p = parent(it);
      if (p && out.has(p) && !out.has(id(it))) { out.add(id(it)); grew = true; }
    }
  }
  return out;
}

type Ranked = { rank?: string; title: string };

/** comparePages is the server's sibling order: ranked first, the rest by title. */
export function comparePages(a: Ranked, b: Ranked): number {
  if (!a.rank !== !b.rank) return a.rank ? -1 : 1;
  if (a.rank && b.rank && a.rank !== b.rank) return a.rank < b.rank ? -1 : 1;
  return a.title.localeCompare(b.title, "ru");
}

export type DropZone = "before" | "inside" | "after";

/**
 * placement turns "drop this next to that" into the move request. siblings are the
 * target's level in order; the dragged page is ignored wherever it stands.
 */
export function placement(
  drag: string, target: string, zone: DropZone, targetParent: string | undefined,
  siblings: string[], targetChildren: string[],
): { parent: string; before?: string; after?: string } {
  if (zone === "inside") {
    const kids = targetChildren.filter((k) => k !== drag);
    return { parent: `note:${target}`, before: kids[kids.length - 1] };
  }
  const level = siblings.filter((s) => s !== drag);
  const at = level.indexOf(target);
  return zone === "before"
    ? { parent: targetParent ?? "", before: level[at - 1], after: target }
    : { parent: targetParent ?? "", before: target, after: level[at + 1] };
}

/**
 * useCollapsed remembers closed branches in this browser. It loads after mount, so
 * the static page and the first client render agree.
 */
export function useCollapsed(storageKey: string) {
  const [collapsed, setCollapsed] = useState<Set<string>>(() => new Set());
  useEffect(() => {
    try {
      const raw = localStorage.getItem(storageKey);
      if (raw) setCollapsed(new Set(JSON.parse(raw) as string[]));
    } catch { /* Storage is optional. */ }
  }, [storageKey]);
  const toggle = useCallback((id: string, open?: boolean) => {
    setCollapsed((cur) => {
      const next = new Set(cur);
      if (open ?? next.has(id)) next.delete(id);
      else next.add(id);
      try { localStorage.setItem(storageKey, JSON.stringify([...next])); } catch { /* Storage is optional. */ }
      return next;
    });
  }, [storageKey]);
  return { collapsed, toggle };
}
