import { NavLink } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import type { ViewName } from "../api/types";

const VIEWS: { view: ViewName; label: string; icon: string }[] = [
  { view: "today", label: "Сегодня", icon: "◎" },
  { view: "inbox", label: "Входящие", icon: "▤" },
  { view: "week", label: "Неделя", icon: "▦" },
  { view: "overdue", label: "Просрочено", icon: "!" },
  { view: "upcoming", label: "Предстоит", icon: "→" },
  { view: "someday", label: "Без даты", icon: "∅" },
  { view: "all", label: "Все задачи", icon: "≡" },
];

function ViewLink({ view, label, icon }: { view: ViewName; label: string; icon: string }) {
  const { data } = useQuery({
    queryKey: ["tasks", "count", view],
    queryFn: () => api.tasks({ view, limit: 1 }),
  });
  return (
    <NavLink to={`/view/${view}`} className={({ isActive }) => "nav-item" + (isActive ? " active" : "")}>
      <span className="icon">{icon}</span>
      {label}
      {data && data.total > 0 && <span className="count">{data.total}</span>}
    </NavLink>
  );
}

export default function Sidebar() {
  const { data: projects } = useQuery({
    queryKey: ["notes", { type: "project" }],
    queryFn: () => api.notes({ type: "project" }),
  });
  const { data: tags } = useQuery({ queryKey: ["tags"], queryFn: api.tags });

  return (
    <nav className="sidebar">
      <div className="nav-group">
        {VIEWS.map((v) => (
          <ViewLink key={v.view} {...v} />
        ))}
      </div>

      <div className="nav-group">
        <div className="nav-title">Проекты</div>
        {projects?.items.map((p) => (
          <NavLink
            key={p.path}
            to={`/note/${p.path}`}
            className={({ isActive }) => "nav-item" + (isActive ? " active" : "")}
          >
            <span className="icon">•</span>
            {p.title}
            {p.open_tasks > 0 && <span className="count">{p.open_tasks}</span>}
          </NavLink>
        ))}
        {projects?.items.length === 0 && (
          <div className="nav-item" style={{ color: "var(--text-faint)" }}>
            нет проектов
          </div>
        )}
      </div>

      <div className="nav-group">
        <div className="nav-title">Обзор</div>
        <NavLink to="/calendar" className={({ isActive }) => "nav-item" + (isActive ? " active" : "")}>
          <span className="icon">▩</span>
          Календарь
        </NavLink>
        <NavLink to="/notes" className={({ isActive }) => "nav-item" + (isActive ? " active" : "")}>
          <span className="icon">▭</span>
          Все заметки
        </NavLink>
        <NavLink to="/integrations" className={({ isActive }) => "nav-item" + (isActive ? " active" : "")}>
          <span className="icon">⇄</span>
          Интеграции
        </NavLink>
        <NavLink to="/passwords" className={({ isActive }) => "nav-item" + (isActive ? " active" : "")}>
          <span className="icon">🔑</span>
          Пароли
        </NavLink>
      </div>

      <div className="nav-group">
        <div className="nav-title">Теги</div>
        {tags?.items.slice(0, 12).map((t) => (
          <NavLink
            key={t.tag}
            to={`/notes?tag=${encodeURIComponent(t.tag)}`}
            className="nav-item"
          >
            <span className="icon">#</span>
            {t.tag}
            <span className="count">{t.count}</span>
          </NavLink>
        ))}
      </div>
    </nav>
  );
}
