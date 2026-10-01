import { Navigate, Route, Routes } from "react-router-dom";
import TopBar from "./components/TopBar";
import Sidebar from "./components/Sidebar";
import AsidePanel from "./components/AsidePanel";
import TasksPage from "./pages/TasksPage";
import CalendarPage from "./pages/CalendarPage";
import NotesPage from "./pages/NotesPage";
import NotePage from "./pages/NotePage";
import SearchPage from "./pages/SearchPage";
import IntegrationsPage from "./pages/IntegrationsPage";
import PasswordsPage from "./pages/PasswordsPage";
import { useLiveUpdates } from "./hooks/useLiveUpdates";

export default function App() {
  useLiveUpdates();

  return (
    <div className="app">
      <TopBar />
      <Sidebar />
      <main className="main">
        <Routes>
          <Route path="/" element={<Navigate to="/view/today" replace />} />
          <Route path="/view/:view" element={<TasksPage />} />
          <Route path="/calendar" element={<CalendarPage />} />
          <Route path="/notes" element={<NotesPage />} />
          <Route path="/note/*" element={<NotePage />} />
          <Route path="/search" element={<SearchPage />} />
          <Route path="/integrations" element={<IntegrationsPage />} />
          <Route path="/passwords" element={<PasswordsPage />} />
          <Route path="*" element={<div className="empty">Страница не найдена</div>} />
        </Routes>
      </main>
      <AsidePanel />
    </div>
  );
}
