// @vitest-environment jsdom
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { api, UNAUTHORIZED_EVENT } from "../api/client";
import type { Identity } from "../api/types";
import AuthGate from "./AuthGate";

const out: Identity = { authenticated: false, mode: "oidc", login_url: "/auth/login", logout_url: "/auth/logout" };
const signedIn: Identity = { ...out, authenticated: true, name: "Алиса", username: "alice" };

afterEach(() => { cleanup(); vi.restoreAllMocks(); window.history.replaceState({}, "", "/"); });

function show(me: Identity) {
  const spy = vi.spyOn(api, "me").mockResolvedValue(me);
  const go = vi.fn();
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><AuthGate go={go}><p>Рабочее место</p></AuthGate></QueryClientProvider>);
  return { spy, go };
}

test("goes to Keycloak without showing the workspace, and comes back to the same page", async () => {
  window.history.replaceState({}, "", "/tracker/WEB/board?q=a");
  const { go } = show(out);
  await waitFor(() => expect(go).toHaveBeenCalledWith("/auth/login?return_to=%2Ftracker%2FWEB%2Fboard%3Fq%3Da"));
  expect(screen.queryByText("Рабочее место")).toBeNull();
});

test("a signed-in user goes straight in", async () => {
  const { go } = show(signedIn);
  await screen.findByText("Рабочее место");
  expect(go).not.toHaveBeenCalled();
});

test("a refused login explains itself instead of bouncing back to Keycloak", async () => {
  window.history.replaceState({}, "", "/?auth_error=forbidden");
  const { go } = show(out);
  await screen.findByText("Нет доступа к Lumen");
  expect(go).not.toHaveBeenCalled();
  expect(screen.getByRole("link", { name: "Войти под другой учётной записью" }).getAttribute("href")).toBe("/auth/logout");
});

test("a session ending mid-work keeps the page and offers a new tab", async () => {
  const { spy, go } = show(signedIn);
  await screen.findByText("Рабочее место");
  spy.mockResolvedValue(out);
  act(() => { window.dispatchEvent(new Event(UNAUTHORIZED_EVENT)); });
  await screen.findByText("Сессия закончилась");
  expect(screen.getByText("Рабочее место")).toBeTruthy();
  expect(go).not.toHaveBeenCalled();
  expect(screen.getByRole("link", { name: /новой вкладке/ }).getAttribute("target")).toBe("_blank");
});

test("local mode needs no sign-in", async () => {
  const { go } = show({ authenticated: true, mode: "none", name: "локальный режим" });
  await screen.findByText("Рабочее место");
  expect(go).not.toHaveBeenCalled();
});

test("a wrong static token asks for one", async () => {
  show({ authenticated: false, mode: "token" });
  await screen.findByText("Нужен токен доступа");
  expect(screen.queryByText("Рабочее место")).toBeNull();
});
