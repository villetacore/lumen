import type {
  Agenda,
  Identity,
  Integration,
  IntegrationKind,
  ListResponse,
  PasswordItem,
  PasswordKind,
  PasswordStore,
  Note,
  NoteSummary,
  SearchHit,
  Stats,
  SyncResult,
  TagCount,
  Task,
  TaskFilter,
  TaskPatch,
} from "./types";

const BASE = "/api/v1";
const TOKEN_KEY = "lumen.token";

/**
 * The token is only needed when lumend runs with --token (a shared instance).
 * It can arrive once as ?token=… — we keep it locally and drop it from the URL
 * so it does not linger in history or get copied into a shared link.
 */
function readToken(): string {
  try {
    const fromUrl = new URLSearchParams(window.location.search).get("token");
    if (fromUrl) {
      localStorage.setItem(TOKEN_KEY, fromUrl);
      const url = new URL(window.location.href);
      url.searchParams.delete("token");
      window.history.replaceState({}, "", url.toString());
      return fromUrl;
    }
    return localStorage.getItem(TOKEN_KEY) ?? "";
  } catch {
    return "";
  }
}

const token = readToken();

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
  ) {
    super(message);
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(BASE + path, {
    ...init,
    // Сессия single sign-on живёт в HttpOnly-cookie.
    credentials: "same-origin",
    headers: {
      "Content-Type": "application/json",
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...(init?.headers ?? {}),
    },
  });
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  const body = text ? JSON.parse(text) : null;
  if (!res.ok) {
    const err = body?.error ?? {};
    throw new ApiError(res.status, err.code ?? "unknown", err.message ?? res.statusText);
  }
  return body as T;
}

function query(params: Record<string, unknown>): string {
  const sp = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === null || value === "") continue;
    sp.set(key, Array.isArray(value) ? value.join(",") : String(value));
  }
  const s = sp.toString();
  return s ? `?${s}` : "";
}

export const api = {
  stats: () => request<Stats>("/stats"),
  reindex: () => request<{ notes: number }>("/reindex", { method: "POST" }),

  tasks: (filter: TaskFilter = {}) =>
    request<ListResponse<Task>>("/tasks" + query({ ...filter })),
  task: (id: string) => request<Task>(`/tasks/${encodeURIComponent(id)}`),
  patchTask: (id: string, patch: TaskPatch) =>
    request<Task>(`/tasks/${encodeURIComponent(id)}`, {
      method: "PATCH",
      body: JSON.stringify(patch),
    }),
  createTask: (input: { note?: string; text: string; section?: string; due?: string }) =>
    request<Task>("/tasks", { method: "POST", body: JSON.stringify(input) }),
  deleteTask: (id: string) =>
    request<void>(`/tasks/${encodeURIComponent(id)}`, { method: "DELETE" }),

  notes: (params: { type?: string; tag?: string[]; prefix?: string; q?: string } = {}) =>
    request<ListResponse<NoteSummary>>("/notes" + query(params)),
  note: (idOrPath: string) => request<Note>(`/notes/${encodePath(idOrPath)}`),
  putNoteBody: (idOrPath: string, body: string, revision: number) =>
    request<Note>(`/notes/${encodePath(idOrPath)}`, {
      method: "PUT",
      body: JSON.stringify({ body, revision }),
    }),
  backlinks: (idOrPath: string) =>
    request<ListResponse<NoteSummary>>(`/backlinks/${encodePath(idOrPath)}`),

  calendar: (from: string, to: string) =>
    request<Agenda>("/calendar" + query({ from, to })),
  moveEvent: (ref: string, date: string, field: "scheduled" | "due" = "scheduled") =>
    request<Task>("/calendar/move", {
      method: "POST",
      body: JSON.stringify({ ref, date, field }),
    }),

  me: () => request<Identity>("/me"),

  passwordStores: () => request<ListResponse<PasswordStore>>("/passwords/stores"),
  passwordKinds: () => request<ListResponse<PasswordKind>>("/passwords/kinds"),
  addPasswordStore: (input: { kind: string; name: string; values: Record<string, string> }) =>
    request<PasswordStore>("/passwords/stores", { method: "POST", body: JSON.stringify(input) }),
  deletePasswordStore: (id: string) =>
    request<void>(`/passwords/stores/${encodeURIComponent(id)}`, { method: "DELETE" }),
  unlockPasswordStore: (id: string, passphrase: string) =>
    request<PasswordStore>(`/passwords/stores/${encodeURIComponent(id)}/unlock`, {
      method: "POST",
      body: JSON.stringify({ passphrase }),
    }),
  lockPasswordStore: (id: string) =>
    request<void>(`/passwords/stores/${encodeURIComponent(id)}/lock`, { method: "POST" }),
  lockAllPasswordStores: () => request<void>("/passwords/lock", { method: "POST" }),
  passwordItems: (id: string, q = "") =>
    request<ListResponse<PasswordItem>>(
      `/passwords/stores/${encodeURIComponent(id)}/items` + (q ? `?q=${encodeURIComponent(q)}` : ""),
    ),
  revealPassword: (id: string, item: string, field: string) =>
    request<{ field: string; value: string }>(
      `/passwords/stores/${encodeURIComponent(id)}/reveal`,
      { method: "POST", body: JSON.stringify({ item, field }) },
    ),

  integrations: () => request<ListResponse<Integration>>("/integrations"),
  integrationKinds: () => request<ListResponse<IntegrationKind>>("/integrations/kinds"),
  addIntegration: (input: {
    kind: string;
    name: string;
    interval?: string;
    folder?: string;
    values: Record<string, string>;
  }) => request<Integration>("/integrations", { method: "POST", body: JSON.stringify(input) }),
  patchIntegration: (
    id: string,
    patch: { enabled?: boolean; name?: string; interval?: string; values?: Record<string, string> },
  ) =>
    request<Integration>(`/integrations/${encodeURIComponent(id)}`, {
      method: "PATCH",
      body: JSON.stringify(patch),
    }),
  deleteIntegration: (id: string) =>
    request<void>(`/integrations/${encodeURIComponent(id)}`, { method: "DELETE" }),
  syncIntegration: (id: string) =>
    request<SyncResult>(`/integrations/${encodeURIComponent(id)}/sync`, { method: "POST" }),

  search: (q: string, limit = 30) =>
    request<ListResponse<SearchHit>>("/search" + query({ q, limit })),
  tags: () => request<ListResponse<TagCount>>("/tags"),
  daily: (date: string, create = false) =>
    request<Note>(`/daily/${date}` + query({ create })),
};

// Note paths contain slashes; keep them readable but escape each segment.
function encodePath(p: string): string {
  return p.split("/").map(encodeURIComponent).join("/");
}

// subscribeEvents wires the server's SSE stream to a callback.
export function subscribeEvents(onEvent: () => void): () => void {
  // EventSource cannot set headers, so the token goes in the query string.
  const url = BASE + "/events" + (token ? `?token=${encodeURIComponent(token)}` : "");
  const es = new EventSource(url);
  const handler = () => onEvent();
  for (const type of [
    "note.changed",
    "note.deleted",
    "task.changed",
    "reindex.finished",
    "integration.synced",
  ]) {
    es.addEventListener(type, handler);
  }
  return () => es.close();
}
