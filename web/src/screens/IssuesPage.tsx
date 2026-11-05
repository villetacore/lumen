"use client";

import { useState } from "react";
import { Link, useNavigate, useRouteParam, useQueryParams } from "../lib/navigation";
import { useQuery } from "@tanstack/react-query";
import { Alert, Badge, Button, EmptyState, Input, Select, Spinner, Table } from "@villetacore/velessoft-ui";
import { api } from "../api/client";
import type { Issue, IssueFilter } from "../api/types";
import Crumbs from "../components/Crumbs";
import Icon from "../components/Icon";
import NewIssueDialog from "../components/NewIssueDialog";
import ProjectTabs from "../components/ProjectTabs";
import { dueTone } from "../components/TaskRow";
import { relativeDue } from "../lib/date";
import { errorText } from "../lib/errors";
import { PeopleOptions } from "../lib/people";
import {
  CATEGORY_TONE, PRIORITIES, type SavedFilter, filterFromParams, filterToParams, issueUrl,
  loadSavedFilters, priorityOf, statusName, storeSavedFilters, typeLabel,
} from "../lib/tracker";

/** IssuesPage is a project's issue list with filters kept in the URL. */
export default function IssuesPage() {
  const key = useRouteParam(1);
  const navigate = useNavigate();
  const [params, setParams] = useQueryParams();
  const filter = filterFromParams(params);
  const [creating, setCreating] = useState(false);
  const [saved, setSaved] = useState<SavedFilter[]>(() => loadSavedFilters(key));

  const { data: project, error: projectError } = useQuery({ queryKey: ["tracker", "project", key], queryFn: () => api.trackerProject(key) });
  const { data, error, isLoading } = useQuery({
    queryKey: ["issues", key, filter],
    // "all" is the board's "every sprint" scope, not a sprint id.
    queryFn: () => api.issues({ ...filter, sprint: filter.sprint === "all" ? undefined : filter.sprint, project: key }),
  });

  const update = (patch: Partial<IssueFilter>) => setParams(filterToParams({ ...filter, ...patch }), { replace: true });
  const query = params.toString();
  const activeSaved = saved.find((s) => s.query === query)?.name ?? "";
  const saveCurrent = () => {
    const name = window.prompt("Название фильтра")?.trim();
    if (!name) return;
    const next = [...saved.filter((s) => s.name !== name), { name, query }];
    setSaved(next);
    storeSavedFilters(key, next);
  };
  const removeSaved = () => {
    const next = saved.filter((s) => s.name !== activeSaved);
    setSaved(next);
    storeSavedFilters(key, next);
  };

  if (projectError) return <Alert tone="error" title="Проект не найден">{errorText(projectError)}</Alert>;

  const columns = [
    { key: "key", header: "Ключ", sortValue: (i: Issue) => Number(i.key.split("-").pop()),
      render: (i: Issue) => <Link className="issue-key" href={issueUrl(i.key)}>{i.key}</Link> },
    { key: "title", header: "Название", sortValue: (i: Issue) => i.title.toLowerCase(),
      render: (i: Issue) => <Link className="issue-title-link" href={issueUrl(i.key)}>{i.title}</Link> },
    { key: "type", header: "Тип", render: (i: Issue) => typeLabel(i.issue_type) },
    { key: "status", header: "Статус", sortValue: (i: Issue) => project?.workflow.findIndex((s) => s.id === i.status) ?? 0,
      render: (i: Issue) => <Badge tone={CATEGORY_TONE[i.status_category ?? "todo"]}>{statusName(project, i.status)}</Badge> },
    { key: "priority", header: "Приоритет", sortValue: (i: Issue) => { const n = PRIORITIES.findIndex((p) => p.id === i.priority); return n < 0 ? 99 : n; },
      render: (i: Issue) => { const p = priorityOf(i.priority); return p ? <Badge tone={p.tone}>{p.label}</Badge> : (i.priority || "—"); } },
    { key: "assignee", header: "Исполнитель", sortValue: (i: Issue) => i.assignee ?? "", render: (i: Issue) => i.assignee || "—" },
    { key: "due", header: "Срок", sortValue: (i: Issue) => i.due ?? "9999",
      render: (i: Issue) => i.due ? <Badge tone={i.status_category === "done" ? "neutral" : dueTone(i.due)}>{relativeDue(i.due)}</Badge> : "—" },
    { key: "labels", header: "Метки", render: (i: Issue) => i.labels?.map((l) => <span key={l} className="md-tag issue-label">#{l}</span>) ?? null },
  ];
  const hasFilter = query !== "";

  return <div className="page">
    <Crumbs items={[{ label: "Проекты", to: "/tracker" }, { label: project ? `${project.key} · ${project.title}` : key }]} />
    <div className="page-head">
      <div><h1>{project?.title ?? key}</h1>
        <span className="sub">{data ? `${issueCount(data.total)}${hasFilter ? " по фильтру" : ""}` : " "}</span>
        <ProjectTabs projectKey={key} /></div>
      <div className="head-actions">
        {project && <Button onClick={() => navigate(`/note/${project.path}`)}><Icon name="folder" size={16} />Заметка проекта</Button>}
        <Button variant="primary" onClick={() => setCreating(true)}><Icon name="plus" size={16} />Тикет</Button>
      </div>
    </div>

    <div className="toolbar issue-filters" role="search" aria-label="Фильтр тикетов">
      <div className="grow"><Input label="Поиск" placeholder="Название или ключ" value={filter.q ?? ""} onChange={(e) => update({ q: e.target.value })} /></div>
      <Select label="Статус" value={filter.category ? `c:${filter.category}` : filter.status?.[0] ?? ""}
        onChange={(e) => { const v = e.target.value; update(v.startsWith("c:") ? { category: v.slice(2) as IssueFilter["category"], status: [] } : { status: v ? [v] : [], category: "" }); }}>
        <option value="">Все</option>
        <option value="c:todo">Не начатые</option>
        <option value="c:in_progress">В работе</option>
        <option value="c:done">Завершённые</option>
        {project?.workflow.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
      </Select>
      <Select label="Тип" value={filter.type ?? ""} onChange={(e) => update({ type: e.target.value })}>
        <option value="">Все</option>
        {project?.issue_types.map((t) => <option key={t} value={t}>{typeLabel(t)}</option>)}
      </Select>
      <Input label="Исполнитель" placeholder="имя или none" list="filter-people" value={filter.assignee ?? ""} onChange={(e) => update({ assignee: e.target.value.trim() })} />
      <PeopleOptions id="filter-people" />
      <Input label="Метка" value={filter.label?.join(",") ?? ""} onChange={(e) => update({ label: e.target.value.split(",").map((s) => s.trim()).filter(Boolean) })} />
      <Select label="Сохранённые" value={activeSaved}
        onChange={(e) => { const s = saved.find((x) => x.name === e.target.value); setParams(new URLSearchParams(s?.query ?? ""), { replace: true }); }}>
        <option value="">—</option>
        {saved.map((s) => <option key={s.name} value={s.name}>{s.name}</option>)}
      </Select>
      <div className="filter-actions">
        {hasFilter && !activeSaved && <Button onClick={saveCurrent}>Сохранить</Button>}
        {activeSaved && <Button onClick={removeSaved}>Удалить «{activeSaved}»</Button>}
        {hasFilter && <Button variant="ghost" onClick={() => setParams(new URLSearchParams(), { replace: true })}>Сбросить</Button>}
      </div>
    </div>

    {isLoading && <Spinner label="Загрузка тикетов" />}
    {error && <Alert tone="error" title="Тикеты не загружены">{errorText(error)}</Alert>}
    {data && data.items.length === 0 && (hasFilter
      ? <EmptyState title="Ничего не найдено" description="Измените или сбросьте фильтр." />
      : <EmptyState title="Тикетов пока нет" description="Создайте первый — он появится файлом в папке проекта."
          actions={<Button variant="primary" onClick={() => setCreating(true)}>Создать тикет</Button>} />)}
    {!!data?.items.length && <div className="issue-table"><Table columns={columns} rows={data.items} rowKey={(i) => i.key} hover compact /></div>}
    {creating && <NewIssueDialog project={key} onClose={() => setCreating(false)} />}
  </div>;
}

function issueCount(n: number): string {
  const mod10 = n % 10, mod100 = n % 100;
  const word = mod10 === 1 && mod100 !== 11 ? "тикет" : mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14) ? "тикета" : "тикетов";
  return `${n} ${word}`;
}
