// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { api } from "../api/client";
import type { NoteSummary } from "../api/types";
import PageTree from "./PageTree";

afterEach(() => { cleanup(); vi.restoreAllMocks(); localStorage.clear(); });

const page = (id: string, title: string, parent?: string, rank?: string) =>
  ({ id, title, parent, rank, content_kind: "document", path: `60-documents/${id}.md`, type: "note", tags: [], tasks: 0, open_tasks: 0, revision: 1 }) as NoteSummary;

const pages = [page("a", "Альфа", undefined, "a"), page("b", "Бета", undefined, "b"), page("c", "Гамма", "note:a", "a")];

function show(onCreate = vi.fn()) {
  const client = new QueryClient();
  render(<QueryClientProvider client={client}><PageTree pages={pages} roots={pages.filter((p) => !p.parent)} onCreate={onCreate} /></QueryClientProvider>);
  return onCreate;
}

test("children nest under their page and fold away", () => {
  show();
  const levels = screen.getAllByRole("treeitem").map((r) => [r.textContent?.includes("Гамма") ? "c" : "x", r.getAttribute("aria-level")]);
  expect(levels[1]).toEqual(["c", "2"]);
  fireEvent.click(screen.getByRole("button", { name: "Свернуть Альфа" }));
  expect(screen.queryByText("Гамма")).toBeNull();
});

test("the keyboard moves a page along the tree", async () => {
  const move = vi.spyOn(api, "movePage").mockResolvedValue(pages[1]);
  show();
  fireEvent.keyDown(screen.getByRole("link", { name: "Бета" }), { key: "ArrowUp", altKey: true });
  await waitFor(() => expect(move.mock.calls[0][0]).toEqual({ id: "b", parent: "", after: "a", before: undefined }));
  fireEvent.keyDown(screen.getByRole("link", { name: "Бета" }), { key: "ArrowRight", altKey: true });
  await waitFor(() => expect(move.mock.calls[1][0]).toEqual({ id: "b", parent: "note:a", before: "c" }));
  fireEvent.keyDown(screen.getByRole("link", { name: "Гамма" }), { key: "ArrowLeft", altKey: true });
  await waitFor(() => expect(move.mock.calls[2][0]).toEqual({ id: "c", parent: "", before: "a", after: "b" }));
});

test("a page cannot be dropped into its own subtree", () => {
  const move = vi.spyOn(api, "movePage").mockResolvedValue(pages[0]);
  show();
  const rows = screen.getAllByRole("treeitem");
  const dt = { setData: vi.fn(), effectAllowed: "" };
  fireEvent.dragStart(rows[0], { dataTransfer: dt });
  fireEvent.dragOver(rows[1], { dataTransfer: dt, clientY: 5 });
  fireEvent.drop(rows[1], { dataTransfer: dt });
  expect(move).not.toHaveBeenCalled();
});

test("a page dragged to another tree on the screen lands there", async () => {
  const move = vi.spyOn(api, "movePage").mockResolvedValue(pages[0]);
  const other = page("z", "Зета", "note:project", "a");
  const client = new QueryClient();
  render(<QueryClientProvider client={client}>
    <PageTree pages={[...pages, other]} roots={pages.filter((p) => !p.parent)} onCreate={vi.fn()} />
    <PageTree pages={[...pages, other]} roots={[other]} rootRef="note:project" onCreate={vi.fn()} />
  </QueryClientProvider>);
  const rows = screen.getAllByRole("treeitem");
  const beta = rows.find((r) => r.textContent?.includes("Бета"))!;
  const zeta = rows.find((r) => r.textContent?.includes("Зета"))!;
  const dt = { setData: vi.fn(), effectAllowed: "" };
  fireEvent.dragStart(beta, { dataTransfer: dt });
  fireEvent.dragOver(zeta, { dataTransfer: dt, clientY: 0 });
  await waitFor(() => expect(zeta.className).toMatch(/drop-/));
  fireEvent.drop(zeta, { dataTransfer: dt });
  await waitFor(() => expect(move).toHaveBeenCalled());
  expect(move.mock.calls[0][0].id).toBe("b");
  expect(move.mock.calls[0][0].parent).toMatch(/note:/);
});

test("the plus button starts a subpage", () => {
  const create = show();
  fireEvent.click(screen.getByRole("button", { name: "Подстраница: Бета" }));
  expect(create).toHaveBeenCalledWith("note:b");
});
