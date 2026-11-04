import { useState } from "react";
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Alert, Button, EmptyState, Spinner } from "@villetacore/velessoft-ui";
import { api } from "../api/client";
import Icon from "../components/Icon";
import NewProjectDialog from "../components/NewProjectDialog";
import { errorText } from "../lib/errors";
import { projectUrl } from "../lib/tracker";

/** TrackerPage lists projects that have an issue key, and starts new ones. */
export default function TrackerPage() {
  const [creating, setCreating] = useState(false);
  const { data, error, isLoading } = useQuery({ queryKey: ["tracker", "projects"], queryFn: api.trackerProjects });
  const { data: open } = useQuery({ queryKey: ["issues", { category: "open" }], queryFn: () => api.issues({}) });
  const openBy = new Map<string, number>();
  for (const is of open?.items ?? []) {
    if (is.status_category !== "done") openBy.set(is.project, (openBy.get(is.project) ?? 0) + 1);
  }

  return <div className="page">
    <div className="page-head">
      <div><h1>Проекты</h1><span className="sub">Тикеты хранятся Markdown-файлами в папке проекта.</span></div>
      <div className="head-actions"><Button variant="primary" onClick={() => setCreating(true)}><Icon name="plus" size={16} />Проект</Button></div>
    </div>
    {isLoading && <Spinner label="Загрузка проектов" />}
    {error && <Alert tone="error" title="Проекты не загружены">{errorText(error)}</Alert>}
    {data && data.items.length === 0 && <EmptyState title="Проектов пока нет"
      description="Создайте проект: у него сразу будет доска для тикетов."
      actions={<Button variant="primary" onClick={() => setCreating(true)}>Создать проект</Button>} />}
    {!!data?.items.length && <ul className="tracker-projects">
      {data.items.map((p) => <li key={p.key}>
        <Link to={`${projectUrl(p.key)}/board`} className="tracker-project">
          <span className="issue-key">{p.key}</span>
          <strong>{p.title}</strong>
          <span className="sub">{openBy.get(p.key) ?? 0} открыто · {p.workflow.map((s) => s.name).join(" → ")}</span>
        </Link>
      </li>)}
    </ul>}
    {creating && <NewProjectDialog onClose={() => setCreating(false)} />}
  </div>;
}
