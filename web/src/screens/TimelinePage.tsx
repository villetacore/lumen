import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, Button, EmptyState, Input, SegmentedControl, Select, Spinner } from "@villetacore/velessoft-ui";
import { api } from "../api/client";
import type { Issue, IssueFilter, ListResponse } from "../api/types";
import Crumbs from "../components/Crumbs";
import ProjectTabs from "../components/ProjectTabs";
import { addDays, iso, longDate, today } from "../lib/date";
import { errorText } from "../lib/errors";
import { PeopleOptions } from "../lib/people";
import {
  DAY_PX, type Dates, type DragMode, type Row, type Span, type Zoom, clampDelta, datesPatch, dayTicks, daysBetween, dependencyEdges,
  dragDates, drawnDates, edgePath, epicSpan, monthTicks, spanOf, timelineRange, timelineRows,
} from "../lib/timeline";
import { filterFromParams, filterToParams, issueUrl, statusName, typeLabel } from "../lib/tracker";

const ROW_H = 36;
const ZOOMS: { value: Zoom; label: string }[] = [
  { value: "day", label: "Дни" }, { value: "week", label: "Недели" }, { value: "month", label: "Месяцы" },
];

type Drag =
  | { issue: Issue; mode: DragMode; x: number; delta: number; moved: boolean }
  | { issue: Issue; mode: "draw"; from: Date; to: Date };
type Change = { key: string; dates: Dates };

const previewOf = (d: Drag): Dates =>
  d.mode === "draw" ? drawnDates(d.from, d.to) : dragDates(d.issue, d.mode, d.delta);

/**
 * TimelinePage is the project's Gantt chart. A drag or Alt+arrows on a bar
 * writes one file: the issue's start and due. Rows stay in place while dates
 * change; undated issues get their dates by drawing on their row.
 */
export default function TimelinePage() {
  const { key = "" } = useParams();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const filter = filterFromParams(params);
  const zoom = (ZOOMS.some((z) => z.value === params.get("zoom")) ? params.get("zoom") : "day") as Zoom;
  const [drag, setDrag] = useState<Drag | null>(null);
  const [announce, setAnnounce] = useState("");
  const scroller = useRef<HTMLDivElement>(null);

  const { data: project, error: projectError } = useQuery({ queryKey: ["tracker", "project", key], queryFn: () => api.trackerProject(key) });
  const { data: sprints } = useQuery({ queryKey: ["sprints", key], queryFn: () => api.sprints(key) });
  const listFilter: IssueFilter = {
    ...filter, status: undefined, category: undefined, project: key,
    sprint: filter.sprint === "all" ? undefined : filter.sprint,
  };
  const listKey = ["issues", key, "timeline", listFilter];
  const { data, error, isLoading } = useQuery({ queryKey: listKey, queryFn: () => api.issues(listFilter) });

  // Requests run one at a time: two quick presses of Alt+→ must not arrive
  // out of order. Each sends whole dates, so the last one is what stays.
  const change = useMutation({
    mutationKey: ["timeline-dates"],
    scope: { id: "timeline-dates" },
    mutationFn: ({ key, patch }: Change & { patch: NonNullable<ReturnType<typeof datesPatch>> }) => api.patchIssue(key, patch),
    onMutate: async ({ key, dates }) => {
      await qc.cancelQueries({ queryKey: listKey });
      const prev = qc.getQueryData<ListResponse<Issue>>(listKey);
      if (prev) qc.setQueryData(listKey, { ...prev, items: prev.items.map((i) => i.key === key ? { ...i, ...dates } : i) });
      return { prev };
    },
    onError: (_e, _c, ctx) => { if (ctx?.prev) qc.setQueryData(listKey, ctx.prev); },
    onSettled: () => {
      // A refetch while another change waits would briefly undo that change.
      if (qc.isMutating({ mutationKey: ["timeline-dates"] }) === 1) qc.invalidateQueries({ queryKey: ["issues"] });
    },
  });

  const issues = data?.items ?? [];
  const now = today();
  const nowKey = iso(now);
  const range = useMemo(() => timelineRange(issues, now), [issues, nowKey]);
  const rows = useMemo(() => timelineRows(issues), [issues]);
  // Bars follow the drag; rows and the range wait for the drop.
  const shown = useMemo(() => issues.map((i) => drag?.issue.key === i.key ? { ...i, ...previewOf(drag) } : i), [issues, drag]);
  const spans = useMemo(() => {
    const m = new Map<string, Span>();
    for (const i of shown) { const s = spanOf(i); if (s) m.set(i.key, s); }
    return m;
  }, [shown]);
  const edges = useMemo(() => dependencyEdges(shown, spans), [shown, spans]);

  const dayPx = DAY_PX[zoom];
  const days = daysBetween(range.start, range.end) + 1;
  const xOf = (d: Date) => daysBetween(range.start, d) * dayPx;
  const todayX = xOf(now);

  // Open on today, and keep the view in place when the range grows to the left.
  const scrolled = useRef(false);
  const shownStart = useRef(range.start);
  const shownPx = useRef(dayPx);
  const toToday = () => { if (scroller.current) scroller.current.scrollLeft = Math.max(0, todayX - dayPx * 7); };
  useLayoutEffect(() => {
    const el = scroller.current;
    if (!el || !data) return;
    if (!scrolled.current || shownPx.current !== dayPx) {
      toToday();
      scrolled.current = true;
    } else if (shownStart.current.getTime() !== range.start.getTime()) {
      el.scrollLeft += daysBetween(range.start, shownStart.current) * dayPx;
    }
    shownStart.current = range.start;
    shownPx.current = dayPx;
  });

  useEffect(() => {
    if (!drag) return;
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") setDrag(null); };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [!drag]);

  if (projectError) return <Alert tone="error" title="Проект не найден">{errorText(projectError)}</Alert>;
  if (!project) return <Spinner label="Загрузка таймлайна" />;

  const setParam = (name: string, value: string) => {
    const next = new URLSearchParams(params);
    if (value) next.set(name, value); else next.delete(name);
    setParams(next, { replace: true });
  };
  const update = (patch: Partial<IssueFilter>) => {
    const next = filterToParams({ ...filter, ...patch, status: undefined, category: undefined });
    if (zoom !== "day") next.set("zoom", zoom);
    setParams(next, { replace: true });
  };

  const commit = (issue: Issue, dates: Dates) => {
    const patch = datesPatch(issue, dates);
    if (!patch) return;
    change.mutate({ key: issue.key, dates, patch });
    setAnnounce(`${issue.key}: ${describe(dates)}`);
  };

  const dayAt = (e: React.PointerEvent, track: HTMLElement) =>
    addDays(range.start, Math.max(0, Math.min(days - 1, Math.floor((e.clientX - track.getBoundingClientRect().left) / dayPx))));

  const startDrag = (e: React.PointerEvent, issue: Issue) => {
    if (e.button !== 0) return;
    e.preventDefault();
    e.stopPropagation();
    e.currentTarget.setPointerCapture?.(e.pointerId);
    const mode = ((e.target as HTMLElement).dataset.edge as DragMode | undefined) ?? "move";
    setDrag({ issue, mode, x: e.clientX, delta: 0, moved: false });
  };
  const onMove = (e: React.PointerEvent) => {
    if (!drag || drag.mode === "draw") return;
    const span = spanOf(drag.issue);
    if (!span) return;
    const delta = clampDelta(span, drag.mode, Math.round((e.clientX - drag.x) / dayPx), range);
    const moved = drag.moved || Math.abs(e.clientX - drag.x) > 3;
    if (delta !== drag.delta || moved !== drag.moved) setDrag({ ...drag, delta, moved });
  };
  const endDrag = () => {
    if (!drag || drag.mode === "draw") return;
    setDrag(null);
    if (drag.moved) commit(drag.issue, previewOf(drag));
    else if (drag.mode === "move") navigate(issueUrl(drag.issue.key));
  };

  // Alt+←/→ moves the bar by a day, Alt+Shift+←/→ moves its end.
  const onBarKey = (e: React.KeyboardEvent, issue: Issue) => {
    if (e.key === "Enter" || e.key === " ") { e.preventDefault(); navigate(issueUrl(issue.key)); return; }
    if (!e.altKey || (e.key !== "ArrowLeft" && e.key !== "ArrowRight")) return;
    e.preventDefault();
    const span = spanOf(issue);
    if (!span) return;
    const mode: DragMode = e.shiftKey ? "end" : "move";
    commit(issue, dragDates(issue, mode, clampDelta(span, mode, e.key === "ArrowLeft" ? -1 : 1, range)));
  };

  const bar = (issue: Issue, span: Span) => {
    const done = issue.status_category === "done";
    const late = !done && span.end < now;
    const width = (daysBetween(span.start, span.end) + 1) * dayPx;
    const state = done ? "готово" : late ? "просрочен" : statusName(project, issue.status);
    return <div className={`tl-bar is-${issue.status_category ?? "todo"}${late ? " is-late" : ""}${drag?.issue.key === issue.key ? " is-dragging" : ""}`}
      style={{ left: xOf(span.start), width: Math.max(width, 8) }}
      role="button" tabIndex={0} data-bar={issue.key}
      aria-label={`${issue.key} ${issue.title}, ${describe(issue)}, ${state}`} aria-describedby="timeline-keys"
      title={`${issue.key} · ${describe(issue)} · ${state}`}
      onPointerDown={(e) => startDrag(e, issue)} onPointerMove={onMove} onPointerUp={endDrag} onPointerCancel={() => setDrag(null)}
      onKeyDown={(e) => onBarKey(e, issue)}>
      {width >= 24 && <><span className="tl-edge start" data-edge="start" /><span className="tl-edge end" data-edge="end" /></>}
    </div>;
  };

  const issueTrack = (issue: Issue) => {
    const span = spans.get(issue.key);
    if (span && (spanOf(issue) || drag?.issue.key !== issue.key)) return bar(issue, span);
    // Mouse only: on a touch screen the same gesture scrolls the chart.
    return <>
      {span && <div className="tl-bar is-drawing" style={{ left: xOf(span.start), width: (daysBetween(span.start, span.end) + 1) * dayPx }} />}
      <div className="tl-draw" title="Протяните, чтобы задать даты"
        onPointerDown={(e) => {
          if (e.button !== 0 || e.pointerType === "touch") return;
          e.preventDefault();
          e.currentTarget.setPointerCapture?.(e.pointerId);
          const day = dayAt(e, e.currentTarget);
          setDrag({ issue, mode: "draw", from: day, to: day });
        }}
        onPointerMove={(e) => { if (drag?.mode === "draw") setDrag({ ...drag, to: dayAt(e, e.currentTarget) }); }}
        onPointerUp={() => {
          if (drag?.mode !== "draw") return;
          setDrag(null);
          commit(drag.issue, previewOf(drag));
        }}
        onPointerCancel={() => setDrag(null)} />
    </>;
  };

  const rowView = (row: Row) => {
    if (row.kind === "section") {
      return <div key={row.id} className="tl-row tl-group" role="row">
        <div className="tl-label" role="rowheader"><strong>{row.label}</strong></div>
        <div className="tl-track" />
      </div>;
    }
    if (row.kind === "epic") {
      const span = epicSpan(row, spans);
      const own = row.issue && spans.has(row.issue.key);
      return <div key={row.id} className="tl-row tl-group" role="row">
        <div className="tl-label" role="rowheader">
          {row.issue
            ? <Link to={issueUrl(row.issue.key)}><span className="issue-key">{row.issue.key}</span> <strong>{row.label}</strong></Link>
            : row.label === row.id.slice(5)
              ? <Link to={issueUrl(row.label)} className="issue-key">{row.label}</Link>
              : <strong>{row.label}</strong>}
        </div>
        <div className="tl-track">
          {own ? bar(row.issue!, span!)
            : span && row.issue && <div className="tl-reach" style={{ left: xOf(span.start), width: (daysBetween(span.start, span.end) + 1) * dayPx }} title={`Диапазон задач эпика ${row.issue.key}`} />}
        </div>
      </div>;
    }
    const done = row.issue.status_category === "done";
    return <div key={row.id} className="tl-row" role="row">
      <div className={`tl-label${done ? " is-done" : ""}`} role="rowheader">
        <Link to={issueUrl(row.issue.key)} title={`${row.issue.title} · ${typeLabel(row.issue.issue_type)}`}>
          <span className="issue-key">{row.issue.key}</span> {row.issue.title}
        </Link>
      </div>
      <div className="tl-track">{issueTrack(row.issue)}</div>
    </div>;
  };

  // Arrows are drawn over the tracks; rows have one height, so y follows from the index.
  const rowIndex = new Map<string, number>();
  rows.forEach((r, i) => {
    if (r.kind === "issue") rowIndex.set(r.id, i);
    if (r.kind === "epic" && r.issue) rowIndex.set(r.issue.key, i);
  });
  const broken = edges.filter((e) => e.broken).length;
  const height = rows.length * ROW_H;
  const trackWidth = days * dayPx;

  return <div className="page timeline-page">
    <Crumbs items={[{ label: "Проекты", to: "/tracker" }, { label: `${project.key} · ${project.title}` }]} />
    <div className="page-head">
      <div><h1>{project.title}</h1><ProjectTabs projectKey={project.key} /></div>
    </div>

    <div className="toolbar issue-filters" role="search" aria-label="Фильтр таймлайна">
      <div className="grow"><Input label="Поиск" placeholder="Название или ключ" value={filter.q ?? ""} onChange={(e) => update({ q: e.target.value })} /></div>
      <details className="more-filters" open={!!(filter.type || filter.assignee || filter.sprint) || undefined}>
      <summary>Фильтры</summary>
      <div className="more-filters-body">
      <Select label="Тип" value={filter.type ?? ""} onChange={(e) => update({ type: e.target.value })}>
        <option value="">Все</option>
        {project.issue_types.map((t) => <option key={t} value={t}>{typeLabel(t)}</option>)}
      </Select>
      <Input label="Исполнитель" placeholder="имя или none" list="filter-people" value={filter.assignee ?? ""} onChange={(e) => update({ assignee: e.target.value.trim() })} />
      <PeopleOptions id="filter-people" />
      <Select label="Спринт" value={filter.sprint || "all"} onChange={(e) => update({ sprint: e.target.value === "all" ? "" : e.target.value })}>
        <option value="all">Все тикеты</option>
        {sprints?.items.map((sp) => <option key={sp.id} value={sp.id}>{sp.name}{sp.state === "active" ? " (идёт)" : ""}</option>)}
      </Select>
      </div>
      </details>
    </div>
    <div className="timeline-controls">
      <SegmentedControl label="Масштаб" options={ZOOMS} value={zoom} onChange={(v: string) => setParam("zoom", v === "day" ? "" : v)} />
      <Button size="sm" onClick={toToday}>Сегодня</Button>
      <p id="timeline-keys" className="hint">Тяните полосу или её край. С клавиатуры: Alt+←/→ сдвигает, Alt+Shift+←/→ меняет срок, Enter открывает тикет.</p>
    </div>

    {isLoading && <Spinner label="Загрузка тикетов" />}
    {error && <Alert tone="error" title="Тикеты не загружены">{errorText(error)}</Alert>}
    {change.isError && <Alert tone="error" title="Даты не изменены">{errorText(change.error)}</Alert>}
    {broken > 0 && <Alert tone="warning" title={`Нарушенных зависимостей: ${broken}`}>Заблокированный тикет начинается раньше, чем заканчивается блокирующий. Такие стрелки — красные.</Alert>}
    {!!data && data.total > issues.length && <p className="hint">Показаны первые {issues.length} из {data.total} тикетов. Сузьте фильтр.</p>}
    <div className="sr-only" aria-live="polite">{announce}</div>

    {data && !rows.length
      ? <EmptyState title="Тикетов нет" description="Подходящих тикетов нет. Измените фильтр или создайте тикет на доске." />
      : data && <TimelineGrid {...{ scroller, range, zoom, trackWidth, height, todayX }}>
        {rows.map(rowView)}
        <svg className="tl-edges" width={trackWidth} height={height} aria-hidden="true">
          <defs>
            <marker id="tl-arrow" markerWidth="7" markerHeight="7" refX="6" refY="3" orient="auto"><path d="M0 0L6 3L0 6z" className="tl-arrow" /></marker>
            <marker id="tl-arrow-broken" markerWidth="7" markerHeight="7" refX="6" refY="3" orient="auto"><path d="M0 0L6 3L0 6z" className="tl-arrow is-broken" /></marker>
          </defs>
          {edges.map((e) => {
            const a = spans.get(e.from)!;
            const b = spans.get(e.to)!;
            const ya = rowIndex.get(e.from);
            const yb = rowIndex.get(e.to);
            if (ya === undefined || yb === undefined) return null;
            return <path key={`${e.from}>${e.to}`} data-edge-link={`${e.from}>${e.to}`} className={`tl-link${e.broken ? " is-broken" : ""}`}
              markerEnd={`url(#tl-arrow${e.broken ? "-broken" : ""})`}
              d={edgePath(xOf(addDays(a.end, 1)), ya * ROW_H + ROW_H / 2, xOf(b.start), yb * ROW_H + ROW_H / 2)} />;
          })}
        </svg>
      </TimelineGrid>}
  </div>;
}

function TimelineGrid({ scroller, range, zoom, trackWidth, height, todayX, children }: {
  scroller: React.RefObject<HTMLDivElement | null>; range: Span; zoom: Zoom;
  trackWidth: number; height: number; todayX: number; children: React.ReactNode;
}) {
  const dayPx = DAY_PX[zoom];
  const months = monthTicks(range, dayPx);
  const ticks = dayTicks(range, zoom);
  return <div className="tl-scroll" ref={scroller} role="table" aria-label="Таймлайн тикетов"
    style={{ "--tl-day": `${dayPx}px`, "--tl-width": `${trackWidth}px`, "--tl-row": `${ROW_H}px`, "--tl-height": `${height}px` } as React.CSSProperties}>
    <div className={`tl-grid zoom-${zoom}`}>
      <div className="tl-head" role="row">
        <div className="tl-label" role="columnheader">Тикет</div>
        <div className="tl-track" role="columnheader" aria-label="Даты">
          <div className="tl-months">{months.map((m) => <span key={m.id} style={{ left: m.left, width: m.width }}>{m.width >= 48 ? m.label : ""}</span>)}</div>
          <div className="tl-days">{ticks.map((t) => <span key={t.id} style={{ left: t.left, width: t.width }}>{t.label}</span>)}</div>
        </div>
      </div>
      <div className="tl-body">
        {children}
        <div className="tl-today" style={{ left: todayX + dayPx / 2 }} title="Сегодня" />
      </div>
    </div>
  </div>;
}

function describe(i: Dates): string {
  const part = (v: string | null) => v ? longDate(new Date(v.slice(0, 10) + "T00:00:00")) : "";
  if (i.start && i.due) return i.start === i.due ? part(i.due) : `${part(i.start)} — ${part(i.due)}`;
  if (i.due) return `срок ${part(i.due)}`;
  if (i.start) return `начало ${part(i.start)}`;
  return "без дат";
}
