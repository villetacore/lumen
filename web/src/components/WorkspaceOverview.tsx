import { Link, useNavigate } from "react-router-dom";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Button } from "@villetacore/velessoft-ui";
import { api } from "../api/client";
import Icon from "./Icon";
import { iso, longDate, today } from "../lib/date";
import { IssueList, byDue } from "../screens/MyIssuesPage";

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
    <dl className="system-map">
      <dt>Проект</dt><dd>Общая работа: доска, тикеты, спринты и документы. <button className="link-button" onClick={onProject}>Создать проект</button></dd>
      <dt>Тикет</dt><dd>Работа внутри проекта: статус, исполнитель, срок, обсуждение.</dd>
      <dt>Задача</dt><dd>Быстрое дело для себя: строка с галочкой в любой заметке. Выросла — сделайте её тикетом.</dd>
      <dt>Документ</dt><dd>Знания, которые остаются: регламенты, описания, решения. В проекте или общие.</dd>
      <dt>Заметка</dt><dd>Рабочие записи для себя: мысли, протоколы встреч, дневник.</dd>
    </dl>
    <p className="hint">Календари, GitLab и другие сервисы подключаются в разделе <Link to="/integrations">Интеграции</Link>.</p>
  </section>;
}
