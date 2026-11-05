// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { navigateTo, startAt, TestRoutes } from "../test/routes";
import { api, ApiError } from "../api/client";
import type { Note, NoteSummary } from "../api/types";
import TaskDescription from "./TaskDescription";

const note: Note = { id: "description", path: "description.md", title: "Описание", type: "note", tags: [], body: "Требования", revision: 1, hash: "h", size: 10 };
const summary: NoteSummary = { ...note, tasks: 0, open_tasks: 0, parent: "task:stable", content_kind: "description" };
afterEach(() => { cleanup(); vi.restoreAllMocks(); });
async function setup(existing = true) {
  vi.spyOn(api, "notes").mockResolvedValue({ items: existing ? [summary] : [], total: existing ? 1 : 0, limit: 200, offset: 0 });
  vi.spyOn(api, "note").mockResolvedValue(note);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  startAt("/task");
  render(<QueryClientProvider client={client}><TestRoutes routes={{ "/task": <TaskDescription parent="task:stable" />, "/away": <p>Другой экран</p> }} /></QueryClientProvider>);
  fireEvent.click(await screen.findByText(existing ? "Изменить описание" : "Добавить описание"));
  const editor = screen.getByRole("textbox", { name: "Описание задачи" }) as HTMLTextAreaElement;
  fireEvent.change(editor, { target: { value: "Мой черновик" } });
  return { client, editor };
}

test("new description is bound to its task and persists", async () => {
  const create = vi.spyOn(api, "createNote").mockResolvedValue({ ...note, body: "Мой черновик" });
  await setup(false);
  fireEvent.click(screen.getByText("Сохранить описание"));
  await waitFor(() => expect(create).toHaveBeenCalledWith({ parent: "task:stable", content_kind: "description", title: "Описание", body: "Мой черновик" }));
});

test("external refetch preserves draft and blocks a stale write", async () => {
  const put = vi.spyOn(api, "putNoteBody");
  const { client, editor } = await setup();
  vi.mocked(api.note).mockResolvedValue({ ...note, body: "Из другого окна", revision: 2 });
  await act(async () => { await client.invalidateQueries({ queryKey: ["note"] }); });
  await screen.findByText(/Описание изменилось/);
  expect(editor.value).toBe("Мой черновик");
  expect((screen.getByText("Сохранить описание") as HTMLButtonElement).disabled).toBe(true);
  expect(put).not.toHaveBeenCalled();
});

test("409 keeps text and declining navigation preserves the editor", async () => {
  const put = vi.spyOn(api, "putNoteBody").mockRejectedValue(new ApiError(409, "conflict", "version changed"));
  const { editor } = await setup();
  fireEvent.click(screen.getByText("Сохранить описание"));
  await screen.findByText(/Описание изменилось/);
  expect(put).toHaveBeenCalledWith("description.md", "Мой черновик", 1);
  vi.spyOn(window, "confirm").mockReturnValue(false);
  act(() => navigateTo("/away"));
  expect(window.location.pathname).toBe("/task");
  expect(editor.value).toBe("Мой черновик");
});
