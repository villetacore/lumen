import { useEffect } from "react";

/**
 * Next has no navigation blocker, so unsaved drafts are protected here. Every
 * way of leaving a page is covered: links and programmatic navigation call
 * confirmLeave (lib/navigation.tsx), back and forward are caught in popstate,
 * and closing the tab asks through beforeunload.
 */

declare global {
  interface Window { __lumenOnPopState?: ((event: PopStateEvent) => void) | null }
}

const LEAVE_MESSAGE = "Есть несохранённые изменения. Уйти и потерять их?";
const INDEX_KEY = "__lumenIndex";

const guards = new Set<{ message: string }>();
const listeners = new Set<() => void>();
let index = 0;
let currentPath = "";
let restoring = false;
let installed = false;

/** Asks before a navigation to another page. Changing only the query never asks. */
export function confirmLeave(href: string): boolean {
  const [guard] = guards;
  if (!guard) return true;
  if (new URL(href, window.location.href).pathname === window.location.pathname) return true;
  return window.confirm(guard.message);
}

/** useLeaveGuard asks before leaving the page while `active` is true. */
export function useLeaveGuard(active: boolean, message = LEAVE_MESSAGE) {
  useEffect(() => {
    if (!active) return;
    const guard = { message };
    const onBeforeUnload = (e: BeforeUnloadEvent) => e.preventDefault();
    guards.add(guard);
    window.addEventListener("beforeunload", onBeforeUnload);
    return () => {
      guards.delete(guard);
      window.removeEventListener("beforeunload", onBeforeUnload);
    };
  }, [active, message]);
}

/** Calls `listener` after each history change, including changes of only the hash. */
export function subscribeLocation(listener: () => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

function changed() {
  currentPath = window.location.pathname;
  // Next writes history from an insertion effect, where React forbids updates.
  queueMicrotask(() => listeners.forEach((listener) => listener()));
}

function onPopState(event: PopStateEvent) {
  const target = event.state?.[INDEX_KEY];
  if (restoring) {
    restoring = false;
    event.stopImmediatePropagation();
    return;
  }
  const [guard] = guards;
  const leaving = window.location.pathname !== currentPath;
  if (guard && leaving && typeof target === "number" && !window.confirm(guard.message)) {
    // Stop Next from rendering the target, then step back to where we were.
    event.stopImmediatePropagation();
    restoring = true;
    window.history.go(index - target);
    return;
  }
  if (typeof target === "number") index = target;
  changed();
}

/**
 * Numbers history entries, because popstate does not say how far the user
 * jumped and undoing a declined jump needs that distance. Call it once at
 * startup. Next unmounts the page from its own popstate listener, so the guard
 * must run first: app/layout.tsx registers a forwarding listener before any of
 * Next's scripts load. Without it (tests) the guard listens directly.
 */
export function installNavigationGuard() {
  if (installed || typeof window === "undefined") return;
  installed = true;
  const { history } = window;
  const saved = history.state?.[INDEX_KEY];
  index = typeof saved === "number" ? saved : 0;
  currentPath = window.location.pathname;

  const push = history.pushState;
  const replace = history.replaceState;
  history.pushState = function pushState(data, unused, url) {
    index += 1;
    push.call(history, { ...data, [INDEX_KEY]: index }, unused, url);
    changed();
  };
  history.replaceState = function replaceState(data, unused, url) {
    // Next also writes to an entry the browser has just traversed to, before
    // popstate announces it. The entry keeps its number, and until popstate
    // decides, the user has not left the page they were on.
    const own = history.state?.[INDEX_KEY];
    replace.call(history, { ...data, [INDEX_KEY]: typeof own === "number" ? own : index }, unused, url);
    if (typeof own !== "number" || own === index) changed();
  };
  history.replaceState(history.state, "");
  if ("__lumenOnPopState" in window) window.__lumenOnPopState = onPopState;
  else window.addEventListener("popstate", onPopState, { capture: true });
}
