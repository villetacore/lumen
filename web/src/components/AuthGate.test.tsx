// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { api, ApiError, UNAUTHORIZED_EVENT } from "../api/client";
import type { Identity } from "../api/types";
import AuthGate from "./AuthGate";

const out: Identity = { authenticated: false, mode: "oidc", login_url: "/auth/login", logout_url: "/auth/logout", password_login: true };
const noForm: Identity = { ...out, password_login: false };
const signedIn: Identity = { ...out, authenticated: true, name: "Алиса", username: "alice" };

// jsdom has no modal dialogs.
HTMLDialogElement.prototype.showModal = function () { this.open = true; };
HTMLDialogElement.prototype.close = function () { this.open = false; };

afterEach(() => { cleanup(); vi.restoreAllMocks(); window.history.replaceState({}, "", "/"); });

function show(me: Identity) {
  const spy = vi.spyOn(api, "me").mockResolvedValue(me);
  const go = vi.fn();
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><AuthGate go={go}><p>Рабочее место</p><textarea aria-label="Черновик" defaultValue="мой текст" /></AuthGate></QueryClientProvider>);
  return { spy, go };
}

function fill(login: string, password: string) {
  fireEvent.change(screen.getByLabelText("Логин"), { target: { value: login } });
  fireEvent.change(screen.getByLabelText("Пароль"), { target: { value: password } });
  fireEvent.click(screen.getByRole("button", { name: "Войти" }));
}

test("without a session it shows the sign-in window, not the workspace, and stays in Lumen", async () => {
  const { go } = show(out);
  await screen.findByText("Вход в Lumen");
  expect(screen.queryByText("Рабочее место")).toBeNull();
  expect(go).not.toHaveBeenCalled();
});

test("the form signs in and opens the workspace on the same page", async () => {
  window.history.replaceState({}, "", "/tracker/WEB/board");
  const { spy } = show(out);
  await screen.findByText("Вход в Lumen");
  const login = vi.spyOn(api, "passwordLogin").mockImplementation(async () => { spy.mockResolvedValue(signedIn); return { ok: true }; });
  fill(" alice ", "секрет");
  await screen.findByText("Рабочее место");
  expect(login).toHaveBeenCalledWith({ username: "alice", password: "секрет", otp: undefined });
  expect(window.location.pathname).toBe("/tracker/WEB/board");
});

test("a wrong password says so and clears the field", async () => {
  show(out);
  await screen.findByText("Вход в Lumen");
  vi.spyOn(api, "passwordLogin").mockRejectedValue(new ApiError(401, "invalid_credentials", "Неверный логин или пароль"));
  fill("alice", "не тот");
  await screen.findByText("Не удалось войти");
  expect(screen.getByText("Неверный логин или пароль")).toBeTruthy();
  expect((screen.getByLabelText("Пароль") as HTMLInputElement).value).toBe("");
  expect((screen.getByLabelText("Логин") as HTMLInputElement).value).toBe("alice");
  expect(screen.queryByText("Рабочее место")).toBeNull();
});

test("a second factor is passed along", async () => {
  const { spy } = show(out);
  await screen.findByText("Вход в Lumen");
  const login = vi.spyOn(api, "passwordLogin").mockImplementation(async () => { spy.mockResolvedValue(signedIn); return { ok: true }; });
  fireEvent.change(screen.getByLabelText("Код"), { target: { value: " 123456 " } });
  fill("alice", "секрет");
  await waitFor(() => expect(login).toHaveBeenCalledWith({ username: "alice", password: "секрет", otp: "123456" }));
});

test("a server without the form still goes to Keycloak and comes back", async () => {
  window.history.replaceState({}, "", "/tracker/WEB/board?q=a");
  const { go } = show(noForm);
  await waitFor(() => expect(go).toHaveBeenCalledWith("/auth/login?return_to=%2Ftracker%2FWEB%2Fboard%3Fq%3Da"));
  expect(screen.queryByText("Рабочее место")).toBeNull();
});

test("a signed-in user goes straight in", async () => {
  const { go } = show(signedIn);
  await screen.findByText("Рабочее место");
  expect(go).not.toHaveBeenCalled();
  expect(screen.queryByText("Вход в Lumen")).toBeNull();
});

test("a refused Keycloak page sign-in explains itself instead of looping", async () => {
  window.history.replaceState({}, "", "/?auth_error=forbidden");
  const { go } = show(noForm);
  await screen.findByText("Нет доступа к Lumen");
  expect(go).not.toHaveBeenCalled();
  expect(screen.getByRole("link", { name: "Войти под другой учётной записью" }).getAttribute("href")).toBe("/auth/logout");
});

test("a session ending mid-work opens the form over the page and keeps the draft", async () => {
  const { spy, go } = show(signedIn);
  const draft = await screen.findByLabelText("Черновик") as HTMLTextAreaElement;
  fireEvent.change(draft, { target: { value: "несохранённое" } });
  spy.mockResolvedValue(out);
  act(() => { window.dispatchEvent(new Event(UNAUTHORIZED_EVENT)); });
  await screen.findByText("Сессия закончилась");
  await screen.findByText("Войдите снова");
  expect(go).not.toHaveBeenCalled();
  expect(draft.isConnected).toBe(true);
  expect(draft.value).toBe("несохранённое");

  vi.spyOn(api, "passwordLogin").mockImplementation(async () => { spy.mockResolvedValue(signedIn); return { ok: true }; });
  fill("alice", "секрет");
  await waitFor(() => expect(screen.queryByText("Сессия закончилась")).toBeNull());
  expect(draft.isConnected).toBe(true);
  expect(draft.value).toBe("несохранённое");
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
