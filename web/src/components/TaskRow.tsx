import { useEffect, useState } from "react";
import { Link, useNavigate } from "../lib/navigation";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api/client";
import type { Task, TaskPatch, TaskStatus } from "../api/types";
import { isOverdue, isToday, relativeDue } from "../lib/date";
import { errorText } from "../lib/errors";
import { Alert, Badge, Button, Input, Select } from "@villetacore/velessoft-ui";
import type { Tone } from "@villetacore/velessoft-ui";

const MARK: Record<TaskStatus, string> = {
  todo: "",
  doing: "/",
  done: "✓",
  cancelled: "×",
  deferred: ">",
};

export const STATUS_LABEL: Record<TaskStatus, string> = {
  todo: "к выполнению",
  doing: "в работе",
  done: "выполнено",
  cancelled: "отменено",
  deferred: "отложено",
};

/** nextStatus cycles the checkbox: todo → doing → done → todo. */
export function nextStatus(s: TaskStatus): TaskStatus {
  if (s === "todo") return "doing";
  if (s === "doing") return "done";
  return "todo";
}

export const PRIORITY_TONE: Record<string, Tone> = { A: "error", B: "warning", C: "info", D: "neutral" };

/** dueTone colours a date badge: overdue is an error, today is the accent. */
export function dueTone(date: string): Tone {
  return isOverdue(date) ? "error" : isToday(date) ? "info" : "neutral";
}

/** TaskRow is one line of a task list. noteTitle links back to the note that holds the task. */
export default function TaskRow({ task, noteTitle }: { task: Task; noteTitle?: string }) {
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

  return (
    <div className={`task-row is-${task.status}`}>
      <button
        className={`checkbox ${task.status}`}
        title={`${STATUS_LABEL[task.status]} — щелчок меняет статус`}
        onClick={() => patch.mutate(nextStatus(task.status))}
        disabled={patch.isPending}
        aria-label={`Статус: ${STATUS_LABEL[task.status]}. Изменить`}
      >
        {MARK[task.status]}
      </button>

      <div className="task-body">
        <button className="task-text" onClick={() => open.mutate()} disabled={open.isPending} title="Открыть задачу">
          {task.text}
        </button>
        <div className="task-meta">
          {task.status === "doing" && <Badge tone="warning" dot>в работе</Badge>}
          {task.priority && <Badge tone={PRIORITY_TONE[task.priority]}>!{task.priority}</Badge>}
          {due && <Badge tone={dueTone(due)}>{relativeDue(due)}</Badge>}
          {task.estimate && <Badge>{task.estimate}</Badge>}
          {task.project && <Badge tone="info">{task.project}</Badge>}
          {noteTitle && <Link className="vs-badge vs-neutral" href={`/note/${task.note_path}`} title="Открыть запись с задачей">{noteTitle}</Link>}
          {open.isError && <span className="hint" role="alert">{errorText(open.error)}</span>}
          {patch.isError && <span className="hint" role="alert">{errorText(patch.error)}</span>}
        </div>
      </div>
    </div>
  );
}

/** TaskEditor edits one task in place; only the fields that changed are sent. */
export function TaskEditor({ task, onClose, onDirty, onDeleted }: { task: Task; onClose: () => void; onDirty?: (dirty: boolean) => void; onDeleted?: () => void }) {
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
    onSuccess: () => {
      onDirty?.(false);
      onDeleted?.();
      qc.invalidateQueries();
    },
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
        <Input label="Текст задачи" value={form.text} onChange={set("text")} autoFocus required />
        <div className="task-editor-fields">
          <Select label="Статус" value={form.status} onChange={set("status")}>
            {(Object.keys(STATUS_LABEL) as TaskStatus[]).map((s) => (
              <option key={s} value={s}>{STATUS_LABEL[s]}</option>
            ))}
          </Select>
          <Input label="Срок" type="date" value={form.due} onChange={set("due")} />
          <Input label="Запланировано" type="date" value={form.scheduled} onChange={set("scheduled")} />
          <Select label="Приоритет" value={form.priority} onChange={set("priority")}>
            <option value="">—</option>
            <option value="A">A — высокий</option>
            <option value="B">B</option>
            <option value="C">C</option>
            <option value="D">D — низкий</option>
          </Select>
          <Input label="Оценка" value={form.estimate} onChange={set("estimate")} placeholder="2h" />
          <Input label="Проект" value={form.project} onChange={set("project")} placeholder="—" />
        </div>
        <div className="task-editor-actions">
          <Button variant="primary" type="submit" loading={save.isPending} disabled={!form.text.trim()}>Сохранить</Button>
          <Button onClick={close}>Отмена</Button>
          <Button
            className="push"
            variant="danger"
            loading={remove.isPending}
            onClick={() => {
              if (window.confirm(`Удалить задачу «${task.text}»?`)) remove.mutate();
            }}
          >
            Удалить
          </Button>
        </div>
      </fieldset>
      {(save.isError || remove.isError) && (
        <Alert tone="error" title="Не удалось сохранить">{errorText(save.error ?? remove.error)}</Alert>
      )}
    </form>
  );
}

/** Date inputs work with YYYY-MM-DD; a time part, if any, is kept off the form. */
function dateOnly(v: string | null): string {
  return v ? v.slice(0, 10) : "";
}
