"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, Badge, Button, EmptyState, Spinner, Switch } from "@villetacore/velessoft-ui";
import { api } from "../api/client";
import type { AutomationDefinition, AutomationRule } from "../api/types";
import AutomationDialog from "../components/AutomationDialog";
import Crumbs from "../components/Crumbs";
import Icon from "../components/Icon";
import ProjectTabs from "../components/ProjectTabs";
import { RUN_STATUS, describeRule, presets } from "../lib/automation";
import { errorText } from "../lib/errors";
import { Link, useRouteParam } from "../lib/navigation";
import { issueUrl } from "../lib/tracker";

const when = (iso: string) => new Date(iso).toLocaleString("ru-RU", { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" });

/** AutomationsPage lists a project's rules and what they have done. */
export default function AutomationsPage() {
  const key = useRouteParam(1);
  const qc = useQueryClient();
  const [editing, setEditing] = useState<{ ruleId?: string; initial?: AutomationDefinition } | null>(null);
  const { data: project, error: projectError } = useQuery({ queryKey: ["tracker", "project", key], queryFn: () => api.trackerProject(key) });
  const { data, error, isLoading } = useQuery({ queryKey: ["automations", key], queryFn: () => api.automations(key) });
  const { data: runs } = useQuery({ queryKey: ["automation-runs", key], queryFn: () => api.automationRuns(key) });
  const refresh = () => qc.invalidateQueries({ queryKey: ["automations", key] });

  const toggle = useMutation({
    mutationFn: (r: AutomationRule) => api.patchAutomation(r.id, { enabled: !r.enabled, revision: r.revision }),
    onSettled: refresh,
  });
  const remove = useMutation({ mutationFn: (id: string) => api.deleteAutomation(id), onSuccess: refresh });
  const run = useMutation({ mutationFn: (id: string) => api.runAutomation(id), onSuccess: refresh });

  if (projectError) return <Alert tone="error" title="Проект не найден">{errorText(projectError)}</Alert>;
  if (!project) return <Spinner label="Загрузка проекта" />;

  const rules = data?.items ?? [];
  const canEdit = !!data?.can_edit;
  const editingRule = rules.find((r) => r.id === editing?.ruleId);
  const actionError = toggle.error ?? remove.error ?? run.error;

  return <div className="page automations-page">
    <Crumbs items={[{ label: "Проекты", to: "/tracker" }, { label: `${project.key} · ${project.title}` }]} />
    <div className="page-head">
      <div><h1>{project.title}</h1><ProjectTabs projectKey={project.key} /></div>
      {canEdit && <div className="head-actions"><Button variant="primary" onClick={() => setEditing({})}><Icon name="plus" size={16} />Правило</Button></div>}
    </div>
    {isLoading && <Spinner label="Загрузка правил" />}
    {error && <Alert tone="error" title="Правила не загружены">{errorText(error)}</Alert>}
    {actionError && <Alert tone="error" title="Не получилось">{errorText(actionError)}</Alert>}
    {data && !canEdit && <p className="hint">Менять правила могут администраторы проекта.</p>}

    {data && rules.length === 0 && <EmptyState icon={<Icon name="zap" size={24} />} title="Правил пока нет"
      description="Правило само делает рутину: меняет поля, пишет комментарий, сообщает людям — по событию или по расписанию."
      actions={canEdit ? <div className="preset-list">
        {presets(project).map((p) => <Button key={p.title} onClick={() => setEditing({ initial: p.definition })}>{p.title}</Button>)}
      </div> : undefined} />}

    {rules.length > 0 && <ul className="automation-rules">
      {rules.map((r) => <li key={r.id} className={r.enabled ? "" : "is-off"}>
        <div className="automation-rule-main">
          <strong>{r.name}</strong>
          <p>{describeRule(r, project)}</p>
          <span className="sub">
            Действует с правами {r.owner || "сервиса"}
            {r.enabled && r.next_run && ` · следующий запуск ${when(r.next_run)}`}
          </span>
        </div>
        <div className="automation-rule-actions">
          <Switch label="Включено" checked={r.enabled} disabled={!canEdit || toggle.isPending} onChange={() => toggle.mutate(r)} />
          {canEdit && <>
            {r.trigger.type === "schedule" && r.enabled && <Button size="sm" loading={run.isPending && run.variables === r.id} onClick={() => run.mutate(r.id)}><Icon name="play" size={14} />Запустить</Button>}
            <Button size="sm" onClick={() => setEditing({ ruleId: r.id })}><Icon name="edit" size={14} />Изменить</Button>
            <Button size="sm" onClick={() => { if (window.confirm(`Удалить правило «${r.name}» и его журнал?`)) remove.mutate(r.id); }}><Icon name="trash" size={14} />Удалить</Button>
          </>}
        </div>
      </li>)}
    </ul>}

    {rules.length > 0 && <section aria-labelledby="runs-title" className="automation-runs">
      <h2 id="runs-title" className="nav-title">Журнал запусков</h2>
      {!runs?.items.length && <p className="hint">Правила ещё не срабатывали.</p>}
      {!!runs?.items.length && <ol>
        {runs.items.map((x) => <li key={x.id}>
          <time dateTime={x.started_at}>{when(x.started_at)}</time>
          <Badge tone={RUN_STATUS[x.status].tone}>{RUN_STATUS[x.status].label}</Badge>
          <span>{x.rule_name ?? "Удалённое правило"}</span>
          {x.issue
            ? <Link className="issue-key" href={issueUrl(x.issue)}>{x.issue}</Link>
            : <span className="sub">подошло тикетов: {x.matched}</span>}
          {x.error && <span className="automation-run-error">{x.error}</span>}
        </li>)}
      </ol>}
    </section>}

    {editing && <AutomationDialog project={project} rule={editing.ruleId ? editingRule : undefined}
      latestRevision={editingRule?.revision} initial={editing.initial} onClose={() => setEditing(null)} />}
  </div>;
}
