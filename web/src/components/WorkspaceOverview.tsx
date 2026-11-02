import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Button, Stat } from "@villetacore/velessoft-ui";
import type { Tone } from "@villetacore/velessoft-ui";
import { api } from "../api/client";
import type { ViewName } from "../api/types";
import Icon from "./Icon";
import { longDate, today } from "../lib/date";

const metrics: { view: ViewName; label: string; delta: string; tone?: Tone }[] = [
  { view: "today", label: "На сегодня", delta: "план и сроки" },
  { view: "inbox", label: "Входящие", delta: "не разобрано", tone: "info" },
  { view: "overdue", label: "Просрочено", delta: "срок прошёл", tone: "error" },
  { view: "done", label: "Выполнено", delta: "всего закрыто", tone: "success" },
];

function Metric({ view, label, delta, tone }: typeof metrics[number]) {
  const { data, isError } = useQuery({ queryKey: ["tasks", "count", view], queryFn: () => api.tasks({ view, limit: 1 }) });
  return <Link to={`/view/${view}`}>
    <Stat label={label} value={data?.total ?? "—"} delta={isError ? "не удалось загрузить" : delta} tone={data?.total ? tone : undefined} />
  </Link>;
}

export default function WorkspaceOverview({ onAdd, onDocument }: { onAdd: () => void; onDocument: () => void }) {
  return <>
    <div className="page-head">
      <div>
        <span className="kicker">Рабочее пространство / {longDate(today())}</span>
        <h1>Сегодня</h1>
        <span className="sub">Задачи, сроки и материалы студии в одном месте.</span>
      </div>
      <div className="head-actions">
        <Button onClick={onDocument}><Icon name="file" size={16} />Документ</Button>
        <Button variant="primary" onClick={onAdd}><Icon name="plus" size={16} />Новая задача</Button>
      </div>
    </div>
    <section className="stats" aria-label="Обзор задач">{metrics.map((m) => <Metric key={m.view} {...m} />)}</section>
  </>;
}
