import { useRef, useState } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, Avatar, Badge, Button, IconButton, Input, Select, Spinner } from "@villetacore/velessoft-ui";
import { api } from "../api/client";
import type { Issue, IssueFilter, ListResponse, TrackerProject } from "../api/types";
import Crumbs from "../components/Crumbs";
import Icon from "../components/Icon";
import NewIssueDialog from "../components/NewIssueDialog";
import ProjectTabs from "../components/ProjectTabs";
import { dueTone } from "../components/TaskRow";
import { relativeDue } from "../lib/date";
import { errorText } from "../lib/errors";
import { type LaneBy, type Move, OTHER, applyMove, boardColumns, canDrop, columnOf, laneOf, layoutBoard, planMove } from "../lib/board";
import { filterFromParams, filterToParams, issueUrl, priorityOf, typeLabel } from "../lib/tracker";

type Drag = { issue: Issue; lane: string };
type Target = { status: string; lane: string; index: number };

/**
 * BoardPage is the project's kanban board. A drop writes one file: the
 * issue's status and rank (POST /issues/{key}/move). Cards move with the
 * mouse, or from the keyboard with Alt+arrows on a focused card.
 */
export default function BoardPage() {
  const { key = "" } = useParams();
  const qc = useQueryClient();
  const [params, setParams] = useSearchParams();
  const filter = filterFromParams(params);
  const laneBy = (params.get("lanes") as LaneBy) || "none";
  const [drag, setDrag] = useState<Drag | null>(null);
  const [target, setTarget] = useState<Target | null>(null);
  const [creating, setCreating] = useState<string | null>(null);
  const [announce, setAnnounce] = useState("");
  const board = useRef<HTMLDivElement>(null);

  const { data: project, error: projectError } = useQuery({ queryKey: ["tracker", "project", key], queryFn: () => api.trackerProject(key) });
  // Status filters make no sense on a board: the columns are the statuses.
  const boardFilter: IssueFilter = { ...filter, status: undefined, category: undefined, project: key };
  const listKey = ["issues", key, "board", boardFilter];
  const { data, error, isLoading } = useQuery({ queryKey: listKey, queryFn: () => api.issues(boardFilter) });

  const move = useMutation({
    mutationFn: (m: Move) => api.moveIssue(m.key, { status: m.status, before: m.before, after: m.after }),
    onMutate: async (m) => {
      await qc.cancelQueries({ queryKey: listKey });
      const prev = qc.getQueryData<ListResponse<Issue>>(listKey);
      const category = project?.workflow.find((s) => s.id === m.status)?.category;
      if (prev) qc.setQueryData(listKey, { ...prev, items: applyMove(prev.items, m, category) });
      return { prev };
    },
    onError: (_e, _m, ctx) => { if (ctx?.prev) qc.setQueryData(listKey, ctx.prev); },
    onSettled: () => qc.invalidateQueries({ queryKey: ["issues"] }),
  });

  if (projectError) return <Alert tone="error" title="Проект не найден">{errorText(projectError)}</Alert>;
  if (!project) return <Spinner label="Загрузка доски" />;

  const issues = data?.items ?? [];
  const columns = boardColumns(project, issues);
  const lanes = layoutBoard(project, issues, laneBy);
  const update = (patch: Partial<IssueFilter>) => {
    const next = filterToParams({ ...filter, ...patch, status: undefined, category: undefined });
    if (laneBy !== "none") next.set("lanes", laneBy);
    setParams(next, { replace: true });
  };
  const setLanes = (by: LaneBy) => {
    const next = new URLSearchParams(params);
    if (by === "none") next.delete("lanes"); else next.set("lanes", by);
    setParams(next, { replace: true });
  };

  const drop = (status: string, lane: string, index: number, issue: Issue) => {
    if (!canDrop(project, issue.status, status)) {
      setAnnounce(`Переход «${issue.status}» → «${status}» не разрешён workflow`);
      return;
    }
    const cell = lanes.find((l) => l.id === lane)?.columns.get(status) ?? [];
    const m = planMove(cell, issue, status, index);
    if (!m) return;
    move.mutate(m);
    const name = project.workflow.find((s) => s.id === status)?.name ?? status;
    setAnnounce(`${issue.key} → ${name}, позиция ${Math.min(index, cell.length) + 1}`);
  };

  // Keyboard: Alt+←/→ moves to the neighbouring allowed column (to its top),
  // Alt+↑/↓ moves within the column.
  const onCardKey = (e: React.KeyboardEvent, issue: Issue, lane: string) => {
    if (!e.altKey || !["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown"].includes(e.key)) return;
    e.preventDefault();
    const col = columnOf(project, issue);
    const cell = lanes.find((l) => l.id === lane)?.columns.get(col) ?? [];
    const pos = cell.findIndex((i) => i.key === issue.key);
    if (e.key === "ArrowUp" && pos > 0) drop(col, lane, pos - 1, issue);
    if (e.key === "ArrowDown" && pos < cell.length - 1) drop(col, lane, pos + 1, issue);
    if (e.key === "ArrowLeft" || e.key === "ArrowRight") {
      const step = e.key === "ArrowLeft" ? -1 : 1;
      const order = project.workflow.map((s) => s.id);
      for (let i = order.indexOf(col) + step; i >= 0 && i < order.length; i += step) {
        if (canDrop(project, issue.status, order[i])) { drop(order[i], lane, 0, issue); break; }
      }
    }
    // Keep focus on the card after it re-renders in its new place.
    requestAnimationFrame(() => board.current?.querySelector<HTMLElement>(`[data-card="${issue.key}"]`)?.focus());
  };

  const dropIndex = (e: React.DragEvent, cell: Issue[]) => {
    const cards = [...(e.currentTarget as HTMLElement).querySelectorAll<HTMLElement>("[data-card]")]
      .filter((el) => el.dataset.card !== drag?.issue.key);
    const i = cards.findIndex((el) => { const r = el.getBoundingClientRect(); return e.clientY < r.top + r.height / 2; });
    return i < 0 ? cell.filter((x) => x.key !== drag?.issue.key).length : i;
  };

  return <div className="page board-page">
    <Crumbs items={[{ label: "Трекер", to: "/tracker" }, { label: `${project.key} · ${project.title}` }]} />
    <div className="page-head">
      <div><span className="kicker">Проект {project.key}</span><h1>{project.title}</h1><ProjectTabs projectKey={project.key} /></div>
      <div className="head-actions"><Button variant="primary" onClick={() => setCreating(project.workflow[0].id)}><Icon name="plus" size={16} />Тикет</Button></div>
    </div>

    <div className="toolbar issue-filters" role="search" aria-label="Фильтр доски">
      <div className="grow"><Input label="Поиск" placeholder="Название или ключ" value={filter.q ?? ""} onChange={(e) => update({ q: e.target.value })} /></div>
      <Select label="Тип" value={filter.type ?? ""} onChange={(e) => update({ type: e.target.value })}>
        <option value="">Все</option>
        {project.issue_types.map((t) => <option key={t} value={t}>{typeLabel(t)}</option>)}
      </Select>
      <Input label="Исполнитель" placeholder="имя или none" value={filter.assignee ?? ""} onChange={(e) => update({ assignee: e.target.value.trim() })} />
      <Input label="Метка" value={filter.label?.join(",") ?? ""} onChange={(e) => update({ label: e.target.value.split(",").map((s) => s.trim()).filter(Boolean) })} />
      <Select label="Дорожки" value={laneBy} onChange={(e) => setLanes(e.target.value as LaneBy)}>
        <option value="none">Без дорожек</option>
        <option value="assignee">По исполнителю</option>
        <option value="epic">По эпику</option>
      </Select>
    </div>
    <p id="board-keys" className="hint board-hint">Перетащите карточку или нажмите на ней Alt+←/→ (колонка) и Alt+↑/↓ (порядок).</p>

    {isLoading && <Spinner label="Загрузка тикетов" />}
    {error && <Alert tone="error" title="Тикеты не загружены">{errorText(error)}</Alert>}
    {move.isError && <Alert tone="error" title="Карточка не перемещена">{errorText(move.error)}</Alert>}
    <div className="sr-only" aria-live="polite">{announce}</div>

    <div className="board" ref={board} style={{ "--board-cols": columns.length } as React.CSSProperties}>
      <div className="board-row board-head" role="row">
        {columns.map((c) => {
          const count = issues.filter((i) => columnOf(project, i) === c.id).length;
          const over = !!c.wip && count > c.wip;
          return <div key={c.id} className={`board-col-head${over ? " over-wip" : ""}`} role="columnheader">
            <span className="board-col-name">{c.name}</span>
            <Badge tone={over ? "warning" : "neutral"} title={c.wip ? `Лимит WIP: ${c.wip}` : undefined}>{count}{c.wip ? ` / ${c.wip}` : ""}</Badge>
            {c.id !== OTHER && <IconButton size="sm" variant="ghost" label={`Новый тикет в «${c.name}»`} onClick={() => setCreating(c.id)}><Icon name="plus" size={14} /></IconButton>}
          </div>;
        })}
      </div>
      {lanes.map((lane) => <section key={lane.id} className="board-lane" aria-label={lane.label || "Доска"}>
        {laneBy !== "none" && <h2 className="board-lane-title">{lane.label} <span className="count">{[...lane.columns.values()].reduce((n, c) => n + c.length, 0)}</span></h2>}
        <div className="board-row">
          {columns.map((c) => {
            const cell = lane.columns.get(c.id) ?? [];
            const allowed = !drag || (drag.lane === lane.id && canDrop(project, drag.issue.status, c.id));
            const here = target && target.status === c.id && target.lane === lane.id;
            return <div key={c.id} className={`board-cell${drag && !allowed ? " is-blocked" : ""}${here ? " is-target" : ""}`}
              aria-label={c.name}
              onDragOver={(e) => {
                if (!drag || !allowed) return;
                e.preventDefault();
                e.dataTransfer.dropEffect = "move";
                const index = dropIndex(e, cell);
                if (!here || target.index !== index) setTarget({ status: c.id, lane: lane.id, index });
              }}
              onDragLeave={(e) => { if (!e.currentTarget.contains(e.relatedTarget as Node)) setTarget(null); }}
              onDrop={(e) => {
                e.preventDefault();
                if (drag && allowed) drop(c.id, lane.id, dropIndex(e, cell), drag.issue);
                setDrag(null); setTarget(null);
              }}>
              {cell.filter((i) => i.key !== drag?.issue.key || !here).map((is, idx) => <div key={is.key}>
                {here && target.index === idx && <div className="drop-marker" />}
                <IssueCard issue={is} project={project} dragging={drag?.issue.key === is.key}
                  onDragStart={() => setDrag({ issue: is, lane: lane.id })}
                  onDragEnd={() => { setDrag(null); setTarget(null); }}
                  onKeyDown={(e) => onCardKey(e, is, laneOf(is, laneBy))} />
              </div>)}
              {here && target.index >= cell.filter((i) => i.key !== drag?.issue.key).length && <div className="drop-marker" />}
              {cell.length === 0 && !here && <div className="board-empty">—</div>}
            </div>;
          })}
        </div>
      </section>)}
    </div>
    {creating && <NewIssueDialog project={project.key} status={creating} onClose={() => setCreating(null)} />}
  </div>;
}

function IssueCard({ issue, project, dragging, onDragStart, onDragEnd, onKeyDown }: {
  issue: Issue; project: TrackerProject; dragging: boolean;
  onDragStart: () => void; onDragEnd: () => void; onKeyDown: (e: React.KeyboardEvent) => void;
}) {
  const p = priorityOf(issue.priority);
  const done = project.workflow.find((s) => s.id === issue.status)?.category === "done";
  return <article className={`board-card${dragging ? " is-dragging" : ""}`} draggable
    onDragStart={(e) => { e.dataTransfer.setData("text/plain", issue.key); e.dataTransfer.effectAllowed = "move"; onDragStart(); }}
    onDragEnd={onDragEnd}>
    <Link to={issueUrl(issue.key)} className="board-card-link" data-card={issue.key} onKeyDown={onKeyDown}
      draggable={false} aria-describedby="board-keys">
      <span className="board-card-title">{issue.title}</span>
    </Link>
    <div className="board-card-meta">
      <span className="issue-key">{issue.key}</span>
      <span className="board-card-type">{typeLabel(issue.issue_type)}</span>
      {p && <Badge tone={p.tone}>{p.label}</Badge>}
      {issue.due && <Badge tone={done ? "neutral" : dueTone(issue.due)}>{relativeDue(issue.due)}</Badge>}
      {!!issue.points && <span className="board-points" title="Story points">{issue.points}</span>}
      {issue.assignee && <span className="board-assignee" title={issue.assignee}><Avatar name={issue.assignee} size="sm" round /></span>}
    </div>
    {!!issue.labels?.length && <div className="board-card-labels">{issue.labels.map((l) => <span key={l} className="md-tag">#{l}</span>)}</div>}
  </article>;
}
