import { createPortal } from "react-dom";
import Brand, { Signature } from "./Brand";
import ThemeSwitch from "./ThemeSwitch";
import Icon, { type IconName } from "./Icon";
import { useState } from "react";
import { NavLink, useNavigate, useSearchParams } from "react-router-dom";
import { useMutation, useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import type { ViewName } from "../api/types";
import { iso, today } from "../lib/date";
import NewNoteDialog from "./NewNoteDialog";
import NewIssueDialog from "./NewIssueDialog";
import { projectUrl } from "../lib/tracker";
import { IconButton } from "@villetacore/velessoft-ui";

const VIEWS: { view: ViewName; label: string; icon: IconName }[] = [
  { view: "today", label: "Сегодня", icon: "sun" },
  { view: "inbox", label: "Входящие", icon: "inbox" },
  { view: "week", label: "Неделя", icon: "calendar" },
  { view: "overdue", label: "Просрочено", icon: "alert" },
  { view: "upcoming", label: "Предстоит", icon: "arrow" },
  { view: "someday", label: "Без даты", icon: "clock" },
  { view: "all", label: "Все задачи", icon: "list" },
  { view: "done", label: "Выполнено", icon: "check" },
];

function ViewLink({ view, label, icon }: { view: ViewName; label: string; icon: IconName }) {
  const { data } = useQuery({
    queryKey: ["tasks", "count", view],
    queryFn: () => api.tasks({ view, limit: 1 }),
  });
  return (
    <NavLink to={`/view/${view}`} className={({ isActive }) => "nav-item" + (isActive ? " active" : "")}>
      <span className="icon"><Icon name={icon} /></span>
      {label}
      {data && data.total > 0 && <span className="count">{data.total}</span>}
    </NavLink>
  );
}

export default function Sidebar({ open, onClose }: { open: boolean; onClose: () => void }) {
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const [creating, setCreating] = useState<"note" | "project" | null>(null);
  // The daily note is created on first open, the way quick capture does it.
  const daily = useMutation({
    mutationFn: () => api.daily(iso(today()), true),
    onSuccess: (n) => navigate(`/note/${n.path}`),
  });
  const { data: projects } = useQuery({
    queryKey: ["notes", { type: "project" }],
    queryFn: () => api.notes({ type: "project" }),
  });
  const { data: tags } = useQuery({ queryKey: ["tags"], queryFn: api.tags });
  const { data: tracker } = useQuery({ queryKey: ["tracker", "projects"], queryFn: api.trackerProjects });
  const [newIssue, setNewIssue] = useState(false);
  const { data: me } = useQuery({ queryKey: ["me"], queryFn: api.me });
  const { data: inbox } = useQuery({ queryKey: ["notifications"], queryFn: () => api.notifications(), enabled: !!me?.username });

  return (
    <nav id="workspace-navigation" className={`sidebar${open ? " open" : ""}`} aria-label="Основная навигация">
      <div className="sidebar-brand"><Brand /><IconButton className="mobile-only" variant="ghost" onClick={onClose} label="Закрыть меню"><Icon name="close" size={16} /></IconButton></div>
      <div className="sidebar-scroll">
      <div className="nav-group">
      <div className="nav-title">Задачи</div>
        {VIEWS.map((v) => (
          <ViewLink key={v.view} {...v} />
        ))}
      </div>

      <div className="nav-group">
        <div className="nav-title">
          Трекер
          <IconButton className="nav-add" size="sm" variant="ghost" label="Новый тикет" onClick={() => { setNewIssue(true); if (open) onClose(); }}>
            <Icon name="plus" size={14} />
          </IconButton>
        </div>
        <NavLink to="/my" className={({ isActive }) => "nav-item" + (isActive ? " active" : "")}>
          <span className="icon"><Icon name="user" /></span>
          Мои задачи
        </NavLink>
        <NavLink to="/inbox" className={({ isActive }) => "nav-item" + (isActive ? " active" : "")}>
          <span className="icon"><Icon name="bell" /></span>
          Уведомления
          {!!inbox?.unread && <span className="count">{inbox.unread}</span>}
        </NavLink>
        <NavLink end to="/tracker" className={({ isActive }) => "nav-item" + (isActive ? " active" : "")}>
          <span className="icon"><Icon name="board" /></span>
          Все проекты
        </NavLink>
        {tracker?.items.map((p) => (
          <NavLink key={p.key} to={projectUrl(p.key)} className={({ isActive }) => "nav-item" + (isActive ? " active" : "")}>
            <span className="icon nav-key">{p.key}</span>
            {p.title}
          </NavLink>
        ))}
      </div>

      <div className="nav-group">
        <div className="nav-title">
          Проекты
          <IconButton className="nav-add" size="sm" variant="ghost" label="Новый проект" onClick={() => { setCreating("project"); if (open) onClose(); }}>
            <Icon name="plus" size={14} />
          </IconButton>
        </div>
        {projects?.items.map((p) => (
          <NavLink
            key={p.path}
            to={`/note/${p.path}`}
            className={({ isActive }) => "nav-item" + (isActive ? " active" : "")}
          >
            <span className="icon"><Icon name="folder" /></span>
            {p.title}
            {p.open_tasks > 0 && <span className="count">{p.open_tasks}</span>}
          </NavLink>
        ))}
        {projects?.items.length === 0 && (
          <div className="nav-item placeholder">нет проектов</div>
        )}
      </div>

      <div className="nav-group">
        <div className="nav-title">Обзор</div>
        <button className="nav-item" onClick={() => daily.mutate()} disabled={daily.isPending}>
          <span className="icon"><Icon name="sun" /></span>
          Мой день
        </button>
        <button className="nav-item" onClick={() => { setCreating("note"); if (open) onClose(); }}>
          <span className="icon"><Icon name="plus" /></span>
          Новый документ
        </button>
        <NavLink to="/calendar" className={({ isActive }) => "nav-item" + (isActive ? " active" : "")}>
          <span className="icon"><Icon name="calendar" /></span>
          Календарь
        </NavLink>
        <NavLink to="/docs" className={({ isActive }) => "nav-item" + (isActive ? " active" : "")}><span className="icon"><Icon name="book" /></span>Документация</NavLink>
        <NavLink to="/notes" className={({ isActive }) => "nav-item" + (isActive && !params.get("section") && !params.get("tag") ? " active" : "")}>
          <span className="icon"><Icon name="file" /></span>
          Материалы
        </NavLink>
        <NavLink to="/people" className={({ isActive }) => "nav-item" + (isActive ? " active" : "")}>
          <span className="icon"><Icon name="users" /></span>
          Команда
        </NavLink>
        <NavLink to="/integrations" className={({ isActive }) => "nav-item" + (isActive ? " active" : "")}>
          <span className="icon"><Icon name="plug" /></span>
          Интеграции
        </NavLink>
        <NavLink to="/passwords" className={({ isActive }) => "nav-item" + (isActive ? " active" : "")}>
          <span className="icon"><Icon name="key" /></span>
          Пароли
        </NavLink>
      </div>

      <div className="nav-group">
        <div className="nav-title">Теги</div>
        {tags?.items.slice(0, 12).map((t) => (
          <NavLink
            key={t.tag}
            to={`/notes?tag=${encodeURIComponent(t.tag)}`}
            // NavLink compares paths only; the tag lives in the query string.
            className={({ isActive }) =>
              "nav-item" + (isActive && params.get("tag") === t.tag ? " active" : "")
            }
          >
            <span className="icon"><Icon name="hash" /></span>
            {t.tag}
            <span className="count">{t.count}</span>
          </NavLink>
        ))}
      </div>
      </div>
      <div className="sidebar-foot"><ThemeSwitch /><Signature /></div>
      {creating && createPortal(<NewNoteDialog type={creating} onClose={() => setCreating(null)} />, document.body)}
      {newIssue && createPortal(<NewIssueDialog onClose={() => setNewIssue(false)} />, document.body)}
    </nav>
  );
}
