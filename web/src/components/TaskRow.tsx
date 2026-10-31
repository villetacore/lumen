import { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api/client";
import type { Task, TaskPatch, TaskStatus } from "../api/types";
import { isOverdue, isToday, relativeDue } from "../lib/date";
import { errorText } from "../lib/errors";

const MARK: Record<TaskStatus, string> = {
  todo: "",
  doing: "/",
  done: "✓",
  cancelled: "×",
  deferred: ">",
};

const STATUS_LABEL: Record<TaskStatus, string> = {
  todo: "к выполнению",
  doing: "в работе",
  done: "выполнено",
  cancelled: "отменено",
  deferred: "отложено",
};

/** nextStatus cycles the checkbox: todo → doing → done → todo. */
function nextStatus(s: TaskStatus): TaskStatus {
  if (s === "todo") return "doing";
  if (s === "doing") return "done";
  return "todo";
}

export default function TaskRow({ task, showNote = true }: { task: Task; showNote?: boolean }) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const open = useMutation({
    mutationFn: () => task.block_id ? Promise.resolve(task) : api.patchTask(task.id, {}),
    onSuccess: (saved) => { qc.invalidateQueries(); navigate(`/task/${encodeURIComponent(saved.id)}`); },
  });

  const patch = useMutation({
    mutationFn: (status: TaskStatus) => api.patchTask(task.id, { status }),
    onSuccess: () => qc.invalidateQueries(),
  });

  const due = task.due ?? task.scheduled;
  const dueClass = due ? (isOverdue(due) ? "due-overdue" : isToday(due) ? "due-today" : "") : "";


  return (
    <div className={`task-row is-${task.status}`}>
      <button
        className={`checkbox ${task.status}`}
        title={`${STATUS_LABEL[task.status]} — щелчок меняет статус`}
        onClick={() => patch.mutate(nextStatus(task.status))}
        disabled={patch.isPending}
        aria-label="Изменить статус"
      >
        {MARK[task.status]}
      </button>

      <div className="task-body">
        <button className="task-text" onClick={() => open.mutate()} disabled={open.isPending} title="Открыть задачу">
          {task.text}
        </button>
        <div className="task-meta">
          {task.priority && <span className={`chip prio-${task.priority}`}>{task.priority}</span>}
          {due && <span className={`chip ${dueClass}`}>{relativeDue(due)}</span>}
          {task.estimate && <span className="chip">{task.estimate}</span>}
          {task.project && <span className="chip">◆ {task.project}</span>}
          {showNote && <Link className="chip" to={`/note/${task.note_path}`}>К проекту или записи</Link>}
          {open.isError && <span className="error">{errorText(open.error)}</span>}
          {patch.isError && <span style={{ color: "var(--overdue)" }}>{errorText(patch.error)}</span>}
        </div>
      </div>
    </div>
  );
}

/** TaskEditor edits one task in place; only the fields that changed are sent. */
export function TaskEditor({ task, onClose, onDirty }: { task: Task; onClose: () => void; onDirty?: (dirty: boolean) => void }) {
  const qc = useQueryClient();
  const [form, setForm] = useState({
    text: task.text,
    status: task.status,
    due: dateOnly(task.due),
    scheduled: dateOnly(task.scheduled),
    priority: task.priority ?? "",
    estimate: task.estimate ?? "",
    project: task.project ?? "",
  });

  const dirty = form.text !== task.text || form.status !== task.status || form.due !== dateOnly(task.due) ||
    form.scheduled !== dateOnly(task.scheduled) || form.priority !== (task.priority ?? "") ||
    form.estimate !== (task.estimate ?? "") || form.project !== (task.project ?? "");
  useEffect(() => { onDirty?.(dirty); }, [dirty, onDirty]);
  const close = () => { if (!dirty || window.confirm("Отменить изменения задачи?")) onClose(); };

  const set = (k: keyof typeof form) => (e: { target: { value: string } }) =>
    setForm((f) => ({ ...f, [k]: e.target.value }));

  const save = useMutation({
    mutationFn: () => {
      const p: TaskPatch = {};
      if (form.text.trim() !== task.text) p.text = form.text.trim();
      if (form.status !== task.status) p.status = form.status;
      if (form.due !== dateOnly(task.due)) p.due = form.due;
      if (form.scheduled !== dateOnly(task.scheduled)) p.scheduled = form.scheduled;
      if (form.priority !== (task.priority ?? "")) p.priority = form.priority;
      if (form.estimate !== (task.estimate ?? "")) p.estimate = form.estimate;
      if (form.project !== (task.project ?? "")) p.project = form.project;
      if (Object.keys(p).length === 0) return Promise.resolve(task);
      return api.patchTask(task.id, p);
    },
    onSuccess: () => {
      qc.invalidateQueries();
      onClose();
    },
  });

  const remove = useMutation({
    mutationFn: () => api.deleteTask(task.id),
    onSuccess: () => qc.invalidateQueries(),
  });

  return (
    <form
      className="task-editor"
      onSubmit={(e) => {
        e.preventDefault();
        if (form.text.trim()) save.mutate();
      }}
      onKeyDown={(e) => {
        if (e.key === "Escape" && !save.isPending) close();
      }}
    >
      <fieldset className="task-fields" disabled={save.isPending || remove.isPending}>
      <input
        className="input task-editor-text"
        value={form.text}
        onChange={set("text")}
        autoFocus
        aria-label="Текст задачи"
      />
      <div className="task-editor-fields">
        <label>
          Статус
          <select className="input" value={form.status} onChange={set("status")}>
            {(Object.keys(STATUS_LABEL) as TaskStatus[]).map((s) => (
              <option key={s} value={s}>
                {STATUS_LABEL[s]}
              </option>
            ))}
          </select>
        </label>
        <label>
          Срок
          <input className="input" type="date" value={form.due} onChange={set("due")} />
        </label>
        <label>
          Запланировано
          <input className="input" type="date" value={form.scheduled} onChange={set("scheduled")} />
        </label>
        <label>
          Приоритет
          <select className="input" value={form.priority} onChange={set("priority")}>
            <option value="">—</option>
            <option value="A">A — высокий</option>
            <option value="B">B</option>
            <option value="C">C</option>
            <option value="D">D — низкий</option>
          </select>
        </label>
        <label>
          Оценка
          <input className="input" value={form.estimate} onChange={set("estimate")} placeholder="2h" size={6} />
        </label>
        <label>
          Проект
          <input className="input" value={form.project} onChange={set("project")} placeholder="—" />
        </label>
      </div>
      <div className="task-editor-actions">
        <button className="btn primary" type="submit" disabled={save.isPending || !form.text.trim()}>
          Сохранить
        </button>
        <button className="btn" type="button" onClick={close}>
          Отмена
        </button>
        <button
          className="btn danger"
          type="button"
          style={{ marginLeft: "auto" }}
          disabled={remove.isPending}
          onClick={() => {
            if (window.confirm(`Удалить задачу «${task.text}»?`)) remove.mutate();
          }}
        >
          Удалить
        </button>
      </div>
      </fieldset>
      {(save.isError || remove.isError) && (
        <div className="error" style={{ marginTop: 8 }}>
          {errorText(save.error ?? remove.error)}
        </div>
      )}
    </form>
  );
}

/** Date inputs work with YYYY-MM-DD; a time part, if any, is kept off the form. */
function dateOnly(v: string | null): string {
  return v ? v.slice(0, 10) : "";
}
