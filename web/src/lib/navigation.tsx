"use client";

import NextLink from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useCallback, useEffect, useMemo, useSyncExternalStore, type ComponentProps, type ReactNode } from "react";
import { confirmLeave, subscribeLocation } from "./navigationGuard";

/**
 * The app's navigation on top of next/navigation. Components use these instead
 * of Next's own, so every route change passes the draft guard and works with
 * the placeholder params of the static export.
 */

type LinkProps = ComponentProps<typeof NextLink> & { href: string };

/** A next/link that respects the draft guard. Long lists do not prefetch: pages hold no data. */
export function Link({ prefetch = false, onNavigate, ...props }: LinkProps) {
  return <NextLink {...props} prefetch={prefetch} onNavigate={(e) => {
    if (!confirmLeave(props.href)) { e.preventDefault(); return; }
    onNavigate?.(e);
  }} />;
}

type NavLinkProps = Omit<LinkProps, "className"> & {
  /** Active only on the exact path, not on the pages below it. */
  end?: boolean;
  className?: string | ((state: { isActive: boolean }) => string);
  children?: ReactNode;
};

/** A Link that knows whether it points at the current page. */
export function NavLink({ end, className, ...props }: NavLinkProps) {
  const { pathname } = useLocation();
  const target = new URL(props.href, "http://lumen").pathname;
  const current = new URL(pathname, "http://lumen").pathname;
  const isActive = current === target || (!end && current.startsWith(`${target}/`));
  const classes = typeof className === "function"
    ? className({ isActive })
    : [className, isActive && "active"].filter(Boolean).join(" ");
  return <Link {...props} className={classes} aria-current={isActive ? "page" : undefined} />;
}

/** Returns a function that navigates after the draft guard agrees. */
export function useNavigate() {
  const router = useRouter();
  return useCallback((href: string, options?: { replace?: boolean }) => {
    if (!confirmLeave(href)) return;
    if (options?.replace) router.replace(href);
    else router.push(href);
  }, [router]);
}

/** Redirects as soon as it renders. */
export function Navigate({ href }: { href: string }) {
  const navigate = useNavigate();
  useEffect(() => { navigate(href, { replace: true }); }, [href, navigate]);
  return null;
}

/** The current address. Next's hooks do not expose the hash. */
export function useLocation() {
  const pathname = usePathname() ?? "";
  const query = useSearchParams()?.toString() ?? "";
  const hash = useSyncExternalStore(subscribeLocation, () => window.location.hash, () => "");
  return useMemo(() => ({ pathname, search: query && `?${query}`, hash }), [pathname, query, hash]);
}

/**
 * A segment of the current path, counted from zero: on /tracker/WEB/board
 * segment 1 is "WEB". Next's useParams cannot be used: the static export has
 * one prebuilt page per route, whose params are the placeholder "_".
 */
export function useRouteParam(segment: number) {
  const value = (usePathname() ?? "").split("/").filter(Boolean)[segment] ?? "";
  try { return decodeURIComponent(value); } catch { return value; }
}

type QueryInit = URLSearchParams | Record<string, string>;

/**
 * The query string as state. Writing goes straight to history, which Next keeps
 * in sync: the page does not change, so there is nothing to fetch.
 */
export function useQueryParams() {
  const pathname = usePathname() ?? "";
  const current = useSearchParams();
  const params = useMemo(() => new URLSearchParams(current?.toString() ?? ""), [current]);
  const setParams = useCallback((next: QueryInit, options?: { replace?: boolean }) => {
    const query = new URLSearchParams(next).toString();
    const url = pathname + (query && `?${query}`);
    if (options?.replace) window.history.replaceState(null, "", url);
    else window.history.pushState(null, "", url);
  }, [pathname]);
  return [params, setParams] as const;
}
