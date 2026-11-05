// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { startAt, TestRoutes } from "../test/routes";
import { api } from "../api/client";
import type { Issue, TrackerProject } from "../api/types";
import TimelinePage from "./TimelinePage";

const project: TrackerProject = {
  key: "WEB", title: "Сайт", note_id: "p", path: "20-projects/web.md", issue_seq: 3, revision: 1, issue_types: ["task", "epic"],
  workflow: [{ id: "todo", name: "К работе", category: "todo" }, { id: "done", name: "Готово", category: "done" }],
};
const issue = (key: string, extra: Partial<Issue>): Issue =>
  ({ key, project: "WEB", path: `${key}.md`, title: `Тикет ${key}`, issue_type: "task", status: "todo", start: null, due: null, revision: 1, ...extra });
const list = [
  issue("WEB-1", { start: "2026-11-02", due: "2026-11-05", links: [{ type: "blocks", target: "WEB-2" }] }),
  issue("WEB-2", { start: "2026-11-03", due: "2026-11-09" }),
  issue("WEB-3", {}),
];

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

async function setup() {
  vi.spyOn(api, "trackerProject").mockResolvedValue(project);
  vi.spyOn(api, "sprints").mockResolvedValue({ items: [], total: 0, limit: 0, offset: 0 });
  vi.spyOn(api, "users").mockResolvedValue({ items: [], total: 0, limit: 0, offset: 0 });
  vi.spyOn(api, "issues").mockResolvedValue({ items: list, total: list.length, limit: 1000, offset: 0 });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  startAt("/tracker/WEB/timeline");
  const view = render(<QueryClientProvider client={client}><TestRoutes routes={{ "/tracker/:key/timeline": <TimelinePage />, "/issue/:key": <p>Карточка</p> }} /></QueryClientProvider>);
  await screen.findByText("Без дат");
  const bar = (key: string) => view.container.querySelector<HTMLElement>(`[data-bar="${key}"]`)!;
  return { view, bar };
}

test("shows bars for dated issues, keeps undated ones apart and marks a broken link", async () => {
  const { view, bar } = await setup();
  expect(bar("WEB-1")).toBeTruthy();
  expect(bar("WEB-3")).toBeNull();
  // WEB-2 starts before its blocker ends.
  expect(view.container.querySelector('[data-edge-link="WEB-1>WEB-2"]')!.getAttribute("class")).toContain("is-broken");
  expect(screen.getByText("Нарушенных зависимостей: 1")).toBeTruthy();
});

test("Alt+arrow moves both dates of one issue; Alt+Shift+arrow only its due", async () => {
  const patch = vi.spyOn(api, "patchIssue").mockImplementation(async (key) => list.find((i) => i.key === key)!);
  const { bar } = await setup();
  fireEvent.keyDown(bar("WEB-1"), { key: "ArrowRight", altKey: true });
  await waitFor(() => expect(patch).toHaveBeenCalledWith("WEB-1", { start: "2026-11-03", due: "2026-11-06" }));
  await waitFor(() => expect(bar("WEB-1").title).toBeTruthy());
  patch.mockClear();
  fireEvent.keyDown(bar("WEB-2"), { key: "ArrowRight", altKey: true, shiftKey: true });
  await waitFor(() => expect(patch).toHaveBeenCalledWith("WEB-2", { due: "2026-11-10" }));
  expect(patch).toHaveBeenCalledTimes(1);
});

test("quick presses are sent in order, one at a time", async () => {
  let release = () => {};
  const patch = vi.spyOn(api, "patchIssue").mockImplementationOnce(() => new Promise((res) => { release = () => res(list[0]); }));
  patch.mockResolvedValue(list[0]);
  const { bar } = await setup();
  fireEvent.keyDown(bar("WEB-1"), { key: "ArrowRight", altKey: true });
  await waitFor(() => expect(patch).toHaveBeenCalledTimes(1));
  await waitFor(() => expect(bar("WEB-1").getAttribute("aria-label")).toContain("3 ноября"));
  fireEvent.keyDown(bar("WEB-1"), { key: "ArrowRight", altKey: true });
  await new Promise((r) => setTimeout(r, 20));
  expect(patch).toHaveBeenCalledTimes(1);
  release();
  await waitFor(() => expect(patch).toHaveBeenLastCalledWith("WEB-1", { start: "2026-11-04", due: "2026-11-07" }));
});

test("a refused change puts the bar back and says why", async () => {
  vi.spyOn(api, "patchIssue").mockRejectedValue(new Error("нет прав"));
  const { bar } = await setup();
  fireEvent.keyDown(bar("WEB-1"), { key: "ArrowLeft", altKey: true });
  await screen.findByText("Даты не изменены");
  expect(bar("WEB-1").getAttribute("aria-label")).toContain("2 ноября");
});

test("Enter opens the issue", async () => {
  const { bar } = await setup();
  fireEvent.keyDown(bar("WEB-1"), { key: "Enter" });
  await waitFor(() => expect(window.location.pathname).toBe("/issue/WEB-1"));
});
