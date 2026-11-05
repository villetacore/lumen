// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { api, ApiError } from "../api/client";
import ConsentPage from "./ConsentPage";

beforeEach(() => {
  window.history.pushState({}, "", "/connect/authorize?client_id=c1&redirect_uri=https%3A%2F%2Fclaude.ai%2Fcb&state=s1&response_type=code&code_challenge=abc&code_challenge_method=S256");
  vi.spyOn(api, "me").mockResolvedValue({ authenticated: true, mode: "oidc", username: "alice", name: "Алиса" });
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); });

function show(go = vi.fn()) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><ConsentPage go={go} /></QueryClientProvider>);
  return go;
}

test("names the client and where the answer goes, then returns on approval", async () => {
  vi.spyOn(api, "consentInfo").mockResolvedValue({ client_name: "Claude", redirect_host: "claude.ai" });
  const send = vi.spyOn(api, "consent").mockResolvedValue({ redirect_to: "https://claude.ai/cb?code=x" });
  const go = show();
  expect(await screen.findByText("Claude")).toBeTruthy();
  expect(screen.getByText("claude.ai")).toBeTruthy();
  fireEvent.click(screen.getByText("Разрешить"));
  await waitFor(() => expect(go).toHaveBeenCalledWith("https://claude.ai/cb?code=x"));
  expect(send.mock.calls[0][0]).toMatchObject({ client_id: "c1", state: "s1", code_challenge: "abc", approve: true });
});

test("refusing is sent as a refusal", async () => {
  vi.spyOn(api, "consentInfo").mockResolvedValue({ client_name: "Claude", redirect_host: "claude.ai" });
  const send = vi.spyOn(api, "consent").mockResolvedValue({ redirect_to: "https://claude.ai/cb?error=access_denied" });
  show();
  fireEvent.click(await screen.findByText("Отказать"));
  await waitFor(() => expect(send.mock.calls[0]?.[0]).toMatchObject({ approve: false }));
});

test("a request the server rejects offers no buttons", async () => {
  vi.spyOn(api, "consentInfo").mockRejectedValue(new ApiError(400, "invalid_redirect_uri", "этот адрес возврата не указан при регистрации клиента"));
  show();
  expect(await screen.findByText("Запрос отклонён")).toBeTruthy();
  expect(screen.queryByText("Разрешить")).toBeNull();
});
