import { expect, test } from "vitest";
import type { Issue, TrackerProject } from "../api/types";
import { OTHER, applyMove, canDrop, layoutBoard, planMove } from "./board";

const project: TrackerProject = {
  key: "WEB", title: "Сайт", note_id: "p", path: "p.md", issue_seq: 0, revision: 1, issue_types: ["task"],
  transitions: { todo: ["doing"], doing: ["todo", "done"] },
  workflow: [
    { id: "todo", name: "К работе", category: "todo" },
    { id: "doing", name: "В работе", category: "in_progress", wip: 2 },
    { id: "done", name: "Готово", category: "done" },
  ],
};
const is = (key: string, status: string, extra: Partial<Issue> = {}): Issue =>
  ({ key, project: "WEB", path: key, title: key, issue_type: "task", status, start: null, due: null, revision: 1, ...extra });
const keys = (list: Issue[]) => list.map((i) => i.key).join(",");

test("layout keeps rank order per column and collects unknown statuses", () => {
  const lanes = layoutBoard(project, [is("A", "todo"), is("B", "doing"), is("C", "todo"), is("D", "archived")], "none");
  expect(lanes).toHaveLength(1);
  expect(keys(lanes[0].columns.get("todo")!)).toBe("A,C");
  expect(keys(lanes[0].columns.get(OTHER)!)).toBe("D");
});

test("assignee lanes are sorted with the unassigned lane last", () => {
  const lanes = layoutBoard(project, [is("A", "todo"), is("B", "todo", { assignee: "bob" }), is("C", "doing", { assignee: "alice" })], "assignee");
  expect(lanes.map((l) => l.label)).toEqual(["alice", "bob", "Не назначены"]);
});

test("planMove names the neighbours around the drop point", () => {
  const cell = [is("A", "doing"), is("B", "doing"), is("C", "doing")];
  expect(planMove(cell, is("X", "todo"), "doing", 0)).toEqual({ key: "X", status: "doing", before: undefined, after: "A" });
  expect(planMove(cell, is("X", "todo"), "doing", 2)).toEqual({ key: "X", status: "doing", before: "B", after: "C" });
  expect(planMove(cell, is("X", "todo"), "doing", 9)).toEqual({ key: "X", status: "doing", before: "C", after: undefined });
  // Moving C to the top of its own column: neighbours exclude C itself.
  expect(planMove(cell, cell[2], "doing", 0)).toEqual({ key: "C", status: "doing", before: undefined, after: "A" });
  // Dropping a card back where it was is not a move.
  expect(planMove(cell, cell[1], "doing", 1)).toBeNull();
});

test("applyMove reorders the list like the server will", () => {
  const list = [is("A", "todo"), is("B", "doing"), is("C", "doing")];
  expect(keys(applyMove(list, { key: "A", status: "doing", before: "B", after: "C" }))).toBe("B,A,C");
  expect(keys(applyMove(list, { key: "C", status: "doing", after: "B" }))).toBe("A,C,B");
  expect(applyMove(list, { key: "A", status: "doing" }, "in_progress").find((i) => i.key === "A")?.status_category).toBe("in_progress");
});

test("canDrop follows workflow transitions", () => {
  expect(canDrop(project, "todo", "doing")).toBe(true);
  expect(canDrop(project, "todo", "done")).toBe(false);
  expect(canDrop(project, "todo", "todo")).toBe(true);
  expect(canDrop(project, "todo", OTHER)).toBe(false);
  expect(canDrop({ ...project, transitions: undefined }, "todo", "done")).toBe(true);
});
