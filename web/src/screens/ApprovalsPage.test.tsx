// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { api } from "../api/client";
import type { Proposal } from "../api/types";
import ApprovalsPage from "./ApprovalsPage";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

const base: Proposal = {
  id: "p-1", requester: "alice", kind: "change_status", target: "WEB-1", args: {}, state: "pending",
  preview: "WEB-1 «Макет»\nСтатус: К работе → В работе", created_at: "2026-11-02T10:00:00Z", expires_at: "2026-11-03T10:00:00Z",
};

function show(items: Proposal[]) {
  vi.spyOn(api, "me").mockResolvedValue({ authenticated: true, mode: "oidc", username: "alice" });
  vi.spyOn(api, "proposals").mockResolvedValue({ items, total: items.length, limit: 0, offset: 0 });
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><ApprovalsPage /></QueryClientProvider>);
}

test("a pending proposal shows its exact change and is applied on confirmation", async () => {
  const approve = vi.spyOn(api, "approveProposal").mockResolvedValue({ ...base, state: "applied", result: "WEB-1" });
  show([base]);
  expect(await screen.findByText(/Статус: К работе → В работе/)).toBeTruthy();
  fireEvent.click(screen.getByText("Подтвердить"));
  await waitFor(() => expect(approve).toHaveBeenCalledWith("p-1", expect.anything()));
});

test("a stale proposal explains why it was not applied and offers no buttons", async () => {
  show([{ ...base, state: "stale", error: "статус уже done; попросите заново", decided_at: "2026-11-02T11:00:00Z" }]);
  expect(await screen.findByText("Не применено: данные изменились")).toBeTruthy();
  expect(screen.queryByText("Подтвердить")).toBeNull();
  expect(screen.getByText("Нечего подтверждать")).toBeTruthy();
});
