import type { Issue } from "../api/types";
import { addDays, iso, parseDate, startOfWeek } from "./date";

/** Pure timeline layout and date arithmetic, kept apart from the page for tests. */

export type Zoom = "day" | "week" | "month";
export const DAY_PX: Record<Zoom, number> = { day: 36, week: 16, month: 5 };

export type Span = { start: Date; end: Date };
export type Dates = { start: string | null; due: string | null };
export type DragMode = "move" | "start" | "end";

const dayOf = (value: string) => parseDate(value.slice(0, 10));

export function daysBetween(a: Date, b: Date): number {
  return Math.round((b.getTime() - a.getTime()) / 86_400_000);
}

/**
 * spanOf is the bar of an issue. One date gives a one-day bar; dates written
 * in the wrong order by hand are shown in the right order.
 */
export function spanOf(i: Dates): Span | null {
  const s = i.start ? dayOf(i.start) : null;
  const d = i.due ? dayOf(i.due) : null;
  const a = s ?? d;
  const b = d ?? s;
  if (!a || !b) return null;
  return a <= b ? { start: a, end: b } : { start: b, end: a };
}

/** timelineRange covers every bar and today, from a Monday to a Sunday, with room to drag. */
export function timelineRange(issues: Dates[], now: Date): Span {
  let start = now;
  let end = now;
  for (const i of issues) {
    const s = spanOf(i);
    if (!s) continue;
    if (s.start < start) start = s.start;
    if (s.end > end) end = s.end;
  }
  const first = startOfWeek(addDays(start, -7));
  const last = addDays(startOfWeek(addDays(end, 14)), 6);
  const minLast = addDays(first, 7 * 6 - 1);
  return { start: first, end: last > minLast ? last : minLast };
}

/** shift moves a stored date by whole days, keeping a time of day if it has one. */
function shift(value: string, days: number): string {
  return iso(addDays(dayOf(value), days)) + value.slice(10);
}

// A stored value is kept as written while its day still is the wanted one.
const keep = (value: string | null, day: Date) => (value && iso(dayOf(value)) === iso(day) ? value : iso(day));

/**
 * dragDates applies a drag of delta days. Moving a bar shifts the dates the
 * issue has. Resizing writes what the bar then shows, so pulling the edge of
 * a one-date bar gives the issue its second date. An edge stops at the other.
 */
export function dragDates(i: Dates, mode: DragMode, delta: number): Dates {
  const span = spanOf(i);
  if (!span || delta === 0) return { start: i.start, due: i.due };
  if (mode === "move") {
    return { start: i.start && shift(i.start, delta), due: i.due && shift(i.due, delta) };
  }
  const length = daysBetween(span.start, span.end);
  const d = mode === "start" ? Math.min(delta, length) : Math.max(delta, -length);
  if (d === 0) return { start: i.start, due: i.due };
  return mode === "start"
    ? { start: iso(addDays(span.start, d)), due: keep(i.due, span.end) }
    : { start: keep(i.start, span.start), due: iso(addDays(span.end, d)) };
}

/** clampDelta keeps the dragged part of a bar inside the visible range. */
export function clampDelta(span: Span, mode: DragMode, delta: number, range: Span): number {
  const min = mode === "end" ? -Infinity : daysBetween(span.start, range.start);
  const max = mode === "start" ? Infinity : daysBetween(span.end, range.end);
  return Math.max(min, Math.min(max, delta));
}

/** drawnDates gives an undated issue the days between two points of its row. */
export function drawnDates(a: Date, b: Date): Dates {
  return a <= b ? { start: iso(a), due: iso(b) } : { start: iso(b), due: iso(a) };
}

/** datesPatch holds only the fields that change, or null if none does. */
export function datesPatch(i: Dates, next: Dates): { start?: string; due?: string } | null {
  const patch: { start?: string; due?: string } = {};
  if (next.start !== i.start) patch.start = next.start ?? "";
  if (next.due !== i.due) patch.due = next.due ?? "";
  return Object.keys(patch).length ? patch : null;
}

export type Row =
  | { kind: "epic"; id: string; label: string; issue?: Issue; children: string[] }
  | { kind: "issue"; id: string; issue: Issue }
  | { kind: "section"; id: string; label: string };

export const NO_EPIC = "__no_epic__";
export const NO_DATES = "__no_dates__";

/**
 * timelineRows groups dated issues under their epics, each group in rank
 * order, so a row does not jump when its dates change. Undated issues wait
 * in a last section, where a date can be drawn on their row. An epic filtered
 * out of the list still heads its issues, by key.
 */
export function timelineRows(issues: Issue[]): Row[] {
  const groups = new Map<string, Issue[]>();
  const add = (id: string, is: Issue) => groups.set(id, [...(groups.get(id) ?? []), is]);
  const undated: Issue[] = [];
  for (const is of issues) {
    if (is.issue_type === "epic") continue;
    if (spanOf(is)) add(is.epic || NO_EPIC, is);
    else undated.push(is);
  }

  const rows: Row[] = [];
  const group = (id: string, label: string, epic?: Issue) => {
    const children = groups.get(id) ?? [];
    rows.push({ kind: "epic", id: `epic:${id}`, label, issue: epic, children: children.map((c) => c.key) });
    for (const is of children) rows.push({ kind: "issue", id: is.key, issue: is });
    groups.delete(id);
  };
  for (const is of issues) {
    if (is.issue_type !== "epic") continue;
    if (spanOf(is) || groups.has(is.key)) group(is.key, is.title, is);
    else undated.push(is);
  }
  const noEpic = groups.has(NO_EPIC);
  for (const id of [...groups.keys()]) if (id !== NO_EPIC) group(id, id);
  if (noEpic) group(NO_EPIC, "Без эпика");

  if (undated.length) {
    rows.push({ kind: "section", id: NO_DATES, label: "Без дат" });
    for (const is of undated) rows.push({ kind: "issue", id: is.key, issue: is });
  }
  return rows;
}

/** epicSpan is what an epic shows: its own dates, or else the reach of its issues. */
export function epicSpan(row: Extract<Row, { kind: "epic" }>, spans: Map<string, Span>): Span | null {
  if (row.issue && spans.has(row.issue.key)) return spans.get(row.issue.key)!;
  const reach = row.children.flatMap((key) => spans.get(key) ?? []);
  if (!reach.length) return null;
  return reach.reduce((a, s) => ({ start: s.start < a.start ? s.start : a.start, end: s.end > a.end ? s.end : a.end }));
}

export type Edge = { from: string; to: string; broken: boolean };

/**
 * dependencyEdges lists blocks links between issues that have a bar here.
 * A link is broken when the blocked issue starts before its blocker ends.
 * The same link written on both issues counts once.
 */
export function dependencyEdges(issues: Issue[], spans: Map<string, Span>): Edge[] {
  const out = new Map<string, Edge>();
  const add = (from: string, to: string) => {
    const a = spans.get(from);
    const b = spans.get(to);
    if (a && b && from !== to) out.set(`${from}>${to}`, { from, to, broken: b.start < a.end });
  };
  for (const is of issues) {
    for (const l of is.links ?? []) {
      if (l.type === "blocks") add(is.key, l.target);
      if (l.type === "blocked-by") add(l.target, is.key);
    }
  }
  return [...out.values()];
}

/** edgePath draws a right-angled route from a bar's end to the next bar's start. */
export function edgePath(x1: number, y1: number, x2: number, y2: number, gap = 8): string {
  if (x2 >= x1 + gap * 2) return `M${x1} ${y1}H${x1 + gap}V${y2}H${x2}`;
  const mid = y1 === y2 ? y1 - 14 : (y1 + y2) / 2;
  return `M${x1} ${y1}H${x1 + gap}V${mid}H${x2 - gap}V${y2}H${x2}`;
}

export type Tick = { id: string; label: string; left: number; width: number };

const MONTHS = ["янв", "фев", "мар", "апр", "май", "июн", "июл", "авг", "сен", "окт", "ноя", "дек"];

/** monthTicks are the upper header's segments, cut at the edges of the range. */
export function monthTicks(range: Span, dayPx: number): Tick[] {
  const out: Tick[] = [];
  let from = range.start;
  while (from <= range.end) {
    const next = new Date(from.getFullYear(), from.getMonth() + 1, 1);
    const to = next > range.end ? addDays(range.end, 1) : next;
    out.push({
      id: iso(from),
      label: `${MONTHS[from.getMonth()]} ${from.getFullYear()}`,
      left: daysBetween(range.start, from) * dayPx,
      width: daysBetween(from, to) * dayPx,
    });
    from = next;
  }
  return out;
}

/** dayTicks are the lower header: every day when zoomed in, otherwise Mondays. */
export function dayTicks(range: Span, zoom: Zoom): Tick[] {
  const dayPx = DAY_PX[zoom];
  const step = zoom === "day" ? 1 : 7;
  const out: Tick[] = [];
  for (let d = range.start; d <= range.end; d = addDays(d, step)) {
    out.push({ id: iso(d), label: String(d.getDate()), left: daysBetween(range.start, d) * dayPx, width: step * dayPx });
  }
  return out;
}
