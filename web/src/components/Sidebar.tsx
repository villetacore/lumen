import { createPortal } from "react-dom";
import Brand, { Signature } from "./Brand";
import ThemeSwitch from "./ThemeSwitch";
import Icon, { type IconName } from "./Icon";
import { useState } from "react";
import { NavLink, useLocation, useNavigate, useQueryParams } from "../lib/navigation";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import NewNoteDialog from "./NewNoteDialog";
import NewIssueDialog from "./NewIssueDialog";
import NewProjectDialog from "./NewProjectDialog";
import { projectUrl } from "../lib/tracker";
import { buildTree, flattenTree, useCollapsed } from "../lib/tree";
import { IconButton, Menu } from "@villetacore/velessoft-ui";

const navClass = (active: boolean) => "nav-item" + (active ? " active" : "");

function Item({ to, icon, label, count, end, active }: { to: string; icon: IconName; label: string; count?: number; end?: boolean; active?: boolean }) {
  return <NavLink href={to} end={end} className={({ isActive }) => navClass(active ?? isActive)}>
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
  const [params] = useQueryParams();
  const [creating, setCreating] = useState<"note" | "entry" | "issue" | "project" | null>(null);
  const { data: me } = useQuery({ queryKey: ["me"], queryFn: api.me });
  const { data: plain } = useQuery({ queryKey: ["notes", { type: "project" }], queryFn: () => api.notes({ type: "project" }) });
  const { data: tracker } = useQuery({ queryKey: ["tracker", "projects"], queryFn: api.trackerProjects });
  const { data: proposals } = useQuery({ queryKey: ["proposals", "pending"], queryFn: () => api.proposals(true) });
  const { data: today } = useQuery({ queryKey: ["tasks", "count", "today"], queryFn: () => api.tasks({ view: "today", limit: 1 }) });

  const admin = !me?.role || me.role === "admin";
  const hasBoard = new Set(tracker?.items.map((p) => p.path));
  const notePlain = plain?.items.filter((n) => !hasBoard.has(n.path)) ?? [];
  const { collapsed, toggle } = useCollapsed("lumen.projects.collapsed");
  const visibleProjects = flattenTree(buildTree(tracker?.items ?? [], (p) => p.key, (p) => p.parent), collapsed, (p) => p.key);
  const create = (what: "note" | "entry" | "issue" | "project") => { setCreating(what); if (open) onClose(); };

  return (
    <nav id="workspace-navigation" className={`sidebar${open ? " open" : ""}`} aria-label="Основная навигация">
      <div className="sidebar-brand"><Brand /><IconButton className="mobile-only" variant="ghost" onClick={onClose} label="Закрыть меню"><Icon name="close" size={16} /></IconButton></div>
      <div className="sidebar-create">
        <Menu variant="primary" className="create-menu" label={<><Icon name="plus" size={16} />Создать</>} items={[
          { label: "Тикет в проекте", icon: <Icon name="board" size={16} />, disabled: !tracker?.items.length, onSelect: () => create("issue") },
          { label: "Задача", icon: <Icon name="check" size={16} />, onSelect: () => navigate("/view/inbox#add") },
          { label: "Заметка", icon: <Icon name="edit" size={16} />, onSelect: () => create("entry") },
          { label: "Документ", icon: <Icon name="book" size={16} />, onSelect: () => create("note") },
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
          {visibleProjects.map(({ item: p, depth, children }) => (
            <div key={p.key} className="nav-tree-row" style={{ paddingLeft: depth * 14 }}>
              {children.length > 0
                ? <button type="button" className="tree-toggle" aria-expanded={!collapsed.has(p.key)}
                    aria-label={`${collapsed.has(p.key) ? "Развернуть" : "Свернуть"} ${p.title}`} onClick={() => toggle(p.key)}>
                    <Icon name="chevron" size={14} className={collapsed.has(p.key) ? "" : "is-open"} />
                  </button>
                : <span className="tree-toggle-space" />}
              <NavLink href={`${projectUrl(p.key)}/board`} className={navClass(pathname === projectUrl(p.key) || pathname.startsWith(`${projectUrl(p.key)}/`))}>
                <span className="icon nav-key">{p.key}</span>
                {p.title}
              </NavLink>
            </div>
          ))}
          {notePlain.map((p) => (
            <NavLink key={p.path} href={`/note/${p.path}`} className={({ isActive }) => navClass(isActive)}>
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
          <NavLink href="/notes" className={({ isActive }) => navClass(isActive && !params.get("section"))}>
            <span className="icon"><Icon name="file" /></span>Заметки
          </NavLink>
          <Item to="/calendar" icon="calendar" label="Календарь" />
        </div>

        <div className="nav-group">
          <div className="nav-title">Настройка</div>
          <Item to="/people" icon="users" label="Команда" />
          <Item to="/connect" icon="spark" label="AI-клиенты" />
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
      {creating === "entry" && createPortal(<NewNoteDialog contentKind="entry" onClose={() => setCreating(null)} />, document.body)}
      {creating === "note" && createPortal(<NewNoteDialog onClose={() => setCreating(null)} />, document.body)}
      {creating === "issue" && createPortal(<NewIssueDialog onClose={() => setCreating(null)} />, document.body)}
      {creating === "project" && createPortal(<NewProjectDialog onClose={() => setCreating(null)} />, document.body)}
    </nav>
  );
}
