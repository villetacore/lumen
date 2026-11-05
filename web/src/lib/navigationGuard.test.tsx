// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/react";
import { afterEach, beforeAll, expect, test, vi } from "vitest";
import { confirmLeave, installNavigationGuard, useLeaveGuard } from "./navigationGuard";

function Draft({ dirty }: { dirty: boolean }) {
  useLeaveGuard(dirty);
  return null;
}

beforeAll(installNavigationGuard);
afterEach(() => { cleanup(); vi.restoreAllMocks(); });

test("asks only while there is a draft, and not when only the query changes", () => {
  window.history.replaceState(null, "", "/note/a.md");
  const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
  expect(confirmLeave("/notes")).toBe(true);
  render(<Draft dirty />);
  expect(confirmLeave("/note/a.md?view=1")).toBe(true);
  expect(confirm).not.toHaveBeenCalled();
  expect(confirmLeave("/notes")).toBe(false);
  expect(confirm).toHaveBeenCalledTimes(1);
});

test("the guard is gone with the page and with a clean draft", () => {
  window.history.replaceState(null, "", "/note/a.md");
  const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
  const { rerender, unmount } = render(<Draft dirty />);
  rerender(<Draft dirty={false} />);
  expect(confirmLeave("/notes")).toBe(true);
  rerender(<Draft dirty />);
  unmount();
  expect(confirmLeave("/notes")).toBe(true);
  expect(confirm).not.toHaveBeenCalled();
});

test("declining back returns to the page, over any distance", async () => {
  window.history.replaceState(null, "", "/view/today");
  window.history.pushState(null, "", "/calendar");
  window.history.pushState(null, "", "/note/a.md");
  const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
  render(<Draft dirty />);
  const seenByNext = vi.fn();
  window.addEventListener("popstate", seenByNext);
  window.history.go(-2);
  await vi.waitFor(() => expect(confirm).toHaveBeenCalledTimes(1));
  await vi.waitFor(() => expect(window.location.pathname).toBe("/note/a.md"));
  await new Promise((resolve) => setTimeout(resolve, 20));
  expect(confirm).toHaveBeenCalledTimes(1);
  expect(seenByNext).not.toHaveBeenCalled();
  window.removeEventListener("popstate", seenByNext);
});

test("a write that reaches the new entry before popstate does not count as leaving", async () => {
  window.history.replaceState(null, "", "/view/today");
  window.history.pushState(null, "", "/calendar");
  const target = window.history.state.__lumenIndex;
  window.history.pushState(null, "", "/note/c.md");
  const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
  render(<Draft dirty />);
  // The browser has traversed: the address changed, popstate has not fired yet.
  // The prototype's method bypasses the guard, which must not know about it yet.
  History.prototype.pushState.call(window.history, { __lumenIndex: target }, "", "/calendar");
  // Next writes to the entry at this moment.
  window.history.replaceState(window.history.state, "", "/calendar");
  expect(window.history.state.__lumenIndex).toBe(target);
  window.dispatchEvent(new PopStateEvent("popstate", { state: window.history.state }));
  expect(confirm).toHaveBeenCalledTimes(1);
  // The step back that the refusal requested.
  window.dispatchEvent(new PopStateEvent("popstate", { state: window.history.state }));
  expect(confirm).toHaveBeenCalledTimes(1);
});

test("accepting back lets the navigation through, and later jumps measure from there", async () => {
  window.history.replaceState(null, "", "/view/today");
  window.history.pushState(null, "", "/calendar");
  window.history.pushState(null, "", "/note/b.md");
  const confirm = vi.spyOn(window, "confirm").mockReturnValue(true);
  render(<Draft dirty />);
  window.history.back();
  await vi.waitFor(() => expect(window.location.pathname).toBe("/calendar"));
  expect(confirm).toHaveBeenCalledTimes(1);
  confirm.mockReturnValue(false);
  window.history.forward();
  await vi.waitFor(() => expect(confirm).toHaveBeenCalledTimes(2));
  await vi.waitFor(() => expect(window.location.pathname).toBe("/calendar"));
});
