// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useLocation } from "../lib/navigation";
import { startAt, TestRoutes } from "../test/routes";
import { api } from "../api/client";
import type { Identity, Inbox } from "../api/types";
import InboxPage from "./InboxPage";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

function Where() {
  const l = useLocation();
  return <p>at {l.pathname}{l.hash}</p>;
}

function show(me: Identity, inbox: Inbox) {
  vi.spyOn(api, "me").mockResolvedValue(me);
  vi.spyOn(api, "notifications").mockResolvedValue(inbox);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  startAt("/inbox");
  render(<QueryClientProvider client={client}><TestRoutes routes={{ "/inbox": <InboxPage />, "/issue/:key": <Where /> }} /></QueryClientProvider>);
}

test("opening a notification marks it read and jumps to the comment", async () => {
  const read = vi.spyOn(api, "readNotifications").mockResolvedValue(undefined);
  show({ authenticated: true, mode: "oidc", username: "bob" }, {
    unread: 1,
    items: [{ id: 7, kind: "mentioned", issue: "WEB-5", ref: "c-1", actor: "alice", created_at: "2026-11-02T10:00:00Z", read: false, title: "Макет", project: "WEB", status: "todo" }],
  });
  fireEvent.click(await screen.findByRole("button", { name: /alice упомянул\(а\) вас в WEB-5 «Макет»/ }));
  await waitFor(() => expect(read).toHaveBeenCalledWith([7]));
  expect(await screen.findByText("at /issue/WEB-5#c-1")).toBeTruthy();
});

test("without a signed-in user the page explains why it is empty", async () => {
  show({ authenticated: true, mode: "none" }, { unread: 0, items: [] });
  expect(await screen.findByText(/для вошедших через Keycloak/)).toBeTruthy();
  expect(api.notifications).not.toHaveBeenCalled();
});
