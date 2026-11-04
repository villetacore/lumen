import { Link, useNavigate } from "react-router-dom";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Button } from "@villetacore/velessoft-ui";
import { api } from "../api/client";
import Icon from "./Icon";
import { iso, longDate, today } from "../lib/date";
import { IssueList, byDue } from "../pages/MyIssuesPage";

/** The head of the home page: the date and the two things people start a day with. */
export default function WorkspaceOverview({ onAdd }: { onAdd: () => void }) {
  const navigate = useNavigate();
  // The daily note is created on first open, the way quick capture does it.
  const daily = useMutation({
    mutationFn: () => api.daily(iso(today()), true),
    onSuccess: (n) => navigate(`/note/${n.path}`),
  });
  return <div className="page-head">
    <div>
      <span className="kicker">{longDate(today())}</span>
      <h1>Сегодня</h1>
    </div>
    <div className="head-actions">
      <Button onClick={() => daily.mutate()} disabled={daily.isPending}><Icon name="file" size={16} />Заметка дня</Button>
      <Button variant="primary" onClick={onAdd}><Icon name="plus" size={16} />Новая задача</Button>
    </div>
  </div>;
}

const SHOWN = 5;

/** What is assigned to me across projects, soonest first. Nothing without a login. */
export function MyTickets() {
  const { data: me } = useQuery({ queryKey: ["me"], queryFn: api.me });
  const enabled = !!me?.username;
  const { data } = useQuery({ queryKey: ["issues", "my", "assigned"], queryFn: () => api.issues({ assignee: "me" }), enabled });
  const { data: projects } = useQuery({ queryKey: ["tracker", "projects"], queryFn: api.trackerProjects, enabled });
  if (!enabled || !data) return null;
  const mine = data.items.filter((i) => i.status_category !== "done").sort(byDue);
  if (!mine.length) return null;
  const statusName = (key: string, id: string) => projects?.items.find((p) => p.key === key)?.workflow.find((s) => s.id === id)?.name ?? id;
  return <section className="today-tickets" aria-label="Мои тикеты">
    <div className="section-title"><h2>Мои тикеты</h2>{mine.length > SHOWN && <Link to="/my">Все {mine.length} <Icon name="chevron" size={14} /></Link>}</div>
    <IssueList items={mine.slice(0, SHOWN)} empty="" statusName={(i) => statusName(i.project, i.status)} />
  </section>;
}

/** The first-run path, until the workspace has a project. */
export function GettingStarted({ onProject }: { onProject: () => void }) {
  const { data: projects } = useQuery({ queryKey: ["tracker", "projects"], queryFn: api.trackerProjects });
  if (!projects || projects.items.length) return null;
  return <section className="card getting-started" aria-label="С чего начать">
    <h2>С чего начать</h2>
    <ol>
      <li><button className="link-button" onClick={onProject}>Создайте проект</button> — у него сразу будет доска.</li>
      <li>Добавьте тикеты и назначьте исполнителей.</li>
      <li>Подключите <Link to="/integrations">календарь и GitLab</Link>, если они нужны.</li>
    </ol>
    <p className="hint">Для личных дел есть задачи: поле ниже, без всякого проекта.</p>
  </section>;
}
