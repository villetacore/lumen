import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import { TaskEditor } from "../components/TaskRow";
import TaskDescription from "../components/TaskDescription";
import RelatedContent from "../components/RelatedContent";
import { errorText } from "../lib/errors";

export default function TaskPage() {
  const { id = "" } = useParams();
  const [editing, setEditing] = useState(false);
  const [editorDirty, setEditorDirty] = useState(false);
  const { data: task, error, isLoading } = useQuery({ queryKey: ["task", id], queryFn: () => api.task(id) });
  if (isLoading) return <div className="spinner">Загрузка…</div>;
  if (!task) return <div className="error">Не удалось открыть задачу: {errorText(error)}</div>;
  return <div>
    <div className="page-head"><h1>{task.text}</h1></div>
    <p><Link to={`/note/${task.note_path}`}>← К проекту или записи</Link></p>
    {editing ? <TaskEditor key={task.id} task={task} onDirty={setEditorDirty} onClose={() => { setEditing(false); setEditorDirty(false); }} /> :
      <button className="btn" onClick={() => setEditing(true)}>Изменить параметры задачи</button>}
    <TaskDescription key={`description-${task.id}`} parent={`task:${task.id}`} externalDirty={editorDirty} />
    <RelatedContent parent={`task:${task.id}`} />
  </div>;
}
