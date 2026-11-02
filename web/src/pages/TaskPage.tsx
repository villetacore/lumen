import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, Badge, Button, Spinner } from "@villetacore/velessoft-ui";
import { api } from "../api/client";
import type { TaskStatus } from "../api/types";
import { PRIORITY_TONE, STATUS_LABEL, TaskEditor, dueTone, nextStatus } from "../components/TaskRow";
import TaskDescription from "../components/TaskDescription";
import RelatedContent from "../components/RelatedContent";
import Crumbs from "../components/Crumbs";
import Icon from "../components/Icon";
import { longDate, parseDate, relativeDue } from "../lib/date";
import { errorText } from "../lib/errors";
import { noteLabel, useNoteIndex } from "../lib/notes";

const MARK: Record<TaskStatus, string> = { todo: "", doing: "/", done: "✓", cancelled: "×", deferred: ">" };

function DateValue({ value }: { value: string | null }) {
  if (!value) return <>—</>;
  return <Badge tone={dueTone(value)}>{relativeDue(value)} · {longDate(parseDate(value))}</Badge>;
}

export default function TaskPage() {
  const { id = "" } = useParams();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const notes = useNoteIndex();
  const [editing, setEditing] = useState(false);
  const [editorDirty, setEditorDirty] = useState(false);
  const { data: task, error, isLoading } = useQuery({ queryKey: ["task", id], queryFn: () => api.task(id) });
  const status = useMutation({
    mutationFn: (s: TaskStatus) => api.patchTask(id, { status: s }),
    onSuccess: () => qc.invalidateQueries(),
  });
  if (isLoading) return <Spinner label="Загрузка задачи" />;
  if (!task) return <Alert tone="error" title="Не удалось открыть задачу" actions={<Button onClick={() => navigate("/view/all")}>Ко всем задачам</Button>}>{errorText(error)}</Alert>;

  const note = notes.byPath.get(task.note_path);
  const noteTitle = noteLabel(note, task.note_path);
  const noteUrl = `/note/${task.note_path}`;

  return <div className="page task-card">
    <Crumbs items={[{ label: "Задачи", to: "/view/all" }, { label: noteTitle, to: noteUrl }, { label: task.text }]} />
    <div className="page-head">
      <div className={`task-title is-${task.status}`}>
        <button className={`checkbox ${task.status}`} onClick={() => status.mutate(nextStatus(task.status))} disabled={status.isPending}
          aria-label={`Статус: ${STATUS_LABEL[task.status]}. Изменить`} title="Щелчок меняет статус">{MARK[task.status]}</button>
        <div>
          <span className="kicker">Задача / {STATUS_LABEL[task.status]}</span>
          <h1>{task.text}</h1>
        </div>
      </div>
      {!editing && <div className="head-actions">
        <Button onClick={() => navigate(noteUrl)}><Icon name={note?.type === "project" ? "folder" : "file"} size={16} />{note?.type === "project" ? "К проекту" : "К записи"}</Button>
        <Button variant="primary" onClick={() => setEditing(true)}><Icon name="edit" size={16} />Изменить</Button>
      </div>}
    </div>
    {status.isError && <Alert tone="error" title="Статус не изменён">{errorText(status.error)}</Alert>}

    {editing ? (
      <TaskEditor key={task.id} task={task} onDirty={setEditorDirty}
        onClose={() => { setEditing(false); setEditorDirty(false); }}
        onDeleted={() => navigate(noteUrl, { replace: true })} />
    ) : (
      <dl className="props">
        <div><dt>Статус</dt><dd>{STATUS_LABEL[task.status]}</dd></div>
        <div><dt>Срок</dt><dd><DateValue value={task.due} /></dd></div>
        <div><dt>Запланировано</dt><dd><DateValue value={task.scheduled} /></dd></div>
        <div><dt>Приоритет</dt><dd>{task.priority ? <Badge tone={PRIORITY_TONE[task.priority]}>!{task.priority}</Badge> : "—"}</dd></div>
        <div><dt>Оценка</dt><dd>{task.estimate || "—"}</dd></div>
        <div><dt>Проект</dt><dd>{task.project || "—"}</dd></div>
        <div><dt>Где записана</dt><dd><Link to={noteUrl}>{noteTitle}</Link></dd></div>
        {!!task.tags?.length && <div><dt>Теги</dt><dd>{task.tags.map((t) => <Link key={t} className="md-tag" to={`/notes?tag=${encodeURIComponent(t)}`} style={{ marginRight: 6 }}>#{t}</Link>)}</dd></div>}
        {task.done && <div><dt>Закрыта</dt><dd>{longDate(parseDate(task.done))}</dd></div>}
      </dl>
    )}

    <TaskDescription key={`description-${task.id}`} parent={`task:${task.id}`} externalDirty={editorDirty} />
    <RelatedContent parent={`task:${task.id}`} />
  </div>;
}
