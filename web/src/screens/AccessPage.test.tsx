// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { startAt } from "../test/routes";
import { api } from "../api/client";
import type { ProjectAccess, TrackerProject } from "../api/types";
import AccessPage from "./AccessPage";

const project: TrackerProject = {
  key: "SEC", title: "Секретный", note_id: "p", path: "20-projects/sec.md", issue_seq: 0, revision: 1,
  issue_types: ["task"], workflow: [{ id: "todo", name: "К работе", category: "todo" }],
};
const open: ProjectAccess = { open: true, my_role: "admin", items: [] };
const closed: ProjectAccess = {
  open: false, my_role: "member",
  items: [{ project: "SEC", kind: "user", name: "alice", role: "member", added_at: "2026-11-02T10:00:00Z", added_by: "boss" }],
};

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

function show(access: ProjectAccess) {
  vi.spyOn(api, "trackerProject").mockResolvedValue(project);
  vi.spyOn(api, "projectAccess").mockResolvedValue(access);
  vi.spyOn(api, "users").mockResolvedValue({ items: [{ username: "alice", name: "Алиса", groups: [], role: "member", first_seen: "", last_seen: "" }], total: 1, limit: 0, offset: 0 });
  vi.spyOn(api, "groups").mockResolvedValue({ items: [{ name: "designers", users: 2 }], total: 1, limit: 0, offset: 0 });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  startAt("/tracker/SEC/access");
  render(<QueryClientProvider client={client}><AccessPage /></QueryClientProvider>);
}

test("adding the first member warns that the project closes", async () => {
  const set = vi.spyOn(api, "setMember").mockResolvedValue(closed);
  const confirm = vi.spyOn(window, "confirm").mockReturnValueOnce(false).mockReturnValueOnce(true);
  show(open);
  await screen.findByText("Открытый проект");
  fireEvent.change(screen.getByRole("combobox", { name: /Имя пользователя/ }), { target: { value: "alice" } });
  fireEvent.click(screen.getByText("Добавить"));
  expect(confirm).toHaveBeenCalledTimes(1);
  expect(set).not.toHaveBeenCalled();
  fireEvent.click(screen.getByText("Добавить"));
  await waitFor(() => expect(set).toHaveBeenCalledWith("SEC", { kind: "user", name: "alice", role: "member" }));
});

test("a project member sees the list read-only", async () => {
  show(closed);
  await screen.findByText("Закрытый проект");
  expect(screen.getByText("Алиса (alice)")).toBeTruthy();
  expect(screen.queryByText("Добавить")).toBeNull();
  expect(screen.queryByRole("button", { name: /Убрать/ })).toBeNull();
});
