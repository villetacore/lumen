"use client";

import { useEffect, useRef, useState } from "react";
import { Link, useLocation, useNavigate, useRouteParam } from "../lib/navigation";
import { useLeaveGuard } from "../lib/navigationGuard";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, Badge, Button, Input, Select, Spinner, Textarea } from "@villetacore/velessoft-ui";
import { api } from "../api/client";
import type { Issue, IssuePatch, Task, TrackerProject } from "../api/types";
import Crumbs from "../components/Crumbs";
import Icon from "../components/Icon";
import Markdown from "../components/Markdown";
import RelatedContent from "../components/RelatedContent";
import IssueHistory from "../components/IssueHistory";
import { longDate } from "../lib/date";
import { PeopleOptions } from "../lib/people";
import { errorText, isConflict, isNotFound } from "../lib/errors";
import {
  CATEGORY_TONE, PRIORITIES, allowedStatuses, issueUrl, projectUrl, splitList, statusName, typeLabel,
} from "../lib/tracker";

/** A draft remembers the issue revision and text it was started from. */
type Draft = { text: string; baseRevision: number; baseText: string };

export default function IssuePage() {
  const key = useRouteParam(1);
  const navigate = useNavigate();
  const location = useLocation();
  const qc = useQueryClient();
  const { data: issue, error, isLoading } = useQuery({
    queryKey: ["issue", key],
    queryFn: () => api.issue(key),
    retry: (n, err) => !isNotFound(err) && n < 1,
  });
  const { data: project } = useQuery({
    queryKey: ["tracker", "project", issue?.project],
    queryFn: () => api.trackerProject(issue!.project),
    enabled: !!issue?.project,
  });

  // Drafts live apart from the query, so a refetch after an SSE event never
  // replaces what the person is typing (same rule as NotePage, LUM-003).
  const [desc, setDesc] = useState<Draft | null>(null);
  const [title, setTitle] = useState<string | null>(null);
  const [comment, setComment] = useState("");
  const descDirty = !!desc && desc.text !== desc.baseText;
  const titleDirty = title !== null && title.trim() !== issue?.title;
  useLeaveGuard(descDirty || titleDirty || !!comment.trim());
  useEffect(() => { setDesc(null); setTitle(null); setComment(""); }, [key]);
  // A notification links to a comment by #id; it exists only after loading.
  useEffect(() => {
    if (issue && location.hash) document.getElementById(location.hash.slice(1))?.scrollIntoView({ block: "center" });
  }, [issue?.key, location.hash]);
  const { data: me } = useQuery({ queryKey: ["me"], queryFn: api.me });
  // Someone changed only fields (status, assignee…) elsewhere: the draft is
  // still based on the current description, so follow the new revision.
  useEffect(() => {
    if (issue && desc && issue.revision !== desc.baseRevision && (issue.description ?? "") === desc.baseText) {
      setDesc({ ...desc, baseRevision: issue.revision });
    }
  }, [issue?.revision]);

  // A field change made here bumps the revision; it must not look like a
  // foreign edit to an open description draft.
  const absorb = (next: Issue) => {
    setDesc((d) => d && (next.description ?? "") === d.baseText ? { ...d, baseRevision: next.revision } : d);
    qc.setQueryData(["issue", key], next);
    qc.invalidateQueries({ queryKey: ["issues"] });
  };
  const patch = useMutation({ mutationFn: (p: IssuePatch) => api.patchIssue(key, p), onSuccess: absorb });
  const toggleTask = useMutation({
    mutationFn: (t: Task) => api.patchTask(t.id, { status: t.status === "done" ? "todo" : "done" }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["issue", key] }),
  });
  const saveDesc = useMutation({
    mutationFn: ({ text, overwrite }: { text: string; overwrite: boolean }) =>
      api.patchIssue(key, { description: text, revision: overwrite ? undefined : desc?.baseRevision }),
    onSuccess: (next) => { setDesc(null); absorb(next); },
  });
  const saveTitle = useMutation({
    mutationFn: (t: string) => api.patchIssue(key, { title: t }),
    onSuccess: (next) => { setTitle(null); absorb(next); },
  });
  const addComment = useMutation({
    mutationFn: () => api.addComment(key, comment),
    onSuccess: () => { setComment(""); qc.invalidateQueries({ queryKey: ["issue", key] }); },
  });
  const remove = useMutation({
    mutationFn: () => api.deleteIssue(key),
    onSuccess: () => { qc.invalidateQueries(); navigate(project ? projectUrl(project.key) : "/tracker", { replace: true }); },
  });

  if (isLoading) return <Spinner label="Загрузка тикета" />;
  if (!issue) return <Alert tone="error" title="Не удалось открыть тикет" actions={<Button onClick={() => navigate("/tracker")}>К трекеру</Button>}>{errorText(error)}</Alert>;

  const changedOnDisk = !!desc && issue.revision !== desc.baseRevision && (issue.description ?? "") !== desc.baseText;
  const category = project?.workflow.find((s) => s.id === issue.status)?.category ?? issue.status_category ?? "todo";

  return <div className="page issue-card">
    <Crumbs items={[{ label: "Трекер", to: "/tracker" }, { label: project ? `${project.key} · ${project.title}` : issue.project, to: projectUrl(issue.project) }, { label: issue.key }]} />
    <div className="page-head">
      <div className="issue-head">
        <span className="kicker">{typeLabel(issue.issue_type)} · {issue.key}</span>
        {title === null
          ? <h1><button className="issue-title-btn" onClick={() => setTitle(issue.title)} title="Переименовать">{issue.title}</button></h1>
          : <form className="issue-title-edit" onSubmit={(e) => { e.preventDefault(); if (title.trim()) saveTitle.mutate(title.trim()); }}>
              <Input label="Название" value={title} autoFocus onChange={(e) => setTitle(e.target.value)}
                onKeyDown={(e) => { if (e.key === "Escape") setTitle(null); }} disabled={saveTitle.isPending} />
              <Button type="submit" variant="primary" loading={saveTitle.isPending} disabled={!title.trim()}>Сохранить</Button>
              <Button onClick={() => setTitle(null)} disabled={saveTitle.isPending}>Отмена</Button>
            </form>}
      </div>
      <div className="head-actions">
        {project && <Select label="Статус" className="status-select" value={issue.status} disabled={patch.isPending}
          onChange={(e) => patch.mutate({ status: e.target.value })}>
          {allowedStatuses(project, issue.status).map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
        </Select>}
        {me?.username && <WatchButton issue={issue} me={me.username} busy={patch.isPending}
          onChange={(watchers) => patch.mutate({ watchers })} />}
        <Button onClick={() => navigate(`/note/${issue.path}`)} title="Открыть файл тикета как заметку"><Icon name="file" size={16} />Файл</Button>
        <Button variant="ghost" onClick={() => { if (window.confirm(`Переместить ${issue.key} в корзину?`)) remove.mutate(); }} loading={remove.isPending}>
          <Icon name="trash" size={16} />Удалить
        </Button>
      </div>
    </div>
    {[patch, saveTitle, remove, toggleTask].map((m, i) => m.isError &&
      <Alert key={i} tone="error" title="Изменение не сохранено">{errorText(m.error)}</Alert>)}

    <div className="issue-layout">
      <div className="issue-main">
        <section aria-labelledby="desc-title">
          <div className="section-head"><h2 id="desc-title" className="nav-title">Описание</h2>
            {!desc && <Button size="sm" onClick={() => setDesc({ text: issue.description ?? "", baseRevision: issue.revision, baseText: issue.description ?? "" })}>
              <Icon name="edit" size={14} />Изменить</Button>}
          </div>
          {desc ? <div className="issue-editor">
            {changedOnDisk && <Alert tone="warning" title="Описание изменилось">
              Пока вы редактировали, тикет изменили в другом месте. Ваш текст сохранён в редакторе.
              <div className="alert-actions">
                <Button size="sm" onClick={() => setDesc(null)}>Взять новую версию</Button>
                <Button size="sm" onClick={() => saveDesc.mutate({ text: desc.text, overwrite: true })}>Сохранить поверх</Button>
              </div>
            </Alert>}
            <Textarea label="Описание тикета" className="document-input" value={desc.text} autoFocus
              onChange={(e) => setDesc({ ...desc, text: e.target.value })}
              onKeyDown={(e) => { if ((e.metaKey || e.ctrlKey) && e.key === "s") { e.preventDefault(); if (!changedOnDisk) saveDesc.mutate({ text: desc.text, overwrite: false }); } }}
              hint="Markdown: - [ ] чек-лист, [[ссылки]], ```mermaid. Ctrl/Cmd+S — сохранить" />
            {saveDesc.isError && <Alert tone="error" title={isConflict(saveDesc.error) ? "Тикет изменился" : "Описание не сохранено"}>
              {errorText(saveDesc.error)}</Alert>}
            <div className="editor-actions">
              <Button variant="primary" loading={saveDesc.isPending} disabled={changedOnDisk || !descDirty}
                onClick={() => saveDesc.mutate({ text: desc.text, overwrite: false })}>Сохранить описание</Button>
              <Button onClick={() => { if (!descDirty || window.confirm("Отменить изменения описания?")) setDesc(null); }}>Отмена</Button>
            </div>
          </div> : issue.description
            ? <Markdown body={issue.description} tasks={issue.checklist ?? []} onToggleTask={(t) => toggleTask.mutate(t)} />
            : <p className="hint">Описания нет.</p>}
        </section>

        <RelatedContent parent={`note:${issue.key}`} />

        {!!issue.links?.length && <section aria-labelledby="links-title">
          <h2 id="links-title" className="nav-title">Связи</h2>
          <ul className="issue-links">{issue.links.map((l) =>
            <li key={l.type + l.target}><span className="sub">{LINK_LABEL[l.type] ?? l.type}</span> <Link className="issue-key" href={issueUrl(l.target)}>{l.target}</Link></li>)}
          </ul>
        </section>}

        <details className="history-block">
          <summary>История изменений</summary>
          <IssueHistory issueKey={issue.key} project={project} />
        </details>

        <section aria-labelledby="comments-title">
          <h2 id="comments-title" className="nav-title">Комментарии · {issue.comments?.length ?? 0}</h2>
          <ol className="issue-comments">
            {issue.comments?.map((c) => <li key={c.id} id={c.id} className="issue-comment">
              <div className="comment-meta"><strong>{c.author || "без автора"}</strong>
                <time dateTime={c.at}>{new Date(c.at).toLocaleString("ru-RU", { dateStyle: "medium", timeStyle: "short" })}</time></div>
              <Markdown body={c.body} />
            </li>)}
          </ol>
          <form className="comment-form" onSubmit={(e) => { e.preventDefault(); if (comment.trim()) addComment.mutate(); }}>
            <Textarea label="Новый комментарий" value={comment} onChange={(e) => setComment(e.target.value)}
              onKeyDown={(e) => { if ((e.metaKey || e.ctrlKey) && e.key === "Enter" && comment.trim()) addComment.mutate(); }}
              hint="Markdown. Ctrl/Cmd+Enter — отправить" disabled={addComment.isPending} />
            {addComment.isError && <Alert tone="error" title="Комментарий не добавлен">{errorText(addComment.error)}</Alert>}
            <Button type="submit" variant="primary" loading={addComment.isPending} disabled={!comment.trim()}>Комментировать</Button>
          </form>
        </section>
      </div>

      <aside className="issue-side" aria-label="Свойства тикета">
        <Badge tone={CATEGORY_TONE[category]}>{statusName(project, issue.status)}</Badge>
        <IssueProps issue={issue} project={project} onPatch={(p) => patch.mutate(p)} busy={patch.isPending} />
      </aside>
    </div>
  </div>;
}

const LINK_LABEL: Record<string, string> = { blocks: "блокирует", "blocked-by": "заблокирован", relates: "связан с", duplicates: "дублирует" };

function IssueProps({ issue, project, onPatch, busy }: { issue: Issue; project?: TrackerProject; onPatch: (p: IssuePatch) => void; busy: boolean }) {
  return <dl className="props issue-props">
    <PeopleOptions id="people-options" />
    <div><dt>Тип</dt><dd>
      <Select label="Тип" className="prop-control" value={issue.issue_type} disabled={busy} onChange={(e) => onPatch({ issue_type: e.target.value })}>
        {(project?.issue_types ?? [issue.issue_type]).map((t) => <option key={t} value={t}>{typeLabel(t)}</option>)}
      </Select></dd></div>
    <div><dt>Приоритет</dt><dd>
      <Select label="Приоритет" className="prop-control" value={issue.priority ?? ""} disabled={busy} onChange={(e) => onPatch({ priority: e.target.value })}>
        <option value="">Не задан</option>
        {issue.priority && !PRIORITIES.some((p) => p.id === issue.priority) && <option value={issue.priority}>{issue.priority}</option>}
        {PRIORITIES.map((p) => <option key={p.id} value={p.id}>{p.label}</option>)}
      </Select></dd></div>
    <PropText label="Исполнитель" value={issue.assignee ?? ""} list="people-options" onCommit={(v) => onPatch({ assignee: v })} />
    <div><dt>Автор</dt><dd>{issue.reporter || "—"}</dd></div>
    <PropText label="Срок" type="date" value={issue.due ?? ""} onCommit={(v) => onPatch({ due: v })} />
    <PropText label="Начало" type="date" value={issue.start ?? ""} onCommit={(v) => onPatch({ start: v })} />
    <PropText label="Метки" value={issue.labels?.join(", ") ?? ""} onCommit={(v) => onPatch({ labels: splitList(v) })} />
    <PropText label="Спринт" value={issue.sprint ?? ""} onCommit={(v) => onPatch({ sprint: v })} />
    <PropText label="Эпик" value={issue.epic ?? ""} onCommit={(v) => onPatch({ epic: v })} />
    <PropText label="Оценка" value={issue.estimate ?? ""} onCommit={(v) => onPatch({ estimate: v })} />
    <PropText label="Story points" type="number" value={issue.points ? String(issue.points) : ""} onCommit={(v) => onPatch({ points: Number(v) || 0 })} />
    <PropText label="Наблюдатели" value={issue.watchers?.join(", ") ?? ""} onCommit={(v) => onPatch({ watchers: splitList(v) })} />
    {issue.created && <div><dt>Создан</dt><dd>{longDate(new Date(issue.created))}</dd></div>}
    {issue.updated && <div><dt>Обновлён</dt><dd>{longDate(new Date(issue.updated))}</dd></div>}
  </dl>;
}

/**
 * PropText edits one property in place: Enter or leaving the field saves,
 * Escape restores. While focused it ignores server refetches.
 */
function PropText({ label, value, type = "text", placeholder, list, onCommit }: {
  label: string; value: string; type?: string; placeholder?: string; list?: string; onCommit: (v: string) => void;
}) {
  const [text, setText] = useState(value);
  const [focused, setFocused] = useState(false);
  // The last value sent, so Enter followed by blur saves once.
  const sent = useRef(value);
  useEffect(() => { sent.current = value; if (!focused) setText(value); }, [value, focused]);
  const send = (v: string) => {
    if (v === sent.current) return;
    sent.current = v;
    onCommit(v);
  };
  return <div><dt>{label}</dt><dd>
    <Input label={label} className="prop-control" type={type} value={text} placeholder={placeholder ?? "—"} list={list}
      onFocus={() => setFocused(true)}
      onBlur={() => { setFocused(false); send(text.trim()); }}
      // A date picker has no "typing" state: each pick is a decision.
      onChange={(e) => { setText(e.target.value); if (type === "date") send(e.target.value); }}
      onKeyDown={(e) => {
        if (e.key === "Enter") { e.preventDefault(); send(text.trim()); }
        if (e.key === "Escape") { setText(value); sent.current = value; requestAnimationFrame(() => (e.target as HTMLInputElement).blur()); }
      }} />
  </dd></div>;
}

/**
 * WatchButton follows an issue. The reporter and the assignee follow it by
 * virtue of their role; others add themselves to "watchers".
 */
function WatchButton({ issue, me, busy, onChange }: { issue: Issue; me: string; busy: boolean; onChange: (w: string[]) => void }) {
  const explicit = issue.watchers ?? [];
  const role = issue.assignee === me ? "исполнитель" : issue.reporter === me ? "автор" : "";
  if (role) {
    return <Button disabled title={`Вы получаете уведомления как ${role}`}><Icon name="eye" size={16} />Слежу</Button>;
  }
  const on = explicit.includes(me);
  return <Button onClick={() => onChange(on ? explicit.filter((w) => w !== me) : [...explicit, me])} disabled={busy}
    aria-pressed={on} title={on ? "Перестать получать уведомления" : "Получать уведомления о комментариях и статусе"}>
    <Icon name={on ? "eyeOff" : "eye"} size={16} />{on ? "Не следить" : "Следить"}
  </Button>;
}

