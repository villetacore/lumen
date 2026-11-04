"use client";

import { useState } from "react";
import { useRouteParam } from "../lib/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, Badge, Button, EmptyState, IconButton, Input, Select, Spinner } from "@villetacore/velessoft-ui";
import { api } from "../api/client";
import type { ProjectMember, WorkspaceRole } from "../api/types";
import Crumbs from "../components/Crumbs";
import Icon from "../components/Icon";
import ProjectTabs from "../components/ProjectTabs";
import { longDate } from "../lib/date";
import { errorText } from "../lib/errors";
import { ROLE_LABEL, ROLE_TONE, personLabel, usePeople } from "../lib/people";

const ROLES: WorkspaceRole[] = ["viewer", "member", "admin"];

/**
 * AccessPage shows who can open a tracker project and lets its admins change
 * that (ADR-0012). Adding the first member closes an open project.
 */
export default function AccessPage() {
  const key = useRouteParam(1);
  const qc = useQueryClient();
  const { data: project, error: projectError } = useQuery({ queryKey: ["tracker", "project", key], queryFn: () => api.trackerProject(key) });
  const { data: access, error, isLoading } = useQuery({ queryKey: ["access", key], queryFn: () => api.projectAccess(key) });
  const { data: people } = usePeople();
  const { data: groups } = useQuery({ queryKey: ["groups"], queryFn: api.groups, staleTime: 60_000 });
  const [kind, setKind] = useState<"user" | "group">("user");
  const [name, setName] = useState("");
  const [role, setRole] = useState<WorkspaceRole>("member");

  const done = () => { qc.invalidateQueries(); };
  const set = useMutation({
    mutationFn: (m: { kind: "user" | "group"; name: string; role: WorkspaceRole }) => api.setMember(key, m),
    onSuccess: (acc) => { qc.setQueryData(["access", key], acc); setName(""); done(); },
  });
  const remove = useMutation({
    mutationFn: (m: ProjectMember) => api.removeMember(key, m.kind, m.name),
    onSuccess: (acc) => { qc.setQueryData(["access", key], acc); done(); },
  });

  if (projectError) return <Alert tone="error" title="Проект не найден">{errorText(projectError)}</Alert>;
  if (!project || isLoading) return <Spinner label="Загрузка доступа" />;
  if (!access) return <Alert tone="error" title="Доступ не загружен">{errorText(error)}</Alert>;

  const canEdit = access.my_role === "admin";
  const byName = new Map(people?.items.map((p) => [p.username, p]));
  const add = () => {
    const n = name.trim().replace(/^\//, "");
    if (!n) return;
    if (access.open && !window.confirm(
      `Проект ${project.key} станет закрытым: его увидят только участники из списка и администраторы пространства. Продолжить?`)) return;
    set.mutate({ kind, name: n, role });
  };
  const drop = (m: ProjectMember) => {
    const last = access.items.length === 1;
    const msg = last
      ? `Убрать последнего участника? Проект ${project.key} станет открытым для всего пространства.`
      : `Убрать ${m.kind === "group" ? "группу" : ""} ${m.name} из проекта?`;
    if (window.confirm(msg)) remove.mutate(m);
  };

  return <div className="page access-page">
    <Crumbs items={[{ label: "Проекты", to: "/tracker" }, { label: `${project.key} · ${project.title}` }]} />
    <div className="page-head">
      <div><h1>{project.title}</h1><ProjectTabs projectKey={project.key} /></div>
    </div>

    <div className={`access-status ${access.open ? "is-open" : "is-closed"}`}>
      <Icon name={access.open ? "unlock" : "lock"} size={18} />
      <div>
        <strong>{access.open ? "Открытый проект" : "Закрытый проект"}</strong>
        <p>{access.open
          ? "Проект видят все в пространстве: читатели читают, участники и администраторы меняют. Добавьте участника, чтобы закрыть проект."
          : "Проект видят только участники ниже и администраторы пространства. Для остальных его тикетов, документов и событий не существует."}</p>
      </div>
      <Badge tone={ROLE_TONE[access.my_role]}>Ваша роль: {ROLE_LABEL[access.my_role]}</Badge>
    </div>
    {[set.error, remove.error].map((e, i) => e && <Alert key={i} tone="error" title="Доступ не изменён">{errorText(e)}</Alert>)}

    {access.items.length === 0
      ? <EmptyState title="Участников нет" description={canEdit ? "Проект открыт. Первый участник закроет его." : "Проект открыт всему пространству."} />
      : <ul className="access-list">
        {access.items.map((m) => <li key={m.kind + m.name} className="access-row">
          <Icon name={m.kind === "group" ? "users" : "user"} size={16} />
          <span className="access-name">
            {m.kind === "group" ? <>Группа <strong>{m.name}</strong></> : <strong>{personLabel(byName.get(m.name), m.name)}</strong>}
            <span className="sub">добавлен {longDate(new Date(m.added_at))}{m.added_by ? ` · ${m.added_by}` : ""}</span>
          </span>
          {canEdit
            ? <Select label={`Роль ${m.name}`} className="access-role" value={m.role} disabled={set.isPending}
                onChange={(e) => set.mutate({ kind: m.kind, name: m.name, role: e.target.value as WorkspaceRole })}>
                {ROLES.map((r) => <option key={r} value={r}>{ROLE_LABEL[r]}</option>)}
              </Select>
            : <Badge tone={ROLE_TONE[m.role]}>{ROLE_LABEL[m.role]}</Badge>}
          {canEdit && <IconButton variant="ghost" label={`Убрать ${m.name}`} onClick={() => drop(m)} disabled={remove.isPending}>
            <Icon name="trash" size={16} />
          </IconButton>}
        </li>)}
      </ul>}

    {canEdit
      ? <form className="access-add" onSubmit={(e) => { e.preventDefault(); add(); }}>
        <Select label="Кого" value={kind} onChange={(e) => { setKind(e.target.value as "user" | "group"); setName(""); }}>
          <option value="user">Пользователя</option>
          <option value="group">Группу Keycloak</option>
        </Select>
        <div className="grow">
          <Input label={kind === "user" ? "Имя пользователя" : "Группа"} value={name} onChange={(e) => setName(e.target.value)}
            list={`access-${kind}`} placeholder={kind === "user" ? "alice" : "designers"} required />
          <datalist id="access-user">
            {people?.items.map((p) => <option key={p.username} value={p.username}>{p.name || p.username}</option>)}
          </datalist>
          <datalist id="access-group">
            {groups?.items.map((g) => <option key={g.name} value={g.name}>{g.users} чел.</option>)}
          </datalist>
        </div>
        <Select label="Роль" value={role} onChange={(e) => setRole(e.target.value as WorkspaceRole)}>
          {ROLES.map((r) => <option key={r} value={r}>{ROLE_LABEL[r]}</option>)}
        </Select>
        <Button type="submit" variant="primary" loading={set.isPending} disabled={!name.trim()}>Добавить</Button>
        <p className="hint access-hint">{kind === "user"
          ? "Справочник пополняется, когда люди входят через Keycloak; можно вписать и того, кто ещё не входил."
          : "Имя группы Keycloak без ведущего «/»."}</p>
      </form>
      : <p className="hint">Менять доступ могут администраторы проекта и пространства{access.open ? "; закрыть открытый проект — только администратор пространства" : ""}.</p>}
  </div>;
}
