// @vitest-environment jsdom
import { afterEach, expect, test, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { api, ApiError } from "../api/client";
import type { Note } from "../api/types";
import NotePage from "./NotePage";

const original: Note = {
  id: "n1", path: "test.md", title: "Тест", type: "note", tags: [],
  revision: 1, body: "Исходный текст", hash: "h", size: 10, tasks: [],
};
afterEach(() => { cleanup(); vi.restoreAllMocks(); });

async function setup() {
  vi.spyOn(api, "note").mockResolvedValue(original);
  vi.spyOn(api, "notes").mockResolvedValue({ items: [], total: 0, limit: 2000, offset: 0 });
  vi.spyOn(api, "backlinks").mockResolvedValue({ items: [], total: 0, limit: 100, offset: 0 });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  const router = createMemoryRouter([
    { path: "/note/*", element: <NotePage /> },
    { path: "/notes", element: <div>Все заметки</div> },
  ], { initialEntries: ["/note/test.md"] });
  render(<QueryClientProvider client={client}><RouterProvider router={router} /></QueryClientProvider>);
  fireEvent.click(await screen.findByText("✎ Редактировать"));
  const editor = screen.getByRole("textbox", { name: "Текст документа" }) as HTMLTextAreaElement;
  fireEvent.change(editor, { target: { value: "Мой текст" } });
  return { client, router, editor };
}

test("refetch preserves draft and explicit overwrite uses the latest revision", async () => {
  const put = vi.spyOn(api, "putNoteBody").mockResolvedValue({ ...original, body: "Мой текст", revision: 3 });
  const { client, editor } = await setup();
  vi.mocked(api.note).mockResolvedValue({ ...original, body: "Внешний текст", revision: 2 });
  await act(async () => { await client.invalidateQueries({ queryKey: ["note", "test.md"] }); });
  expect(editor.value).toBe("Мой текст");
  await screen.findByText(/Ваш текст не потерян/);
  expect((screen.getByRole("button", { name: "Сохранить" }) as HTMLButtonElement).disabled).toBe(true);
  fireEvent.click(screen.getByText("Сохранить мою версию поверх"));
  await waitFor(() => expect(put).toHaveBeenCalledWith("test.md", "Мой текст", 2));
});

test("typing during save stays editable and the next save uses the returned revision", async () => {
  let finish!: (note: Note) => void;
  const put = vi.spyOn(api, "putNoteBody").mockImplementationOnce(() => new Promise((resolve) => { finish = resolve; }));
  const { editor } = await setup();
  fireEvent.click(screen.getByRole("button", { name: "Сохранить" }));
  await waitFor(() => expect(put).toHaveBeenCalledTimes(1));
  fireEvent.change(editor, { target: { value: "Мой текст и продолжение" } });
  const saved = { ...original, body: "Мой текст", revision: 2 };
  vi.mocked(api.note).mockResolvedValue(saved);
  await act(async () => { finish(saved); });
  expect((screen.getByRole("textbox", { name: "Текст документа" }) as HTMLTextAreaElement).value).toBe("Мой текст и продолжение");
  put.mockResolvedValue({ ...saved, body: editor.value, revision: 3 });
  fireEvent.click(screen.getByRole("button", { name: "Сохранить" }));
  await waitFor(() => expect(put).toHaveBeenLastCalledWith("test.md", "Мой текст и продолжение", 2));
});

test("409 refetches the conflicting version without discarding the draft", async () => {
  vi.spyOn(api, "putNoteBody").mockRejectedValue(new ApiError(409, "conflict", "revision mismatch"));
  const { editor } = await setup();
  vi.mocked(api.note).mockResolvedValue({ ...original, revision: 2, body: "Чужой текст" });
  fireEvent.click(screen.getByRole("button", { name: "Сохранить" }));
  await screen.findByText(/Ваш текст не потерян/);
  expect(editor.value).toBe("Мой текст");
});

test("server failure is shown as a save error and preserves the draft", async () => {
  vi.spyOn(api, "putNoteBody").mockRejectedValue(new ApiError(500, "internal", "Ошибка записи"));
  const { editor } = await setup();
  fireEvent.click(screen.getByRole("button", { name: "Сохранить" }));
  await screen.findByText(/Не удалось сохранить: Ошибка записи/);
  expect(editor.value).toBe("Мой текст");
  expect(screen.queryByText(/Ваш текст не потерян/)).toBeNull();
});

test("declining navigation keeps the unsaved draft", async () => {
  const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
  const { router, editor } = await setup();
  await act(async () => { await router.navigate("/notes"); });
  expect(confirm).toHaveBeenCalled();
  expect(router.state.location.pathname).toBe("/note/test.md");
  expect(editor.value).toBe("Мой текст");
});
