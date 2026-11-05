// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { startAt, TestRoutes } from "../test/routes";
import { api, ApiError } from "../api/client";
import type { AutomationRule, TrackerProject } from "../api/types";
import AutomationsPage from "./AutomationsPage";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

// jsdom has no modal dialogs.
HTMLDialogElement.prototype.showModal = function () { this.setAttribute("open", ""); };
HTMLDialogElement.prototype.close = function () { this.removeAttribute("open"); };

const page = { limit: 0, offset: 0 };

const project = {
  key: "WEB", title: "Сайт", note_id: "n", path: "20-projects/web.md", issue_types: ["task", "bug"], issue_seq: 0, revision: 1,
  workflow: [{ id: "todo", name: "К работе", category: "todo" }, { id: "done", name: "Готово", category: "done" }],
} as TrackerProject;

const rule: AutomationRule = {
  id: "a-1", project: "WEB", enabled: true, owner: "ann", revision: 3, created_at: "2026-11-05T10:00:00Z", updated_at: "2026-11-05T10:00:00Z",
  name: "Готовые", trigger: { type: "status_changed", to: "done" }, conditions: [], actions: [{ type: "add_label", value: "итог" }],
};

function show(items: AutomationRule[], canEdit = true) {
  vi.spyOn(api, "trackerProject").mockResolvedValue(project);
  vi.spyOn(api, "trackerProjects").mockResolvedValue({ items: [project], total: 1, ...page });
  vi.spyOn(api, "users").mockResolvedValue({ items: [], total: 0, ...page });
  vi.spyOn(api, "automations").mockResolvedValue({ items, total: items.length, can_edit: canEdit, ...page });
  vi.spyOn(api, "automationRuns").mockResolvedValue({ items: [
    { id: 1, rule_id: "a-1", rule_name: "Готовые", project: "WEB", occurrence: "event:1", issue: "WEB-7", status: "failed", error: "действие 1: нельзя", actions: 0, matched: 0, started_at: "2026-11-05T10:00:00Z" },
  ], total: 1, ...page });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  startAt("/tracker/WEB/automations");
  render(<QueryClientProvider client={client}><TestRoutes routes={{ "/tracker/:key/automations": <AutomationsPage /> }} /></QueryClientProvider>);
  return client;
}

test("rules read as sentences, and the log shows what failed", async () => {
  show([rule]);
  expect(await screen.findByText("Когда статус меняется на «Готово»: метка +итог.")).toBeTruthy();
  expect(await screen.findByText("действие 1: нельзя")).toBeTruthy();
  expect(screen.getByText("WEB-7")).toBeTruthy();
});

test("a preset fills the form and saves nothing by itself", async () => {
  const create = vi.spyOn(api, "createAutomation").mockResolvedValue(rule);
  show([]);
  fireEvent.click(await screen.findByRole("button", { name: "Сообщать автору о завершении" }));
  const name = await screen.findByLabelText(/Название/) as HTMLInputElement;
  expect(name.value).toBe("Сообщить автору, что готово");
  expect(create).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Сохранить" }));
  await waitFor(() => expect(create).toHaveBeenCalled());
  expect(create.mock.calls[0][1].trigger).toEqual({ type: "status_changed", to: "done" });
});

test("a draft survives a refetch, and a conflict keeps it and offers to save over", async () => {
  const client = show([rule]);
  fireEvent.click(await screen.findByRole("button", { name: "Изменить" }));
  const name = await screen.findByLabelText(/Название/) as HTMLInputElement;
  fireEvent.change(name, { target: { value: "Мой черновик" } });

  // Someone else saved it; the list refreshes under the open form.
  vi.mocked(api.automations).mockResolvedValue({ items: [{ ...rule, name: "Чужое имя", revision: 4 }], total: 1, can_edit: true, ...page });
  await client.invalidateQueries();
  await waitFor(() => expect(screen.getByText("Чужое имя")).toBeTruthy());
  expect((screen.getByLabelText(/Название/) as HTMLInputElement).value).toBe("Мой черновик");

  const patch = vi.spyOn(api, "patchAutomation").mockRejectedValueOnce(new ApiError(409, "conflict", "правило изменили"));
  fireEvent.click(screen.getByRole("button", { name: "Сохранить" }));
  expect(await screen.findByText("Правило изменили, пока вы его редактировали")).toBeTruthy();
  expect(patch.mock.calls[0][1].revision).toBe(3);
  expect((screen.getByLabelText(/Название/) as HTMLInputElement).value).toBe("Мой черновик");

  patch.mockResolvedValue({ ...rule, revision: 5 });
  fireEvent.click(screen.getByRole("button", { name: "Сохранить поверх" }));
  await waitFor(() => expect(patch).toHaveBeenCalledTimes(2));
  expect(patch.mock.calls[1][1].revision).toBe(4);
  expect(patch.mock.calls[1][1].definition?.name).toBe("Мой черновик");
});

test("a reader sees rules without controls", async () => {
  show([rule], false);
  expect(await screen.findByText("Менять правила могут администраторы проекта.")).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Изменить" })).toBeNull();
  expect((screen.getByRole("switch", { name: "Включено" }) as HTMLInputElement).disabled).toBe(true);
});
