// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { api } from "../api/client";
import type { Issue, TrackerProject } from "../api/types";
import IssuePage from "./IssuePage";

const project: TrackerProject = {
  key: "WEB", title: "Сайт", note_id: "p", path: "20-projects/web.md", issue_seq: 1, revision: 1,
  issue_types: ["task", "bug"], transitions: { todo: ["doing"] },
  workflow: [
    { id: "todo", name: "К работе", category: "todo" },
    { id: "doing", name: "В работе", category: "in_progress" },
    { id: "done", name: "Готово", category: "done" },
  ],
};
const issue: Issue = {
  key: "WEB-1", project: "WEB", path: "20-projects/WEB/issues/WEB-1.md", title: "Доска", issue_type: "task",
  status: "todo", start: null, due: null, revision: 1, description: "Было", comments: [],
};

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

async function setup() {
  vi.spyOn(api, "issue").mockResolvedValue(issue);
  vi.spyOn(api, "trackerProject").mockResolvedValue(project);
  vi.spyOn(api, "users").mockResolvedValue({ items: [], total: 0, limit: 0, offset: 0 });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const router = createMemoryRouter([
    { path: "/issue/:key", element: <IssuePage /> },
    { path: "/away", element: <p>Другой экран</p> },
  ], { initialEntries: ["/issue/WEB-1"] });
  render(<QueryClientProvider client={client}><RouterProvider router={router} /></QueryClientProvider>);
  await screen.findByText("Доска");
  fireEvent.click(screen.getByText("Изменить"));
  const editor = screen.getByRole("textbox", { name: "Описание тикета" }) as HTMLTextAreaElement;
  fireEvent.change(editor, { target: { value: "Мой черновик" } });
  return { client, router, editor };
}

test("status select offers only the workflow's allowed moves", async () => {
  await setup();
  const status = screen.getByRole("combobox", { name: "Статус" }) as HTMLSelectElement;
  expect([...status.options].map((o) => o.value)).toEqual(["todo", "doing"]);
});

test("a field changed elsewhere keeps the draft and lets it save", async () => {
  const patch = vi.spyOn(api, "patchIssue").mockResolvedValue({ ...issue, description: "Мой черновик", revision: 3 });
  const { client, editor } = await setup();
  vi.mocked(api.issue).mockResolvedValue({ ...issue, assignee: "alice", revision: 2 });
  await act(async () => { await client.invalidateQueries({ queryKey: ["issue"] }); });
  await waitFor(() => expect((screen.getAllByRole("combobox", { name: "Исполнитель" })[0] as HTMLInputElement).value).toBe("alice"));
  expect(editor.value).toBe("Мой черновик");
  expect(screen.queryByText("Описание изменилось")).toBeNull();
  fireEvent.click(screen.getByText("Сохранить описание"));
  await waitFor(() => expect(patch).toHaveBeenCalledWith("WEB-1", { description: "Мой черновик", revision: 2 }));
});

test("a description changed elsewhere warns, keeps the draft and blocks a stale save", async () => {
  const patch = vi.spyOn(api, "patchIssue").mockResolvedValue({ ...issue, description: "Мой черновик", revision: 3 });
  const { client, editor } = await setup();
  vi.mocked(api.issue).mockResolvedValue({ ...issue, description: "Из Obsidian", revision: 2 });
  await act(async () => { await client.invalidateQueries({ queryKey: ["issue"] }); });
  await screen.findByText("Описание изменилось");
  expect(editor.value).toBe("Мой черновик");
  expect((screen.getByText("Сохранить описание") as HTMLButtonElement).disabled).toBe(true);
  fireEvent.click(screen.getByText("Сохранить поверх"));
  await waitFor(() => expect(patch).toHaveBeenCalledWith("WEB-1", { description: "Мой черновик", revision: undefined }));
});

test("leaving with a draft asks first", async () => {
  const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
  const { router, editor } = await setup();
  await act(async () => { await router.navigate("/away"); });
  expect(confirm).toHaveBeenCalled();
  expect(router.state.location.pathname).toBe("/issue/WEB-1");
  expect(editor.value).toBe("Мой черновик");
});
