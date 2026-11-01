import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import type { ViewName } from "../api/types";
import Icon, { type IconName } from "./Icon";
import { longDate, today } from "../lib/date";

const metrics: { view: ViewName; label: string; icon: IconName; tone: string }[] = [
  { view: "today", label: "задач на сегодня", icon: "sun", tone: "" },
  { view: "inbox", label: "во входящих", icon: "inbox", tone: "lime" },
  { view: "overdue", label: "просроченных задач", icon: "clock", tone: "warn" },
  { view: "done", label: "выполненных задач", icon: "check", tone: "ok" },
];
function Metric({ view, label, icon, tone }: typeof metrics[number]) {
  const { data, isError } = useQuery({ queryKey: ["tasks", "count", view], queryFn: () => api.tasks({ view, limit: 1 }) });
  const count = data?.total;
  const noun = (one: string, few: string, many: string) => count === undefined ? many : count % 10 === 1 && count % 100 !== 11 ? one : count % 10 >= 2 && count % 10 <= 4 && (count % 100 < 12 || count % 100 > 14) ? few : many;
  const caption = view === "today" ? `${noun("задача", "задачи", "задач")} на сегодня` : view === "overdue" ? noun("просроченная задача", "просроченные задачи", "просроченных задач") : view === "done" ? noun("выполненная задача", "выполненные задачи", "выполненных задач") : label;
  return <Link className="card stat" to={`/view/${view}`}><span className={`stat-icon ${tone}`}><Icon name={icon} /></span><div><div className="stat-value">{data?.total ?? "—"}</div><div className="stat-label">{isError ? "Не удалось загрузить" : caption}</div></div></Link>;
}
export default function WorkspaceOverview({ onAdd, onDocument }: { onAdd: () => void; onDocument: () => void }) {
  return <>
    <section className="workspace-intro">
      <div className="workspace-kicker"><span>Lumen / Рабочее пространство</span><span>by velessoft</span></div>
      <div className="workspace-heading">
        <div><h1>Сегодня<span aria-hidden="true">_</span></h1><p>{longDate(today())}</p></div>
        <div className="workspace-actions"><button className="btn" onClick={onDocument}><Icon name="file" size={20} />Документ</button><button className="btn primary" onClick={onAdd}><Icon name="plus" size={20} />Новая задача</button></div>
      </div>
    </section>
    <section className="stats" aria-label="Обзор задач">{metrics.map(m => <Metric key={m.view} {...m} />)}</section>
  </>;
}
