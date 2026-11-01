import { useRef, useState } from "react";
import { Link } from "react-router-dom";
import WorkspaceOverview from "../components/WorkspaceOverview";
import AsidePanel from "../components/AsidePanel";
import NewNoteDialog from "../components/NewNoteDialog";
import Icon from "../components/Icon";
import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import type { Task, ViewName } from "../api/types";
import TaskRow from "../components/TaskRow";
import QuickAdd from "../components/QuickAdd";
import { isOverdue, isToday, longDate, today } from "../lib/date";
import { errorText } from "../lib/errors";

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
    // Overdue is about the deadline; "today" is about the plan, so a task
    // scheduled for today with a later deadline still belongs to today.
    const plan = t.scheduled ?? t.due;
    if (!plan) groups[3].tasks.push(t);
    else if (t.due ? isOverdue(t.due) : isOverdue(plan)) groups[0].tasks.push(t);
    else if (isToday(plan) || isOverdue(plan)) groups[1].tasks.push(t);
    else groups[2].tasks.push(t);
  }
  return groups.filter((g) => g.tasks.length > 0);
}

export default function TasksPage() {
  const { view = "today" } = useParams();
  const addInput = useRef<HTMLInputElement>(null);
  const [creating, setCreating] = useState(false);
  const { data, isLoading, error } = useQuery({
    queryKey: ["tasks", view],
    queryFn: () => api.tasks({ view: view as ViewName, limit: 500 }),
  });

  const tasks = data?.items ?? [];
  const groups = groupTasks(tasks, view);

  return (
    <div className="page">
      {view === "today" ? <WorkspaceOverview onAdd={() => addInput.current?.focus()} onDocument={() => setCreating(true)} /> : <div className="page-head">
        <h1>{TITLES[view] ?? view}</h1>
        <span className="sub">
          {view === "today" ? longDate(today()) : `${data?.total ?? 0} задач`}
        </span>
      </div>}
      <div className={view === "today" ? "dashboard-split" : "task-content"}>
      <div className="task-content">
      {view === "today" && <div className="section-title"><h2>Задачи на сегодня</h2><Link to="/view/all">Все задачи <Icon name="chevron" size={16} /></Link></div>}
      <QuickAdd inputRef={addInput} note={view === "inbox" ? "00-inbox/inbox.md" : undefined} scheduled={view === "today" || view === "week" ? "today" : undefined} />
      {error && <div className="error">Не удалось загрузить задачи: {errorText(error)}</div>}
      {isLoading && <div className="spinner">Загрузка…</div>}

      {!isLoading && !error && tasks.length === 0 && (
        <div className="empty"><span className="empty-icon"><Icon name="check" size={28} /></span><strong>Место для новых планов</strong><span>Добавьте задачу и начните с самого важного.</span></div>
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

      </div>
      {view === "today" && <div className="dashboard-aside">
        <section><h2>Быстрые действия</h2><div className="card quick-links">
          <button onClick={() => setCreating(true)}><span className="quick-icon"><Icon name="file" /></span><span><strong>Новый документ</strong><small>Сохранить идею или решение</small></span><Icon name="chevron" /></button>
          <Link to="/notes?section=documents"><span className="quick-icon"><Icon name="book" /></span><span><strong>Документация</strong><small>Знания и материалы команды</small></span><Icon name="chevron" /></Link>
          <Link to="/integrations"><span className="quick-icon"><Icon name="plug" /></span><span><strong>Интеграции</strong><small>Подключённые сервисы</small></span><Icon name="chevron" /></Link>
        </div></section>
        <AsidePanel />
      </div>}
      </div>
      {creating && <NewNoteDialog onClose={() => setCreating(false)} />}
    </div>
  );
}
