import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, Badge, Button, Input } from "@villetacore/velessoft-ui";
import { api } from "../api/client";
import type { Issue, TrackerProject } from "../api/types";
import { errorText } from "../lib/errors";
import { Link } from "../lib/navigation";
import { CATEGORY_TONE, issueUrl, statusName } from "../lib/tracker";
import Icon from "./Icon";

/** SubIssues lists the tickets nested under one, and adds a new one by its title. */
export default function SubIssues({ issue, project }: { issue: Issue; project?: TrackerProject }) {
  const qc = useQueryClient();
  const [title, setTitle] = useState("");
  const [adding, setAdding] = useState(false);
  const { data } = useQuery({ queryKey: ["issues", { parent: issue.key }], queryFn: () => api.issues({ parent: issue.key }) });
  const items = data?.items ?? [];
  const done = items.filter((i) => i.status_category === "done").length;
  const add = useMutation({
    mutationFn: () => api.createIssue({ project: issue.project, title: title.trim(), parent: issue.key }),
    onSuccess: () => { setTitle(""); qc.invalidateQueries(); },
  });
  return <section aria-labelledby="sub-title" className="sub-issues">
    <div className="section-head"><h2 id="sub-title" className="nav-title">Подзадачи{items.length > 0 && ` · ${done} из ${items.length}`}</h2>
      {!adding && <Button size="sm" onClick={() => setAdding(true)}><Icon name="plus" size={14} />Подзадача</Button>}
    </div>
    {items.length > 0 && <div className="note-list">
      {items.map((i) => <div className="note-row" key={i.key}>
        <Link className="issue-key" href={issueUrl(i.key)}>{i.key}</Link>
        <Link href={issueUrl(i.key)}>{i.title}</Link>
        <Badge tone={CATEGORY_TONE[i.status_category ?? "todo"]}>{statusName(project, i.status)}</Badge>
        {i.assignee && <span className="path">{i.assignee}</span>}
      </div>)}
    </div>}
    {adding && <form className="sub-issue-form" onSubmit={(e) => { e.preventDefault(); if (title.trim() && !add.isPending) add.mutate(); }}>
      <Input label="Название подзадачи" autoFocus value={title} onChange={(e) => setTitle(e.target.value)} />
      <Button type="submit" variant="primary" loading={add.isPending} disabled={!title.trim()}>Добавить</Button>
      <Button onClick={() => { setAdding(false); setTitle(""); }}>Закрыть</Button>
    </form>}
    {add.isError && <Alert tone="error" title="Подзадача не создана">{errorText(add.error)}</Alert>}
  </section>;
}
