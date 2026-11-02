import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Alert, Avatar, Badge, EmptyState, Input, Spinner, Table } from "@villetacore/velessoft-ui";
import { api } from "../api/client";
import type { Person } from "../api/types";
import { longDate } from "../lib/date";
import { errorText } from "../lib/errors";
import { ROLE_LABEL, ROLE_TONE } from "../lib/people";

/**
 * PeoplePage is the team as Lumen knows it: everyone who signed in through
 * Keycloak, with groups and workspace role as of their last request.
 */
export default function PeoplePage() {
  const [q, setQ] = useState("");
  const { data: me } = useQuery({ queryKey: ["me"], queryFn: api.me });
  const { data, error, isLoading } = useQuery({ queryKey: ["users", q], queryFn: () => api.users(q) });
  const { data: groups } = useQuery({ queryKey: ["groups"], queryFn: api.groups });

  const columns = [
    { key: "name", header: "Человек", sortValue: (p: Person) => (p.name || p.username).toLowerCase(),
      render: (p: Person) => <span className="person"><Avatar name={p.name || p.username} size="sm" round />
        <span><strong>{p.name || p.username}</strong><span className="sub">{p.username}{p.email ? ` · ${p.email}` : ""}</span></span></span> },
    { key: "role", header: "Роль", sortValue: (p: Person) => p.role,
      render: (p: Person) => <Badge tone={ROLE_TONE[p.role]}>{ROLE_LABEL[p.role]}</Badge> },
    { key: "groups", header: "Группы", render: (p: Person) => p.groups.length ? p.groups.join(", ") : "—" },
    { key: "last", header: "Заходил", sortValue: (p: Person) => p.last_seen, render: (p: Person) => longDate(new Date(p.last_seen)) },
  ];

  return <div className="page">
    <div className="page-head">
      <div><span className="kicker">Пространство</span><h1>Команда</h1>
        <span className="sub">Учётные записи ведёт Keycloak; здесь — кто входил в Lumen, с группами и ролью.</span></div>
    </div>
    {me && me.mode !== "oidc" && <Alert tone="info" title="Вход без Keycloak">
      Сейчас Lumen работает {me.mode === "token" ? "по токену" : "в локальном режиме"}: справочник пополняется только при входе через single sign-on.
    </Alert>}
    <div className="toolbar"><div className="grow"><Input label="Поиск" placeholder="Имя, логин или почта" value={q} onChange={(e) => setQ(e.target.value)} /></div></div>
    {isLoading && <Spinner label="Загрузка команды" />}
    {error && <Alert tone="error" title="Справочник не загружен">{errorText(error)}</Alert>}
    {data && data.items.length === 0 && <EmptyState title={q ? "Никого не найдено" : "Пока никто не входил"} description="Люди появляются здесь после первого входа через Keycloak." />}
    {!!data?.items.length && <div className="issue-table"><Table columns={columns} rows={data.items} rowKey={(p) => p.username} hover compact /></div>}
    {!!groups?.items.length && <section>
      <h2 className="nav-title">Группы</h2>
      <ul className="group-list">{groups.items.map((g) => <li key={g.name}><strong>{g.name}</strong> <span className="sub">{g.users} чел.</span></li>)}</ul>
    </section>}
  </div>;
}
