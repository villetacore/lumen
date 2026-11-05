import { useMemo, useSyncExternalStore, type ComponentProps } from "react";
import { vi } from "vitest";

// Tests run against jsdom's real location and history. Only the parts of Next
// that need its app router are replaced.

function subscribe(listener: () => void) {
  window.addEventListener("test:location", listener);
  return () => window.removeEventListener("test:location", listener);
}
const notify = () => window.dispatchEvent(new Event("test:location"));
const useUrl = (read: () => string) => useSyncExternalStore(subscribe, read, read);

const router = {
  push: (href: string) => { window.history.pushState(null, "", href); notify(); },
  replace: (href: string) => { window.history.replaceState(null, "", href); notify(); },
};

vi.mock("next/navigation", () => ({
  usePathname: () => useUrl(() => window.location.pathname),
  useSearchParams: () => {
    const query = useUrl(() => window.location.search);
    return useMemo(() => new URLSearchParams(query), [query]);
  },
  useRouter: () => router,
}));

vi.mock("next/link", () => ({
  default: ({ prefetch: _prefetch, onNavigate, href, onClick, ...props }: ComponentProps<"a"> & {
    prefetch?: boolean;
    onNavigate?: (e: { preventDefault(): void }) => void;
  }) => <a {...props} href={href} onClick={(e) => {
    onClick?.(e);
    e.preventDefault();
    let cancelled = false;
    onNavigate?.({ preventDefault: () => { cancelled = true; } });
    if (!cancelled) router.push(String(href));
  }} />,
}));

if (typeof window !== "undefined") {
  // Direct history writes such as useQueryParams must reach the hooks above.
  for (const method of ["pushState", "replaceState"] as const) {
    const original = window.history[method];
    window.history[method] = function patched(...args: Parameters<History["pushState"]>) {
      original.apply(window.history, args);
      notify();
    };
  }
}
