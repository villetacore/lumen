import type { Issue, TrackerProject, WorkflowStatus } from "../api/types";

/** Pure board layout and move arithmetic, kept apart from the page for tests. */

export type LaneBy = "none" | "assignee" | "epic";

export type Lane = { id: string; label: string; columns: Map<string, Issue[]> };

/** OTHER collects issues whose status is not a column of the workflow. */
export const OTHER = "__other__";

export function boardColumns(p: TrackerProject, issues: Issue[]): WorkflowStatus[] {
  const known = new Set(p.workflow.map((s) => s.id));
  const strays = issues.some((i) => !known.has(i.status));
  return strays ? [...p.workflow, { id: OTHER, name: "Другие статусы", category: "todo" }] : p.workflow;
}

export function columnOf(p: TrackerProject, issue: Issue): string {
  return p.workflow.some((s) => s.id === issue.status) ? issue.status : OTHER;
}

export function laneOf(issue: Issue, by: LaneBy): string {
  if (by === "assignee") return issue.assignee ?? "";
  if (by === "epic") return issue.epic ?? "";
  return "";
}

/**
 * layoutBoard splits issues (already in rank order) into lanes and columns.
 * Lanes are sorted by name with the empty lane ("not assigned") last.
 */
export function layoutBoard(p: TrackerProject, issues: Issue[], by: LaneBy): Lane[] {
  const cols = boardColumns(p, issues);
  const lanes = new Map<string, Lane>();
  const lane = (id: string) => {
    let l = lanes.get(id);
    if (!l) {
      l = { id, label: laneLabel(id, by), columns: new Map(cols.map((c) => [c.id, [] as Issue[]])) };
      lanes.set(id, l);
    }
    return l;
  };
  if (by === "none") lane("");
  for (const is of issues) lane(laneOf(is, by)).columns.get(columnOf(p, is))!.push(is);
  return [...lanes.values()].sort((a, b) =>
    a.id === "" ? 1 : b.id === "" ? -1 : a.label.localeCompare(b.label, "ru"));
}

function laneLabel(id: string, by: LaneBy): string {
  if (by === "assignee") return id || "Не назначены";
  if (by === "epic") return id || "Без эпика";
  return "";
}

export type Move = { key: string; status: string; before?: string; after?: string };

/**
 * planMove turns "drop issue at position index of this column cell" into the
 * API move: the new status and the keys of the neighbours above and below.
 * index counts positions in the cell without the dragged card. Returns null
 * when nothing would change.
 */
export function planMove(cell: Issue[], dragged: Issue, status: string, index: number): Move | null {
  const others = cell.filter((i) => i.key !== dragged.key);
  const at = Math.max(0, Math.min(index, others.length));
  const before = others[at - 1]?.key;
  const after = others[at]?.key;
  if (status === dragged.status) {
    const was = cell.findIndex((i) => i.key === dragged.key);
    if (was === at) return null;
  }
  return { key: dragged.key, status, before, after };
}

/** applyMove reorders a rank-ordered list locally, for an optimistic update. */
export function applyMove(issues: Issue[], move: Move, category?: Issue["status_category"]): Issue[] {
  const moving = issues.find((i) => i.key === move.key);
  if (!moving) return issues;
  const moved: Issue = { ...moving, status: move.status, status_category: category ?? moving.status_category };
  const rest = issues.filter((i) => i.key !== move.key);
  let at = rest.length;
  if (move.after) at = rest.findIndex((i) => i.key === move.after);
  else if (move.before) at = rest.findIndex((i) => i.key === move.before) + 1;
  if (at < 0) at = rest.length;
  return [...rest.slice(0, at), moved, ...rest.slice(at)];
}

/** canDrop mirrors the server's transition rule for highlighting columns. */
export function canDrop(p: TrackerProject, from: string, to: string): boolean {
  if (to === OTHER) return false;
  if (from === to) return true;
  const allowed = p.transitions?.[from];
  return !allowed || allowed.includes(to);
}
