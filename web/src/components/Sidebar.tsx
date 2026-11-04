import { createPortal } from "react-dom";
import Brand, { Signature } from "./Brand";
import ThemeSwitch from "./ThemeSwitch";
import Icon, { type IconName } from "./Icon";
import { useState } from "react";
import { NavLink, useLocation, useNavigate, useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import NewNoteDialog from "./NewNoteDialog";
import NewIssueDialog from "./NewIssueDialog";
import NewProjectDialog from "./NewProjectDialog";
import { projectUrl } from "../lib/tracker";
import { IconButton, Menu } from "@villetacore/velessoft-ui";

const navClass = (active: boolean) => "nav-item" + (active ? " active" : "");

function Item({ to, icon, label, count, end, active }: { to: string; icon: IconName; label: string; count?: number; end?: boolean; active?: boolean }) {
  return <NavLink to={to} end={end} className={({ isActive }) => navClass(active ?? isActive)}>
    <span className="icon"><Icon name={icon} /></span>
    {label}
    {!!count && <span className="count">{count}</span>}
  </NavLink>;
}

/**
 * Sidebar answers three questions: what do I do now, in which project, and
 * where is the knowledge. Everything else is behind "Настройка" or on the page
 * it belongs to: task views are tabs of "Задачи", tags are filters of "Заметки".
 */
export default function Sidebar({ open, onClose }: { open: boolean; onClose: () => void }) {
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const [params] = useSearchParams();
  const [creating, setCreating] = useState<"note" | "issue" | "project" | null>(null);
  const { data: me } = useQuery({ queryKey: ["me"], queryFn: api.me });
  const { data: plain } = useQuery({ queryKey: ["notes", { type: "project" }], queryFn: () => api.notes({ type: "project" }) });
  const { data: tracker } = useQuery({ queryKey: ["tracker", "projects"], queryFn: api.trackerProjects });
  const { data: proposals } = useQuery({ queryKey: ["proposals", "pending"], queryFn: () => api.proposals(true) });
  const { data: today } = useQuery({ queryKey: ["tasks", "count", "today"], queryFn: () => api.tasks({ view: "today", limit: 1 }) });

  const admin = !me?.role || me.role === "admin";
  const hasBoard = new Set(tracker?.items.map((p) => p.path));
  const notePlain = plain?.items.filter((n) => !hasBoard.has(n.path)) ?? [];
  const create = (what: "note" | "issue" | "project") => { setCreating(what); if (open) onClose(); };

  return (
    <nav id="workspace-navigation" className={`sidebar${open ? " open" : ""}`} aria-label="Основная навигация">
      <div className="sidebar-brand"><Brand /><IconButton className="mobile-only" variant="ghost" onClick={onClose} label="Закрыть меню"><Icon name="close" size={16} /></IconButton></div>
      <div className="sidebar-create">
        <Menu variant="primary" className="create-menu" label={<><Icon name="plus" size={16} />Создать</>} items={[
          { label: "Тикет в проекте", icon: <Icon name="board" size={16} />, disabled: !tracker?.items.length, onSelect: () => create("issue") },
          { label: "Задача", icon: <Icon name="check" size={16} />, onSelect: () => navigate("/view/inbox#add") },
          { label: "Документ", icon: <Icon name="file" size={16} />, onSelect: () => create("note") },
          { type: "separator" },
          { label: "Проект", icon: <Icon name="folder" size={16} />, onSelect: () => create("project") },
        ]} />
      </div>
      <div className="sidebar-scroll">
        <div className="nav-group">
          <Item to="/view/today" icon="sun" label="Сегодня" count={today?.total} />
          {me?.username && <Item to="/my" icon="user" label="Мои тикеты" />}
          <Item to="/view/all" icon="list" label="Задачи" active={pathname.startsWith("/view/") && pathname !== "/view/today"} />
          {!!proposals?.total && <Item to="/approvals" icon="check" label="Подтвердить" count={proposals.total} />}
        </div>

        <div className="nav-group">
          <div className="nav-title">
            Проекты
            <IconButton className="nav-add" size="sm" variant="ghost" label="Новый проект" onClick={() => create("project")}>
              <Icon name="plus" size={14} />
            </IconButton>
          </div>
          {tracker?.items.map((p) => (
            <NavLink key={p.key} to={`${projectUrl(p.key)}/board`} className={navClass(pathname.startsWith(projectUrl(p.key)))}>
              <span className="icon nav-key">{p.key}</span>
              {p.title}
            </NavLink>
          ))}
          {notePlain.map((p) => (
            <NavLink key={p.path} to={`/note/${p.path}`} className={({ isActive }) => navClass(isActive)}>
              <span className="icon"><Icon name="folder" /></span>
              {p.title}
              {p.open_tasks > 0 && <span className="count">{p.open_tasks}</span>}
            </NavLink>
          ))}
          {tracker && plain && !tracker.items.length && !notePlain.length && (
            <button className="nav-item" onClick={() => create("project")}>
              <span className="icon"><Icon name="plus" /></span>Создать первый проект
            </button>
          )}
        </div>

        <div className="nav-group">
          <div className="nav-title">Знания</div>
          <Item to="/docs" icon="book" label="Документация" />
          <NavLink to="/notes" className={({ isActive }) => navClass(isActive && !params.get("section"))}>
            <span className="icon"><Icon name="file" /></span>Заметки
          </NavLink>
          <Item to="/calendar" icon="calendar" label="Календарь" />
        </div>

        <div className="nav-group">
          <div className="nav-title">Настройка</div>
          <Item to="/people" icon="users" label="Команда" />
          {admin && <Item to="/integrations" icon="plug" label="Интеграции" />}
          {admin && <Item to="/passwords" icon="key" label="Пароли" />}
        </div>
      </div>
      <div className="sidebar-foot">
        {me?.mode === "oidc" && me.authenticated
          ? <div className="sidebar-user">
              <span className="sidebar-user-name" title={me.email}>{me.name}</span>
              <a href={me.logout_url}>Выйти</a>
            </div>
          : <Signature />}
        <ThemeSwitch />
      </div>
      {creating === "note" && createPortal(<NewNoteDialog onClose={() => setCreating(null)} />, document.body)}
      {creating === "issue" && createPortal(<NewIssueDialog onClose={() => setCreating(null)} />, document.body)}
      {creating === "project" && createPortal(<NewProjectDialog onClose={() => setCreating(null)} />, document.body)}
    </nav>
  );
}
