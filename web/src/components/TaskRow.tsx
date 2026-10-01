import { Link } from "react-router-dom";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api/client";
import type { Task, TaskStatus } from "../api/types";
import { isOverdue, isToday, relativeDue } from "../lib/date";

const MARK: Record<TaskStatus, string> = {
  todo: "",
  doing: "/",
  done: "✓",
  cancelled: "×",
  deferred: ">",
};

/** nextStatus cycles the checkbox: todo → doing → done → todo. */
function nextStatus(s: TaskStatus): TaskStatus {
  if (s === "todo") return "doing";
  if (s === "doing") return "done";
  return "todo";
}

export default function TaskRow({ task }: { task: Task }) {
  const qc = useQueryClient();

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
        title={task.status}
        onClick={() => patch.mutate(nextStatus(task.status))}
        disabled={patch.isPending}
        aria-label="Изменить статус"
      >
        {MARK[task.status]}
      </button>

      <div className="task-body">
        <div className="task-text">{task.text}</div>
        <div className="task-meta">
          {task.priority && <span className={`chip prio-${task.priority}`}>{task.priority}</span>}
          {due && <span className={`chip ${dueClass}`}>{relativeDue(due)}</span>}
          {task.estimate && <span className="chip">{task.estimate}</span>}
          {task.project && <span className="chip">◆ {task.project}</span>}
          <Link className="chip" to={`/note/${task.note_path}`} title={task.note_path}>
            {task.note_path.split("/").pop()}
          </Link>
        </div>
      </div>
    </div>
  );
}
