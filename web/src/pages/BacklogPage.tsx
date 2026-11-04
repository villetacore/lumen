import { useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, Badge, Button, Dialog, Input, Select, Spinner, Textarea } from "@villetacore/velessoft-ui";
import { api } from "../api/client";
import type { Issue, Sprint, SprintInput, TrackerProject } from "../api/types";
import Crumbs from "../components/Crumbs";
import Icon from "../components/Icon";
import NewIssueDialog from "../components/NewIssueDialog";
import ProjectTabs from "../components/ProjectTabs";
import { iso, longDate, parseDate, today } from "../lib/date";
import { errorText } from "../lib/errors";
import { neighbours } from "../lib/board";
import { CATEGORY_TONE, issueUrl, priorityOf, statusName, typeLabel } from "../lib/tracker";

const BACKLOG = "";

type Section = { id: string; sprint?: Sprint; issues: Issue[] };
type Drag = { issue: Issue; from: string };
type Move = { key: string; sprint: string; before?: string; after?: string };

/**
 * BacklogPage plans work: open sprints on top, the ranked backlog below.
 * An issue moves between them by drag and drop or with its "Спринт" select;
 * Alt+↑/↓ on a row reorders it. Each move rewrites only that issue's file.
 */
export default function BacklogPage() {
  const { key = "" } = useParams();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [drag, setDrag] = useState<Drag | null>(null);
  const [target, setTarget] = useState<{ section: string; index: number } | null>(null);
  const [dialog, setDialog] = useState<{ kind: "edit" | "start" | "complete"; sprint: Sprint } | null>(null);
  const [creating, setCreating] = useState(false);
  const [announce, setAnnounce] = useState("");
  const root = useRef<HTMLDivElement>(null);

  const { data: project, error: projectError } = useQuery({ queryKey: ["tracker", "project", key], queryFn: () => api.trackerProject(key) });
  const { data: sprints } = useQuery({ queryKey: ["sprints", key], queryFn: () => api.sprints(key) });
  const { data, isLoading, error } = useQuery({ queryKey: ["issues", key, "backlog"], queryFn: () => api.issues({ project: key }) });

  const move = useMutation({
    mutationFn: (m: Move) => api.moveIssue(m.key, { sprint: m.sprint, before: m.before, after: m.after }),
    onSettled: () => qc.invalidateQueries({ queryKey: ["issues"] }),
  });
  const createSprint = useMutation({
    mutationFn: () => api.createSprint(key),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["sprints", key] }),
  });

  if (projectError) return <Alert tone="error" title="Проект не найден">{errorText(projectError)}</Alert>;
  if (!project || !sprints) return <Spinner label="Загрузка бэклога" />;

  const open = sprints.items.filter((s) => s.state !== "closed");
  const openIds = new Set(open.map((s) => s.id));
  const active = open.find((s) => s.state === "active");
  const issues = data?.items ?? [];
  const sections: Section[] = [
    ...open.map((s) => ({ id: s.id, sprint: s, issues: issues.filter((i) => i.sprint === s.id) })),
    // Finished issues leave the backlog; issues of closed or unknown sprints return to it.
    { id: BACKLOG, issues: issues.filter((i) => !openIds.has(i.sprint ?? "") && i.status_category !== "done") },
  ];

  const place = (issue: Issue, section: string, index: number) => {
    const list = sections.find((s) => s.id === section)?.issues ?? [];
    const sameSection = (issue.sprint ?? "") === section || (section === BACKLOG && !openIds.has(issue.sprint ?? ""));
    const n = neighbours(list, issue.key, index, !sameSection);
    if (!n) return;
    move.mutate({ key: issue.key, sprint: section, ...n });
    setAnnounce(`${issue.key} → ${section ? sections.find((s) => s.id === section)?.sprint?.name : "Бэклог"}, позиция ${index + 1}`);
  };
  const onRowKey = (e: React.KeyboardEvent, issue: Issue, section: Section) => {
    if (!e.altKey || (e.key !== "ArrowUp" && e.key !== "ArrowDown")) return;
    e.preventDefault();
    const pos = section.issues.findIndex((i) => i.key === issue.key);
    const to = e.key === "ArrowUp" ? pos - 1 : pos + 1;
    if (to < 0 || to >= section.issues.length) return;
    place(issue, section.id, to);
    requestAnimationFrame(() => root.current?.querySelector<HTMLElement>(`[data-row="${issue.key}"]`)?.focus());
  };
  const dropIndex = (e: React.DragEvent) => {
    const rows = [...(e.currentTarget as HTMLElement).querySelectorAll<HTMLElement>("[data-row]")].filter((el) => el.dataset.row !== drag?.issue.key);
    const i = rows.findIndex((el) => { const r = el.getBoundingClientRect(); return e.clientY < r.top + r.height / 2; });
    return i < 0 ? rows.length : i;
  };

  return <div className="page backlog-page" ref={root}>
    <Crumbs items={[{ label: "Проекты", to: "/tracker" }, { label: `${project.key} · ${project.title}` }]} />
    <div className="page-head">
      <div><h1>{project.title}</h1><ProjectTabs projectKey={project.key} /></div>
      <div className="head-actions">
        <Button onClick={() => createSprint.mutate()} loading={createSprint.isPending}><Icon name="plus" size={16} />Спринт</Button>
        <Button variant="primary" onClick={() => setCreating(true)}><Icon name="plus" size={16} />Тикет</Button>
      </div>
    </div>
    <p id="backlog-keys" className="hint board-hint">Перетащите тикет между спринтами и бэклогом или выберите спринт в строке; Alt+↑/↓ меняет порядок.</p>
    {isLoading && <Spinner label="Загрузка тикетов" />}
    {[error, move.error, createSprint.error].map((e, i) => e && <Alert key={i} tone="error" title="Ошибка">{errorText(e)}</Alert>)}
    <div className="sr-only" aria-live="polite">{announce}</div>

    {sections.map((section) => {
      const sp = section.sprint;
      const points = section.issues.reduce((n, i) => n + (i.points ?? 0), 0);
      const done = section.issues.filter((i) => i.status_category === "done").length;
      const here = target?.section === section.id;
      const allowed = !drag || drag.issue.status_category !== "done" || section.id !== BACKLOG;
      const visible = section.issues.filter((i) => !(here && i.key === drag?.issue.key));
      return <section key={section.id || "backlog"} className={`backlog-section${sp?.state === "active" ? " is-active" : ""}`} aria-labelledby={`sec-${section.id || "backlog"}`}>
        <header className="backlog-head">
          <div className="backlog-title">
            <h2 id={`sec-${section.id || "backlog"}`}>{sp ? sp.name : "Бэклог"}</h2>
            {sp && <Badge tone={sp.state === "active" ? "info" : "neutral"}>{sp.state === "active" ? "идёт" : "запланирован"}</Badge>}
            <span className="sub">
              {section.issues.length} шт.{points ? ` · ${points} SP` : ""}{sp && done ? ` · готово ${done}` : ""}
              {sp?.start && ` · ${longDate(parseDate(sp.start))} — ${sp.end ? longDate(parseDate(sp.end)) : "…"}`}
            </span>
          </div>
          {sp?.goal && <p className="backlog-goal">{sp.goal}</p>}
          {sp && <div className="head-actions">
            {sp.state === "active"
              ? <><Button onClick={() => navigate(`/tracker/${encodeURIComponent(project.key)}/board`)}>Доска</Button>
                  <Button variant="primary" onClick={() => setDialog({ kind: "complete", sprint: sp })}>Завершить спринт</Button></>
              : <Button variant="primary" disabled={!!active} title={active ? `Сначала завершите ${active.name}` : undefined}
                  onClick={() => setDialog({ kind: "start", sprint: sp })}>Начать спринт</Button>}
            <Button variant="ghost" onClick={() => setDialog({ kind: "edit", sprint: sp })}><Icon name="edit" size={14} />Изменить</Button>
          </div>}
        </header>
        <ol className={`backlog-list${here ? " is-target" : ""}${drag && !allowed ? " is-blocked" : ""}`}
          onDragOver={(e) => {
            if (!drag || !allowed) return;
            e.preventDefault();
            const index = dropIndex(e);
            if (!here || target.index !== index) setTarget({ section: section.id, index });
          }}
          onDragLeave={(e) => { if (!e.currentTarget.contains(e.relatedTarget as Node)) setTarget(null); }}
          onDrop={(e) => {
            e.preventDefault();
            if (drag && allowed) place(drag.issue, section.id, dropIndex(e));
            setDrag(null); setTarget(null);
          }}>
          {visible.map((is, idx) => <li key={is.key}>
            {here && target.index === idx && <div className="drop-marker" />}
            <BacklogRow issue={is} project={project} sprints={open} dragging={drag?.issue.key === is.key}
              onDragStart={() => setDrag({ issue: is, from: section.id })}
              onDragEnd={() => { setDrag(null); setTarget(null); }}
              onKeyDown={(e) => onRowKey(e, is, section)}
              onSprint={(id) => place(is, id, sections.find((s) => s.id === id)?.issues.length ?? 0)} />
          </li>)}
          {here && target.index >= visible.length && <li><div className="drop-marker" /></li>}
          {section.issues.length === 0 && !here && <li className="backlog-empty">{sp ? "Перетащите сюда тикеты из бэклога" : "Бэклог пуст"}</li>}
        </ol>
      </section>;
    })}

    {creating && <NewIssueDialog project={project.key} onClose={() => setCreating(false)} />}
    {dialog && dialog.kind !== "complete" && <SprintDialog sprint={dialog.sprint} start={dialog.kind === "start"} onClose={() => setDialog(null)} />}
    {dialog?.kind === "complete" && <CompleteDialog sprint={dialog.sprint} project={project} planned={open.filter((s) => s.state === "planned")}
      unfinished={issues.filter((i) => i.sprint === dialog.sprint.id && i.status_category !== "done").length} onClose={() => setDialog(null)} />}
  </div>;
}

function BacklogRow({ issue, project, sprints, dragging, onDragStart, onDragEnd, onKeyDown, onSprint }: {
  issue: Issue; project: TrackerProject; sprints: Sprint[]; dragging: boolean;
  onDragStart: () => void; onDragEnd: () => void; onKeyDown: (e: React.KeyboardEvent) => void; onSprint: (id: string) => void;
}) {
  const p = priorityOf(issue.priority);
  const done = issue.status_category === "done";
  const current = sprints.some((s) => s.id === issue.sprint) ? issue.sprint! : BACKLOG;
  return <div className={`backlog-row${dragging ? " is-dragging" : ""}${done ? " is-done" : ""}`} draggable
    onDragStart={(e) => { e.dataTransfer.setData("text/plain", issue.key); e.dataTransfer.effectAllowed = "move"; onDragStart(); }}
    onDragEnd={onDragEnd}>
    <span className="backlog-grip" aria-hidden="true">⋮⋮</span>
    <span className="issue-key">{issue.key}</span>
    <Link className="backlog-row-title" to={issueUrl(issue.key)} data-row={issue.key} onKeyDown={onKeyDown}
      draggable={false} aria-describedby="backlog-keys">{issue.title}</Link>
    <span className="backlog-row-meta">
      <span className="board-card-type">{typeLabel(issue.issue_type)}</span>
      {p && <Badge tone={p.tone}>{p.label}</Badge>}
      <Badge tone={CATEGORY_TONE[issue.status_category ?? "todo"]}>{statusName(project, issue.status)}</Badge>
      {issue.assignee && <span className="sub">{issue.assignee}</span>}
      {!!issue.points && <span className="board-points" title="Story points">{issue.points}</span>}
    </span>
    <select className="vs-input backlog-sprint" aria-label={`Спринт для ${issue.key}`} value={current}
      onChange={(e) => onSprint(e.target.value)}>
      <option value={BACKLOG}>Бэклог</option>
      {sprints.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
    </select>
  </div>;
}

/** SprintDialog edits a sprint, or starts it with its dates and goal. */
function SprintDialog({ sprint, start, onClose }: { sprint: Sprint; start: boolean; onClose: () => void }) {
  const qc = useQueryClient();
  const now = today();
  const [form, setForm] = useState<Required<SprintInput>>({
    name: sprint.name, goal: sprint.goal ?? "",
    start: sprint.start ?? (start ? iso(now) : ""),
    end: sprint.end ?? (start ? iso(new Date(now.getFullYear(), now.getMonth(), now.getDate() + 14)) : ""),
  });
  const set = (k: keyof SprintInput) => (e: { target: { value: string } }) => setForm((f) => ({ ...f, [k]: e.target.value }));
  const save = useMutation({
    mutationFn: () => start ? api.startSprint(sprint.id, form) : api.patchSprint(sprint.id, form),
    onSuccess: () => { qc.invalidateQueries(); onClose(); },
  });
  const badDates = !!form.start && !!form.end && form.end < form.start;
  return <Dialog open onClose={() => !save.isPending && onClose()} title={start ? `Начать: ${sprint.name}` : `Спринт ${sprint.id}`} footer={<>
    <Button onClick={onClose} disabled={save.isPending}>Отмена</Button>
    <Button type="submit" form="sprint-form" variant="primary" loading={save.isPending} disabled={!form.name.trim() || badDates}>{start ? "Начать" : "Сохранить"}</Button>
  </>}>
    <form id="sprint-form" className="issue-form" onSubmit={(e) => { e.preventDefault(); if (form.name.trim() && !badDates) save.mutate(); }}>
      <Input label="Название" required value={form.name} onChange={set("name")} />
      <div className="form-row">
        <Input label="Начало" type="date" value={form.start} onChange={set("start")} />
        <Input label="Конец" type="date" value={form.end} onChange={set("end")} error={badDates ? "Конец раньше начала" : undefined} />
      </div>
      <Textarea label="Цель спринта" value={form.goal} onChange={set("goal")} />
      {save.isError && <Alert tone="error" title={start ? "Спринт не начат" : "Спринт не сохранён"}>{errorText(save.error)}</Alert>}
    </form>
  </Dialog>;
}

/** CompleteDialog closes the active sprint and sends unfinished issues on. */
function CompleteDialog({ sprint, project, planned, unfinished, onClose }: {
  sprint: Sprint; project: TrackerProject; planned: Sprint[]; unfinished: number; onClose: () => void;
}) {
  const qc = useQueryClient();
  const [moveTo, setMoveTo] = useState(planned[0]?.id ?? BACKLOG);
  const complete = useMutation({
    mutationFn: async () => {
      // "Новый спринт" creates the destination first.
      const dst = moveTo === "__new__" ? (await api.createSprint(project.key)).id : moveTo;
      return api.completeSprint(sprint.id, dst);
    },
    onSuccess: () => { qc.invalidateQueries(); onClose(); },
  });
  return <Dialog open onClose={() => !complete.isPending && onClose()} title={`Завершить ${sprint.name}`} footer={<>
    <Button onClick={onClose} disabled={complete.isPending}>Отмена</Button>
    <Button variant="primary" loading={complete.isPending} onClick={() => complete.mutate()}>Завершить</Button>
  </>}>
    <div className="issue-form">
      <p>{unfinished === 0 ? "Все тикеты спринта выполнены." : `Незавершённых тикетов: ${unfinished}. Готовые останутся в спринте для отчётов.`}</p>
      {unfinished > 0 && <Select label="Куда перенести незавершённые" value={moveTo} onChange={(e) => setMoveTo(e.target.value)}>
        <option value={BACKLOG}>В бэклог</option>
        {planned.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
        <option value="__new__">В новый спринт</option>
      </Select>}
      {complete.isError && <Alert tone="error" title="Спринт не завершён">{errorText(complete.error)}</Alert>}
    </div>
  </Dialog>;
}
