import { expect, test } from "vitest";
import type { Issue } from "../api/types";
import { parseDate } from "./date";
import {
  type Row, type Span, clampDelta, datesPatch, dayTicks, dependencyEdges, dragDates, drawnDates, epicSpan, monthTicks, spanOf, timelineRange, timelineRows,
} from "./timeline";

const d = parseDate;
const is = (key: string, extra: Partial<Issue> = {}): Issue =>
  ({ key, project: "WEB", path: key, title: key, issue_type: "task", status: "todo", start: null, due: null, revision: 1, ...extra });
const ids = (rows: Row[]) => rows.map((r) => r.id).join(",");
const spansOf = (list: Issue[]) => new Map(list.flatMap((i): [string, Span][] => { const s = spanOf(i); return s ? [[i.key, s]] : []; }));

test("a bar comes from one date, two dates, or hand-swapped dates", () => {
  expect(spanOf({ start: null, due: null })).toBeNull();
  expect(spanOf({ start: null, due: "2026-11-10" })).toEqual({ start: d("2026-11-10"), end: d("2026-11-10") });
  expect(spanOf({ start: "2026-11-12", due: "2026-11-10" })).toEqual({ start: d("2026-11-10"), end: d("2026-11-12") });
});

test("moving shifts only the dates the issue has and keeps a time of day", () => {
  expect(dragDates({ start: "2026-11-02", due: "2026-11-05" }, "move", 3)).toEqual({ start: "2026-11-05", due: "2026-11-08" });
  expect(dragDates({ start: null, due: "2026-11-05" }, "move", -1)).toEqual({ start: null, due: "2026-11-04" });
  expect(dragDates({ start: "2026-11-02", due: "2026-11-05T15:00" }, "move", 1)).toEqual({ start: "2026-11-03", due: "2026-11-06T15:00" });
});

test("moving across a month end and a clock change lands on whole days", () => {
  expect(dragDates({ start: "2026-10-24", due: "2026-10-25" }, "move", 8)).toEqual({ start: "2026-11-01", due: "2026-11-02" });
  expect(dragDates({ start: "2026-03-28", due: "2026-03-28" }, "move", 2)).toEqual({ start: "2026-03-30", due: "2026-03-30" });
});

test("an edge stops at the other edge", () => {
  const i = { start: "2026-11-02", due: "2026-11-05" };
  expect(dragDates(i, "start", 2)).toEqual({ start: "2026-11-04", due: "2026-11-05" });
  expect(dragDates(i, "start", 99)).toEqual({ start: "2026-11-05", due: "2026-11-05" });
  expect(dragDates(i, "end", -99)).toEqual({ start: "2026-11-02", due: "2026-11-02" });
  expect(dragDates({ start: "2026-11-02", due: "2026-11-05T15:00" }, "start", 1)).toEqual({ start: "2026-11-03", due: "2026-11-05T15:00" });
});

test("resizing a one-date bar writes what the bar then shows", () => {
  expect(dragDates({ start: null, due: "2026-11-05" }, "start", -2)).toEqual({ start: "2026-11-03", due: "2026-11-05" });
  expect(dragDates({ start: "2026-11-05", due: null }, "end", 2)).toEqual({ start: "2026-11-05", due: "2026-11-07" });
  expect(dragDates({ start: null, due: "2026-11-05" }, "start", 2)).toEqual({ start: null, due: "2026-11-05" });
});

test("a patch names only the changed dates", () => {
  const i = { start: "2026-11-02", due: "2026-11-05" };
  expect(datesPatch(i, dragDates(i, "end", 1))).toEqual({ due: "2026-11-06" });
  expect(datesPatch(i, dragDates(i, "move", 0))).toBeNull();
  expect(datesPatch(i, dragDates(i, "move", 1))).toEqual({ start: "2026-11-03", due: "2026-11-06" });
});

test("a drawn range is ordered whichever way it was drawn", () => {
  expect(drawnDates(d("2026-11-05"), d("2026-11-02"))).toEqual({ start: "2026-11-02", due: "2026-11-05" });
});

test("range spans Monday to Sunday, covers bars and today, and limits a drag", () => {
  const r = timelineRange([{ start: "2026-09-16", due: "2026-09-20" }, { start: null, due: null }], d("2026-11-04"));
  expect(r.start).toEqual(d("2026-09-07"));
  expect(r.start.getDay()).toBe(1);
  expect(r.end.getDay()).toBe(0);
  expect(r.end >= d("2026-11-18")).toBe(true);
  const span = { start: d("2026-09-10"), end: d("2026-09-12") };
  expect(clampDelta(span, "move", -100, r)).toBe(-3);
  expect(clampDelta(span, "start", 50, r)).toBe(50);
});

test("rows group dated issues by epic in rank order; undated wait at the end", () => {
  const rows = timelineRows([
    is("W-9", { due: "2026-11-09" }),
    is("W-1", { issue_type: "epic", title: "Эпик" }),
    is("W-3", { epic: "W-1", due: "2026-11-20" }),
    is("W-2", { epic: "W-1", due: "2026-11-01" }),
    is("W-4", { epic: "W-77", start: "2026-11-01" }),
    is("W-5"),
    is("W-6", { issue_type: "epic", title: "Пустой" }),
    is("W-7", { issue_type: "epic", title: "С датами", start: "2026-11-01", due: "2026-11-02" }),
  ]);
  expect(ids(rows)).toBe("epic:W-1,W-3,W-2,epic:W-7,epic:W-77,W-4,epic:__no_epic__,W-9,__no_dates__,W-5,W-6");
});

test("an epic without dates reaches over its issues; its own dates win", () => {
  const list = [is("A", { start: "2026-11-02", due: "2026-11-03" }), is("B", { due: "2026-11-09" })];
  const spans = spansOf(list);
  const row: Extract<Row, { kind: "epic" }> = { kind: "epic", id: "epic:E", label: "E", children: ["A", "B"] };
  expect(epicSpan(row, spans)).toEqual({ start: d("2026-11-02"), end: d("2026-11-09") });
  expect(epicSpan({ ...row, children: [] }, spans)).toBeNull();
  const own = is("E", { start: "2026-12-01", due: "2026-12-02" });
  expect(epicSpan({ ...row, issue: own }, spansOf([...list, own]))).toEqual(spanOf(own));
});

test("blocks links become one edge per pair; a blocked start before the end is broken", () => {
  const list = [
    is("A", { start: "2026-11-02", due: "2026-11-05", links: [{ type: "blocks", target: "B" }, { type: "blocks", target: "C" }, { type: "relates", target: "C" }, { type: "blocks", target: "GONE" }] }),
    is("B", { start: "2026-11-05", due: "2026-11-07", links: [{ type: "blocked-by", target: "A" }] }),
    is("C", { start: "2026-11-04", due: "2026-11-07" }),
    is("D", { links: [{ type: "blocked-by", target: "A" }] }),
  ];
  expect(dependencyEdges(list, spansOf(list))).toEqual([
    { from: "A", to: "B", broken: false },
    { from: "A", to: "C", broken: true },
  ]);
});

test("header ticks line up with days and are cut at the range", () => {
  const r = { start: d("2026-10-26"), end: d("2026-11-08") };
  expect(monthTicks(r, 10).map((m) => [m.label, m.left, m.width])).toEqual([["окт 2026", 0, 60], ["ноя 2026", 60, 80]]);
  expect(dayTicks(r, "day")).toHaveLength(14);
  expect(dayTicks(r, "week").map((t) => t.label)).toEqual(["26", "2"]);
});
