import { useEffect, useRef, useState, type ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { IconButton, ToastProvider, VelessoftProvider } from "@villetacore/velessoft-ui";
import Brand from "./components/Brand";
import Icon from "./components/Icon";
import TopBar from "./components/TopBar";
import Sidebar from "./components/Sidebar";
import AuthGate from "./components/AuthGate";
import { useLiveUpdates } from "./hooks/useLiveUpdates";
import { useLocation } from "./lib/navigation";
import { installNavigationGuard } from "./lib/navigationGuard";

installNavigationGuard();

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { staleTime: 5_000, refetchOnWindowFocus: true, retry: 1 },
  },
});

/** Everything around a page. It renders only in the browser (see app/ClientShell.tsx). */
export default function Shell({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={queryClient}>
      <VelessoftProvider>
        <ToastProvider>
          <AuthGate><Workspace>{children}</Workspace></AuthGate>
        </ToastProvider>
      </VelessoftProvider>
    </QueryClientProvider>
  );
}

function Workspace({ children }: { children: ReactNode }) {
  useLiveUpdates();
  const [menuOpen, setMenuOpen] = useState(false);
  const { pathname, search, hash } = useLocation();
  const menuButton = useRef<HTMLButtonElement>(null);
  const wasOpen = useRef(false);
  const closeMenu = () => setMenuOpen(false);
  useEffect(() => { setMenuOpen(false); window.scrollTo(0, 0); }, [pathname, search, hash]);
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
        {children}
      </main>
      </div>
    </div>
  );
}
