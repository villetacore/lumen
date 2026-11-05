// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { api } from "../api/client";
import ConnectPage from "./ConnectPage";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

function show() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><ConnectPage /></QueryClientProvider>);
}

test("shows the address and disconnects a client", async () => {
  vi.spyOn(api, "mcpConnection").mockResolvedValue({ url: "https://lumen.test/mcp", auth: "oauth" });
  vi.spyOn(api, "mcpConnections").mockResolvedValue({ items: [{ id: "g1", client_name: "Claude", redirect_host: "claude.ai",
    connected_at: "2026-11-04T10:00:00Z", expires_at: "2026-12-04T10:00:00Z" }] });
  const off = vi.spyOn(api, "mcpDisconnect").mockResolvedValue(undefined);
  show();
  expect(await screen.findByText("https://lumen.test/mcp")).toBeTruthy();
  expect(screen.getByText(/claude mcp add .* lumen https:\/\/lumen.test\/mcp$/)).toBeTruthy();
  fireEvent.click(await screen.findByLabelText("Отключить Claude"));
  await waitFor(() => expect(off).toHaveBeenCalledWith("g1", expect.anything()));
});

test("a token server explains the header and lists no connections", async () => {
  vi.spyOn(api, "mcpConnection").mockResolvedValue({ url: "https://lumen.test/mcp", auth: "token" });
  const list = vi.spyOn(api, "mcpConnections");
  show();
  expect(await screen.findByText(/--header "Authorization: Bearer <токен>"/)).toBeTruthy();
  expect(screen.queryByText(/Подключённые клиенты/)).toBeNull();
  expect(list).not.toHaveBeenCalled();
});
