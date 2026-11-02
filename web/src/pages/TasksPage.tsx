import { useRef, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Alert, Button, EmptyState, Spinner } from "@villetacore/velessoft-ui";
import WorkspaceOverview from "../components/WorkspaceOverview";
import AsidePanel from "../components/AsidePanel";
import NewNoteDialog from "../components/NewNoteDialog";
import Icon from "../components/Icon";
import { api } from "../api/client";
import type { Task, ViewName } from "../api/types";
import TaskRow from "../components/TaskRow";
import QuickAdd from "../components/QuickAdd";
import { isOverdue, isToday } from "../lib/date";
import { errorText } from "../lib/errors";
import { noteLabel, useNoteIndex } from "../lib/notes";

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

const HINTS: Record<string, string> = {
  inbox: "Быстрые записи, которые ещё не разобраны по проектам.",
  week: "Всё, что запланировано или должно быть сделано до конца недели.",
  overdue: "Задачи, срок которых уже прошёл.",
  upcoming: "Задачи с датой в будущем.",
  someday: "Открытые задачи без срока и плана.",
  done: "Закрытые задачи.",
  all: "Все открытые задачи, сгруппированные по проектам и записям.",
};

/** Views that group by the note holding the task; the others group by date. */
const BY_NOTE = new Set(["all", "done", "inbox", "someday"]);

type Group = { key: string; label: string; path?: string; tasks: Task[] };

/** groupTasks splits a flat list into the sections the view should show. */
function groupTasks(tasks: Task[], view: string, title: (path: string) => string): Group[] {
  if (BY_NOTE.has(view)) {
    const byNote = new Map<string, Task[]>();
    for (const t of tasks) {
      const list = byNote.get(t.note_path) ?? [];
      list.push(t);
      byNote.set(t.note_path, list);
    }
    return [...byNote.entries()]
      .map(([path, list]) => ({ key: path, label: title(path), path, tasks: list }))
      .sort((a, b) => a.label.localeCompare(b.label, "ru"));
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
  const notes = useNoteIndex();
  const { data, isLoading, error } = useQuery({
    queryKey: ["tasks", view],
    queryFn: () => api.tasks({ view: view as ViewName, limit: 500 }),
  });

  const title = (path: string) => noteLabel(notes.byPath.get(path), path);
  const tasks = data?.items ?? [];
  const byNote = BY_NOTE.has(view);
  const groups = groupTasks(tasks, view, title);
  const todayView = view === "today";

  const list = (
    <div className="task-content">
      {todayView && <div className="section-title"><h2>Задачи на сегодня</h2><Link to="/view/all">Все задачи <Icon name="chevron" size={14} /></Link></div>}
      <QuickAdd inputRef={addInput} note={view === "inbox" ? "00-inbox/inbox.md" : undefined} scheduled={todayView || view === "week" ? "today" : undefined} />
      {error && <Alert tone="error" title="Не удалось загрузить задачи">{errorText(error)}</Alert>}
      {isLoading && <Spinner label="Загрузка задач" />}

      {!isLoading && !error && tasks.length === 0 && (
        <EmptyState
          icon={<Icon name="check" size={24} />}
          title={view === "done" ? "Пока ничего не закрыто" : "Здесь пусто"}
          description={view === "done" ? "Выполненные задачи появятся здесь." : "Добавьте задачу в поле выше — начните с самого важного."}
          actions={view !== "done" ? <Button onClick={() => addInput.current?.focus()}>Добавить задачу</Button> : undefined}
        />
      )}

      {tasks.length > 0 && (
        <div className="list">
          {groups.map((g) => (
            <section key={g.key} className="list-group" aria-label={g.label}>
              <div className="group-head">
                {g.path ? (
                  <>
                    <Icon name={notes.byPath.get(g.path)?.type === "project" ? "folder" : "file"} size={16} />
                    <Link to={`/note/${g.path}`}>{g.label}</Link>
                  </>
                ) : g.label}
                <span className="count">{g.tasks.length}</span>
              </div>
              {g.tasks.map((t) => (
                <TaskRow key={t.id} task={t} noteTitle={byNote ? undefined : title(t.note_path)} />
              ))}
            </section>
          ))}
        </div>
      )}
    </div>
  );

  return (
    <div className="page">
      {todayView ? (
        <WorkspaceOverview onAdd={() => addInput.current?.focus()} onDocument={() => setCreating(true)} />
      ) : (
        <div className="page-head">
          <div>
            <span className="kicker">Задачи / {data?.total ?? 0}</span>
            <h1>{TITLES[view] ?? view}</h1>
            {HINTS[view] && <span className="sub">{HINTS[view]}</span>}
          </div>
        </div>
      )}
      {todayView ? (
        <div className="split">
          {list}
          <div className="aside-stack">
            <section className="card quick-links" aria-label="Быстрые действия">
              <div className="nav-title">Быстрые действия</div>
              <button className="nav-item" onClick={() => setCreating(true)}><span className="icon"><Icon name="file" size={16} /></span>Новый документ</button>
              <Link className="nav-item" to="/docs"><span className="icon"><Icon name="book" size={16} /></span>Документация</Link>
              <Link className="nav-item" to="/integrations"><span className="icon"><Icon name="plug" size={16} /></span>Интеграции</Link>
            </section>
            <AsidePanel />
          </div>
        </div>
      ) : list}
      {creating && <NewNoteDialog onClose={() => setCreating(false)} />}
    </div>
  );
}
