import { expect, test } from "vitest";
import { ancestorsOf, buildTree, comparePages, descendantsOf, flattenTree, placement } from "./tree";

type N = { id: string; parent?: string; title: string; rank?: string };
const tree = (items: N[]) => buildTree(items, (n) => n.id, (n) => n.parent, comparePages);
const shape = (nodes: ReturnType<typeof tree>): unknown => nodes.map((n) => (n.children.length ? [n.item.id, shape(n.children)] : n.item.id));

test("nests by parent and orders siblings: ranked first, then by title", () => {
  const t = tree([
    { id: "b", title: "Б" }, { id: "a", title: "А" }, { id: "r", title: "Я", rank: "i" },
    { id: "c", parent: "a", title: "В" },
  ]);
  expect(shape(t)).toEqual(["r", ["a", ["c"]], "b"]);
  expect(t[1].children[0].depth).toBe(1);
});

test("an unknown parent makes a root, and a cycle still shows every item once", () => {
  const t = tree([
    { id: "x", parent: "hidden", title: "x" },
    { id: "p", parent: "q", title: "p" }, { id: "q", parent: "p", title: "q" },
    { id: "s", parent: "s", title: "s" },
  ]);
  const ids = flattenTree(t, new Set(), (n) => n.id).map((n) => n.item.id).sort();
  expect(ids).toEqual(["p", "q", "s", "x"]);
});

test("a collapsed branch hides what is under it", () => {
  const t = tree([{ id: "a", title: "a" }, { id: "b", parent: "a", title: "b" }, { id: "c", parent: "b", title: "c" }]);
  expect(flattenTree(t, new Set(["b"]), (n) => n.id).map((n) => n.item.id)).toEqual(["a", "b"]);
});

test("ancestors run from the top, and a cycle ends the walk", () => {
  const all = [{ key: "A" }, { key: "B", parent: "A" }, { key: "C", parent: "B" }];
  expect(ancestorsOf(all, "B").map((p) => p.key)).toEqual(["A", "B"]);
  expect(ancestorsOf([{ key: "P", parent: "Q" }, { key: "Q", parent: "P" }], "P").length).toBe(2);
});

test("descendants include the node itself", () => {
  const items = [{ id: "a" }, { id: "b", parent: "a" }, { id: "c", parent: "b" }, { id: "d" }] as { id: string; parent?: string }[];
  expect([...descendantsOf(items, "a", (i) => i.id, (i) => i.parent)].sort()).toEqual(["a", "b", "c"]);
});

test("placement names the neighbours, ignoring the dragged page", () => {
  const level = ["a", "b", "c"];
  expect(placement("c", "b", "before", "note:p", level, [])).toEqual({ parent: "note:p", before: "a", after: "b" });
  expect(placement("a", "b", "after", undefined, level, [])).toEqual({ parent: "", before: "b", after: "c" });
  expect(placement("b", "a", "after", undefined, level, [])).toEqual({ parent: "", before: "a", after: "c" });
  expect(placement("a", "c", "inside", undefined, level, ["x", "y"])).toEqual({ parent: "note:c", before: "y" });
});
