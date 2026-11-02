import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Alert, Badge, EmptyState, Spinner } from "@villetacore/velessoft-ui";
import { api } from "../api/client";
import type { Issue } from "../api/types";
import { dueTone } from "../components/TaskRow";
import { relativeDue } from "../lib/date";
import { errorText } from "../lib/errors";
import { CATEGORY_TONE, issueUrl, priorityOf, typeLabel } from "../lib/tracker";

/** byDue puts dated issues first, soonest on top. */
function byDue(a: Issue, b: Issue) {
  return (a.due ?? "9999").localeCompare(b.due ?? "9999") || a.key.localeCompare(b.key);
}

function IssueList({ items, empty, statusName }: { items: Issue[]; empty: string; statusName: (i: Issue) => string }) {
  if (!items.length) return <p className="hint">{empty}</p>;
  return <ul className="my-issues">
    {items.map((i) => {
      const p = priorityOf(i.priority);
      return <li key={i.key}>
        <Link to={issueUrl(i.key)} className="my-issue">
          <span className="issue-key">{i.key}</span>
          <span className="my-issue-title">{i.title}</span>
          <span className="my-issue-meta">
            <span className="sub">{typeLabel(i.issue_type)}</span>
            {p && <Badge tone={p.tone}>{p.label}</Badge>}
            <Badge tone={CATEGORY_TONE[i.status_category ?? "todo"]}>{statusName(i)}</Badge>
            {i.due && <Badge tone={dueTone(i.due)}>{relativeDue(i.due)}</Badge>}
          </span>
        </Link>
      </li>;
    })}
  </ul>;
}

/** MyIssuesPage: what is assigned to me and what I follow, across projects. */
export default function MyIssuesPage() {
  const { data: me } = useQuery({ queryKey: ["me"], queryFn: api.me });
  const enabled = !!me?.username;
  const { data: projects } = useQuery({ queryKey: ["tracker", "projects"], queryFn: api.trackerProjects, enabled: !!me?.username });
  const statusName = (i: Issue) => projects?.items.find((p) => p.key === i.project)?.workflow.find((s) => s.id === i.status)?.name ?? i.status;
  const assigned = useQuery({ queryKey: ["issues", "my", "assigned"], queryFn: () => api.issues({ assignee: "me" }), enabled });
  const watching = useQuery({ queryKey: ["issues", "my", "watching"], queryFn: () => api.issues({ watcher: "me" }), enabled });

  const mine = (assigned.data?.items ?? []).filter((i) => i.status_category !== "done").sort(byDue);
  const doneCount = (assigned.data?.items ?? []).length - mine.length;
  const followed = (watching.data?.items ?? []).filter((i) => i.assignee !== me?.username && i.status_category !== "done").sort(byDue);

  return <div className="page">
    <div className="page-head">
      <div><span className="kicker">Трекер</span><h1>Мои задачи</h1>
        <span className="sub">Тикеты всех доступных вам проектов: назначенные вам и те, за которыми вы следите.</span></div>
    </div>
    {me && !me.username && <Alert tone="info" title="Нужен вход через Keycloak">
      «Мои задачи» строятся по вашему логину, а в {me.mode === "token" ? "режиме токена" : "локальном режиме"} его нет.
      Тикеты по исполнителю можно найти фильтром в списке проекта.
    </Alert>}
    {(assigned.isLoading || watching.isLoading) && enabled && <Spinner label="Загрузка" />}
    {[assigned.error, watching.error].map((e, i) => e && <Alert key={i} tone="error" title="Не загружено">{errorText(e)}</Alert>)}
    {enabled && assigned.data && <section aria-labelledby="my-assigned">
      <h2 id="my-assigned" className="nav-title">Назначены мне · {mine.length}</h2>
      <IssueList items={mine} statusName={statusName} empty={doneCount ? `Открытых нет; выполнено ${doneCount}.` : "Пока ничего не назначено."} />
    </section>}
    {enabled && watching.data && <section aria-labelledby="my-watching">
      <h2 id="my-watching" className="nav-title">Слежу · {followed.length}</h2>
      <IssueList items={followed} statusName={statusName} empty="Вы автор или наблюдатель открытых тикетов — они появятся здесь." />
    </section>}
    {enabled && assigned.data && watching.data && !mine.length && !followed.length && !doneCount &&
      <EmptyState title="Пока пусто" description="Тикеты появятся, когда вам их назначат или вы начнёте за ними следить." />}
  </div>;
}
