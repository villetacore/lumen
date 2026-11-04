import { useEffect, useRef, useState } from "react";
import Brand from "./components/Brand";
import Icon from "./components/Icon";
import { Navigate, Route, Routes, useLocation } from "react-router-dom";
import TopBar from "./components/TopBar";
import Sidebar from "./components/Sidebar";
import TasksPage from "./screens/TasksPage";
import CalendarPage from "./screens/CalendarPage";
import NotesPage from "./screens/NotesPage";
import DocsPage from "./screens/DocsPage";
import { EmptyState, IconButton } from "@villetacore/velessoft-ui";
import TaskPage from "./screens/TaskPage";
import NotePage from "./screens/NotePage";
import SearchPage from "./screens/SearchPage";
import IntegrationsPage from "./screens/IntegrationsPage";
import PasswordsPage from "./screens/PasswordsPage";
import TrackerPage from "./screens/TrackerPage";
import IssuesPage from "./screens/IssuesPage";
import IssuePage from "./screens/IssuePage";
import BoardPage from "./screens/BoardPage";
import TimelinePage from "./screens/TimelinePage";
import BacklogPage from "./screens/BacklogPage";
import AccessPage from "./screens/AccessPage";
import PeoplePage from "./screens/PeoplePage";
import InboxPage from "./screens/InboxPage";
import MyIssuesPage from "./screens/MyIssuesPage";
import ApprovalsPage from "./screens/ApprovalsPage";
import AuthGate from "./components/AuthGate";
import { useLiveUpdates } from "./hooks/useLiveUpdates";

export default function App() {
  return <AuthGate><Workspace /></AuthGate>;
}

function Workspace() {
  useLiveUpdates();
  const [menuOpen, setMenuOpen] = useState(false);
  const location = useLocation();
  const menuButton = useRef<HTMLButtonElement>(null);
  const wasOpen = useRef(false);
  const closeMenu = () => setMenuOpen(false);
  useEffect(() => { setMenuOpen(false); window.scrollTo(0, 0); }, [location]);
  useEffect(() => {
    const desktop = window.matchMedia("(min-width: 961px)");
    const closeOnDesktop = () => { if (desktop.matches) setMenuOpen(false); };
    desktop.addEventListener("change", closeOnDesktop);
    return () => desktop.removeEventListener("change", closeOnDesktop);
  }, []);
  useEffect(() => {
    if (!menuOpen) {
      if (wasOpen.current && !document.querySelector('[role="dialog"]')) menuButton.current?.focus();
      wasOpen.current = false;
      return;
    }
    wasOpen.current = true;
    const nav = document.getElementById("workspace-navigation");
    const frame = requestAnimationFrame(() => nav?.querySelector<HTMLButtonElement>("button")?.focus());
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setMenuOpen(false);
      if (e.key !== "Tab") return;
      const items = Array.from(nav?.querySelectorAll<HTMLElement>('a[href], button:not(:disabled), input, select, textarea') ?? []).filter(el => el.getClientRects().length);
      const first = items[0], last = items[items.length - 1];
      if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last?.focus(); }
      else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first?.focus(); }
    };
    window.addEventListener("keydown", onKey);
    return () => { cancelAnimationFrame(frame); window.removeEventListener("keydown", onKey); };
  }, [menuOpen]);

  return (
    <div className="app">
      <Sidebar open={menuOpen} onClose={closeMenu} />
      {menuOpen && <button className="scrim" onClick={closeMenu} aria-label="Закрыть навигацию" tabIndex={-1} />}
      <div className="workspace" inert={menuOpen}>
      <header className="mobilebar"><IconButton ref={menuButton} variant="ghost" label="Открыть меню" aria-expanded={menuOpen} aria-controls="workspace-navigation" onClick={() => setMenuOpen(true)}><Icon name="menu" size={18} /></IconButton><Brand /></header>
      <main className="main">
      <TopBar />
        <Routes>
          <Route path="/" element={<Navigate to="/view/today" replace />} />
          <Route path="/view/:view" element={<TasksPage />} />
          <Route path="/calendar" element={<CalendarPage />} />
          <Route path="/notes" element={<NotesPage />} />
          <Route path="/docs" element={<DocsPage />} />
          <Route path="/task/:id" element={<TaskPage />} />
          <Route path="/note/*" element={<NotePage />} />
          <Route path="/tracker" element={<TrackerPage />} />
          <Route path="/tracker/:key" element={<IssuesPage />} />
          <Route path="/tracker/:key/board" element={<BoardPage />} />
          <Route path="/tracker/:key/backlog" element={<BacklogPage />} />
          <Route path="/tracker/:key/timeline" element={<TimelinePage />} />
          <Route path="/tracker/:key/access" element={<AccessPage />} />
          <Route path="/people" element={<PeoplePage />} />
          <Route path="/inbox" element={<InboxPage />} />
          <Route path="/my" element={<MyIssuesPage />} />
          <Route path="/approvals" element={<ApprovalsPage />} />
          <Route path="/issue/:key" element={<IssuePage />} />
          <Route path="/search" element={<SearchPage />} />
          <Route path="/integrations" element={<IntegrationsPage />} />
          <Route path="/passwords" element={<PasswordsPage />} />
          <Route path="*" element={<EmptyState title="Страница не найдена" description="Проверьте адрес или вернитесь к задачам." />} />
        </Routes>
      </main>
      </div>
    </div>
  );
}
