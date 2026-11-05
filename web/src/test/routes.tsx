import type { ReactNode } from "react";
import { useLocation } from "../lib/navigation";
import { confirmLeave } from "../lib/navigationGuard";

/** Opens an address before a test renders. */
export function startAt(href: string) {
  window.history.replaceState(null, "", href);
}

/** A navigation as a link would do it: the draft guard may refuse. */
export function navigateTo(href: string) {
  if (confirmLeave(href)) window.history.pushState(null, "", href);
}

const toRegExp = (pattern: string) =>
  new RegExp(`^${pattern.replace(/\/\*$/, "/.+").replace(/:[^/]+/g, "[^/]+")}$`);

/** Stands in for the file routes of src/app, so a test can see where it ended up. */
export function TestRoutes({ routes }: { routes: Record<string, ReactNode> }) {
  const { pathname } = useLocation();
  const match = Object.keys(routes).find((pattern) => toRegExp(pattern).test(pathname));
  return <>{match && routes[match]}</>;
}
