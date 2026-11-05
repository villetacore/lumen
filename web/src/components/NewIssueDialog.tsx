import { useEffect, useRef, useState } from "react";
import { useNavigate } from "../lib/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, Button, Dialog, Input, Select, Textarea } from "@villetacore/velessoft-ui";
import { api } from "../api/client";
import { errorText } from "../lib/errors";
import { PeopleOptions } from "../lib/people";
import { PRIORITIES, issueUrl, splitList, typeLabel } from "../lib/tracker";

/** NewIssueDialog creates a tracker issue; the server assigns its key. */
export default function NewIssueDialog({ project, status, sprint, onClose }: { project?: string; status?: string; sprint?: string; onClose: () => void }) {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const { data: projects } = useQuery({ queryKey: ["tracker", "projects"], queryFn: api.trackerProjects });
  const [form, setForm] = useState({ project: project ?? "", title: "", issue_type: "", priority: "", assignee: "", labels: "", due: "", description: "" });
  const set = (k: keyof typeof form) => (e: { target: { value: string } }) => setForm((f) => ({ ...f, [k]: e.target.value }));
  const titleInput = useRef<HTMLInputElement>(null);
  useEffect(() => {
    const frame = requestAnimationFrame(() => titleInput.current?.focus());
    return () => cancelAnimationFrame(frame);
  }, []);
  useEffect(() => {
    if (!form.project && projects?.items.length) setForm((f) => ({ ...f, project: projects.items[0].key }));
  }, [projects]);
  const current = projects?.items.find((p) => p.key === form.project);

  const dirty = !!(form.title || form.description);
  const create = useMutation({
    mutationFn: () => api.createIssue({
      project: form.project, title: form.title.trim(), status, sprint,
      issue_type: form.issue_type || undefined, priority: form.priority || undefined,
      assignee: form.assignee.trim() || undefined, labels: splitList(form.labels),
      due: form.due || undefined, description: form.description || undefined,
    }),
    onSuccess: (is) => {
      qc.invalidateQueries();
      onClose();
      navigate(issueUrl(is.key));
    },
  });
  const close = () => {
    if (!create.isPending && (!dirty || window.confirm("Закрыть без сохранения?"))) onClose();
  };
  useEffect(() => {
    const onUnload = (e: BeforeUnloadEvent) => { if (dirty) e.preventDefault(); };
    window.addEventListener("beforeunload", onUnload);
    return () => window.removeEventListener("beforeunload", onUnload);
  }, [dirty]);

  const ready = !!form.title.trim() && !!form.project;
  return <Dialog open onClose={close} title="Новый тикет" closeOnBackdrop={!dirty} footer={<>
    <Button onClick={close} disabled={create.isPending}>Отмена</Button>
    <Button type="submit" form="new-issue" variant="primary" loading={create.isPending} loadingText="Создание…" disabled={!ready}>Создать</Button>
  </>}>
    <form id="new-issue" className="issue-form" onSubmit={(e) => { e.preventDefault(); if (ready && !create.isPending) create.mutate(); }}>
      {!project && <Select label="Проект" value={form.project} onChange={set("project")} disabled={create.isPending} required>
        {!projects?.items.length && <option value="">Нет проектов трекера</option>}
        {projects?.items.map((p) => <option key={p.key} value={p.key}>{p.key} · {p.title}</option>)}
      </Select>}
      <Input ref={titleInput} label="Название" required value={form.title} onChange={set("title")} disabled={create.isPending} />
      <div className="form-row">
        <Select label="Тип" value={form.issue_type} onChange={set("issue_type")} disabled={create.isPending}>
          {(current?.issue_types ?? []).map((t, i) => <option key={t} value={i === 0 ? "" : t}>{typeLabel(t)}</option>)}
        </Select>
        <Select label="Приоритет" value={form.priority} onChange={set("priority")} disabled={create.isPending}>
          <option value="">Не задан</option>
          {PRIORITIES.map((p) => <option key={p.id} value={p.id}>{p.label}</option>)}
        </Select>
      </div>
      <div className="form-row">
        <Input label="Исполнитель" value={form.assignee} onChange={set("assignee")} hint="Имя пользователя" list="new-issue-people" disabled={create.isPending} />
        <PeopleOptions id="new-issue-people" />
        <Input label="Срок" type="date" value={form.due} onChange={set("due")} disabled={create.isPending} />
      </div>
      <Input label="Метки" value={form.labels} onChange={set("labels")} hint="Через запятую или пробел" disabled={create.isPending} />
      <Textarea label="Описание" className="document-input" value={form.description} onChange={set("description")}
        hint="Markdown: списки, [[ссылки]], - [ ] чек-лист, ```mermaid" disabled={create.isPending} />
      {create.isError && <Alert tone="error" title="Тикет не создан">{errorText(create.error)}</Alert>}
    </form>
  </Dialog>;
}
