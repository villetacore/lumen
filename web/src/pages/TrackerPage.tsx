import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, Button, Dialog, EmptyState, Input, Select, Spinner } from "@villetacore/velessoft-ui";
import { api } from "../api/client";
import Icon from "../components/Icon";
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
      <div><span className="kicker">Трекер</span><h1>Проекты</h1><span className="sub">Тикеты хранятся Markdown-файлами в папке проекта.</span></div>
      <div className="head-actions"><Button variant="primary" onClick={() => setCreating(true)}><Icon name="plus" size={16} />Проект трекера</Button></div>
    </div>
    {isLoading && <Spinner label="Загрузка проектов" />}
    {error && <Alert tone="error" title="Проекты не загружены">{errorText(error)}</Alert>}
    {data && data.items.length === 0 && <EmptyState title="Проектов трекера пока нет"
      description="Создайте проект с ключом (например, WEB) или включите трекер у существующего проекта."
      actions={<Button variant="primary" onClick={() => setCreating(true)}>Создать проект</Button>} />}
    {!!data?.items.length && <ul className="tracker-projects">
      {data.items.map((p) => <li key={p.key}>
        <Link to={projectUrl(p.key)} className="tracker-project">
          <span className="issue-key">{p.key}</span>
          <strong>{p.title}</strong>
          <span className="sub">{openBy.get(p.key) ?? 0} открыто · {p.workflow.map((s) => s.name).join(" → ")}</span>
        </Link>
      </li>)}
    </ul>}
    {creating && <NewTrackerProjectDialog onClose={() => setCreating(false)} />}
  </div>;
}

function NewTrackerProjectDialog({ onClose }: { onClose: () => void }) {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [key, setKey] = useState("");
  const [title, setTitle] = useState("");
  const [note, setNote] = useState("");
  const { data: plain } = useQuery({ queryKey: ["notes", { type: "project" }], queryFn: () => api.notes({ type: "project" }) });
  const { data: tracked } = useQuery({ queryKey: ["tracker", "projects"], queryFn: api.trackerProjects });
  const used = new Set(tracked?.items.map((p) => p.path));
  const candidates = plain?.items.filter((n) => !used.has(n.path)) ?? [];
  const create = useMutation({
    mutationFn: () => api.createTrackerProject({ key: key.trim().toUpperCase(), title: note ? undefined : title.trim(), note: note || undefined }),
    onSuccess: (p) => { qc.invalidateQueries(); onClose(); navigate(projectUrl(p.key)); },
  });
  const validKey = /^[A-Z][A-Z0-9]{0,9}$/.test(key.trim().toUpperCase());
  const ready = validKey && (!!note || !!title.trim());

  return <Dialog open onClose={() => !create.isPending && onClose()} title="Проект трекера" footer={<>
    <Button onClick={onClose} disabled={create.isPending}>Отмена</Button>
    <Button type="submit" form="new-tracker-project" variant="primary" loading={create.isPending} disabled={!ready}>Создать</Button>
  </>}>
    <form id="new-tracker-project" className="issue-form" onSubmit={(e) => { e.preventDefault(); if (ready) create.mutate(); }}>
      <Input label="Ключ" required value={key} onChange={(e) => setKey(e.target.value.toUpperCase())} maxLength={10}
        hint="Латиница и цифры, с буквы: WEB → тикеты WEB-1, WEB-2…" error={key && !validKey ? "Например: WEB, LUM2" : undefined} />
      {!!candidates.length && <Select label="Проект" value={note} onChange={(e) => setNote(e.target.value)}
        hint="Можно включить трекер у существующего проекта: его заметка останется на месте">
        <option value="">Новый проект</option>
        {candidates.map((n) => <option key={n.path} value={n.path}>{n.title}</option>)}
      </Select>}
      {!note && <Input label="Название" required value={title} onChange={(e) => setTitle(e.target.value)} />}
      {create.isError && <Alert tone="error" title="Проект не создан">{errorText(create.error)}</Alert>}
    </form>
  </Dialog>;
}
