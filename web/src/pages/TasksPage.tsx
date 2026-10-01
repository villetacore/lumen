import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import type { Task, ViewName } from "../api/types";
import TaskRow from "../components/TaskRow";
import QuickAdd from "../components/QuickAdd";
import { isOverdue, isToday, longDate, today } from "../lib/date";

const TITLES: Record<string, string> = {
  today: "Сегодня",
  inbox: "Входящие",
  week: "Неделя",
  overdue: "Просрочено",
  upcoming: "Предстоит",
  someday: "Без даты",
  done: "Выполнено",
  all: "Все задачи",
};

type Group = { key: string; label: string; tasks: Task[] };

/** groupTasks splits a flat list into the sections the view should show. */
function groupTasks(tasks: Task[], view: string): Group[] {
  if (view === "all" || view === "done" || view === "inbox") {
    const byNote = new Map<string, Task[]>();
    for (const t of tasks) {
      const list = byNote.get(t.note_path) ?? [];
      list.push(t);
      byNote.set(t.note_path, list);
    }
    return [...byNote.entries()].map(([path, list]) => ({
      key: path,
      label: path,
      tasks: list,
    }));
  }

  const groups: Group[] = [
    { key: "overdue", label: "Просрочено", tasks: [] },
    { key: "today", label: "Сегодня", tasks: [] },
    { key: "later", label: "Дальше", tasks: [] },
    { key: "none", label: "Без даты", tasks: [] },
  ];
  for (const t of tasks) {
    const d = t.due ?? t.scheduled;
    if (!d) groups[3].tasks.push(t);
    else if (isOverdue(d)) groups[0].tasks.push(t);
    else if (isToday(d)) groups[1].tasks.push(t);
    else groups[2].tasks.push(t);
  }
  return groups.filter((g) => g.tasks.length > 0);
}

export default function TasksPage() {
  const { view = "today" } = useParams();
  const { data, isLoading, error } = useQuery({
    queryKey: ["tasks", view],
    queryFn: () => api.tasks({ view: view as ViewName, limit: 500 }),
  });

  const tasks = data?.items ?? [];
  const groups = groupTasks(tasks, view);

  return (
    <>
      <div className="page-head">
        <h1>{TITLES[view] ?? view}</h1>
        <span className="sub">
          {view === "today" ? longDate(today()) : `${data?.total ?? 0} задач`}
        </span>
      </div>

      {error && <div className="error">Не удалось загрузить задачи: {String(error)}</div>}
      {isLoading && <div className="spinner">Загрузка…</div>}

      {!isLoading && tasks.length === 0 && (
        <div className="empty">Здесь пусто. Добавьте задачу ниже.</div>
      )}

      {groups.map((g) => (
        <section key={g.key}>
          <div className="section-head">
            {g.label} <span style={{ opacity: 0.7 }}>{g.tasks.length}</span>
          </div>
          {g.tasks.map((t) => (
            <TaskRow key={t.id} task={t} />
          ))}
        </section>
      ))}

      <QuickAdd />
    </>
  );
}
