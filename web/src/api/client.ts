import type {
  Agenda,
  Identity,
  Inbox,
  Integration,
  IntegrationKind,
  Issue,
  IssueComment,
  IssueEvent,
  IssueFilter,
  IssueInput,
  IssuePatch,
  Person,
  PersonGroup,
  Proposal,
  ProjectAccess,
  WorkspaceRole,
  Sprint,
  SprintInput,
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
  TrackerProject,
} from "./types";

const BASE = "/api/v1";
const TOKEN_KEY = "lumen.token";

/** Fired on a 401, so the entrance can find out the session has ended. */
export const UNAUTHORIZED_EVENT = "lumen:unauthorized";

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
    if (res.status === 401) window.dispatchEvent(new Event(UNAUTHORIZED_EVENT));
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
  /** Signs in with Lumen's own form; the server checks the password with Keycloak. */
  passwordLogin: async (input: { username: string; password: string; otp?: string }): Promise<{ ok: boolean }> => {
    const res = await fetch("/auth/password", {
      method: "POST", credentials: "same-origin",
      headers: { "Content-Type": "application/json" }, body: JSON.stringify(input),
    });
    const body = await res.json().catch(() => null);
    if (!res.ok) throw new ApiError(res.status, body?.error?.code ?? "unknown", body?.error?.message ?? res.statusText);
    return body;
  },

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
  createTask: (input: {
    note?: string;
    text: string;
    section?: string;
    due?: string;
    scheduled?: string;
    priority?: string;
    project?: string;
  }) =>
    request<Task>("/tasks", { method: "POST", body: JSON.stringify(input) }),
  deleteTask: (id: string) =>
    request<void>(`/tasks/${encodeURIComponent(id)}`, { method: "DELETE" }),

  notes: (params: { parent?: string; content_kind?: string; type?: string; tag?: string[]; prefix?: string; q?: string; limit?: number } = {}) =>
    request<ListResponse<NoteSummary>>("/notes" + query(params)),
  note: (idOrPath: string) => request<Note>(`/notes/${encodePath(idOrPath)}`).then(normNote),
  putNoteBody: (idOrPath: string, body: string, revision: number) =>
    request<Note>(`/notes/${encodePath(idOrPath)}`, {
      method: "PUT",
      body: JSON.stringify({ body, revision }),
    }).then(normNote),
  createNote: (input: { path?: string; parent?: string; content_kind?: string; title?: string; type?: string; tags?: string[]; body?: string }) =>
    request<Note>("/notes", { method: "POST", body: JSON.stringify(input) }).then(normNote),
  patchNote: (
    idOrPath: string,
    patch: { title?: string; type?: string; tags?: string[]; revision?: number },
  ) =>
    request<Note>(`/notes/${encodePath(idOrPath)}`, {
      method: "PATCH",
      body: JSON.stringify(patch),
    }).then(normNote),
  deleteNote: (idOrPath: string) =>
    request<void>(`/notes/${encodePath(idOrPath)}`, { method: "DELETE" }),
  backlinks: (idOrPath: string) =>
    request<ListResponse<NoteSummary>>(`/backlinks/${encodePath(idOrPath)}`),

  trackerProjects: () => request<ListResponse<TrackerProject>>("/tracker/projects"),
  trackerProject: (key: string) => request<TrackerProject>(`/tracker/projects/${encodeURIComponent(key)}`),
  createTrackerProject: (input: { key: string; title?: string; note?: string }) =>
    request<TrackerProject>("/tracker/projects", { method: "POST", body: JSON.stringify(input) }),
  issues: (filter: IssueFilter = {}) => request<ListResponse<Issue>>("/issues" + query({ ...filter })),
  issue: (key: string) => request<Issue>(`/issues/${encodeURIComponent(key)}`),
  createIssue: (input: IssueInput) =>
    request<Issue>("/issues", { method: "POST", body: JSON.stringify(input) }),
  patchIssue: (key: string, patch: IssuePatch) =>
    request<Issue>(`/issues/${encodeURIComponent(key)}`, { method: "PATCH", body: JSON.stringify(patch) }),
  moveIssue: (key: string, move: { status?: string; before?: string; after?: string; sprint?: string; revision?: number }) =>
    request<Issue>(`/issues/${encodeURIComponent(key)}/move`, { method: "POST", body: JSON.stringify(move) }),
  issueHistory: (key: string) => request<ListResponse<IssueEvent>>(`/issues/${encodeURIComponent(key)}/history`),
  addComment: (key: string, body: string) =>
    request<IssueComment>(`/issues/${encodeURIComponent(key)}/comments`, { method: "POST", body: JSON.stringify({ body }) }),
  deleteIssue: (key: string) => request<void>(`/issues/${encodeURIComponent(key)}`, { method: "DELETE" }),
  sprints: (project: string) => request<ListResponse<Sprint>>(`/tracker/projects/${encodeURIComponent(project)}/sprints`),
  createSprint: (project: string, input: SprintInput = {}) =>
    request<Sprint>(`/tracker/projects/${encodeURIComponent(project)}/sprints`, { method: "POST", body: JSON.stringify(input) }),
  patchSprint: (id: string, input: SprintInput) =>
    request<Sprint>(`/sprints/${encodeURIComponent(id)}`, { method: "PATCH", body: JSON.stringify(input) }),
  startSprint: (id: string, input: SprintInput = {}) =>
    request<Sprint>(`/sprints/${encodeURIComponent(id)}/start`, { method: "POST", body: JSON.stringify(input) }),
  completeSprint: (id: string, moveTo = "") =>
    request<{ sprint: Sprint; moved: number }>(`/sprints/${encodeURIComponent(id)}/complete`, { method: "POST", body: JSON.stringify({ move_to: moveTo }) }),

  calendar: (from: string, to: string) =>
    request<Agenda>("/calendar" + query({ from, to })),
  moveEvent: (ref: string, date: string, field: "scheduled" | "due" = "scheduled") =>
    request<Task>("/calendar/move", {
      method: "POST",
      body: JSON.stringify({ ref, date, field }),
    }),

  me: () => request<Identity>("/me"),
  notifications: (unread = false) => request<Inbox>("/notifications" + query({ unread: unread || undefined })),
  readNotifications: (ids?: number[]) =>
    request<void>("/notifications/read", { method: "POST", body: JSON.stringify(ids ? { ids } : {}) }),
  proposals: (pending = false) => request<ListResponse<Proposal>>("/proposals" + query({ pending: pending || undefined })),
  approveProposal: (id: string) => request<Proposal>(`/proposals/${encodeURIComponent(id)}/approve`, { method: "POST" }),
  rejectProposal: (id: string) => request<Proposal>(`/proposals/${encodeURIComponent(id)}/reject`, { method: "POST" }),
  users: (q = "") => request<ListResponse<Person>>("/users" + query({ q })),
  groups: () => request<ListResponse<PersonGroup>>("/groups"),
  projectAccess: (key: string) => request<ProjectAccess>(`/tracker/projects/${encodeURIComponent(key)}/members`),
  setMember: (key: string, member: { kind: "user" | "group"; name: string; role: WorkspaceRole }) =>
    request<ProjectAccess>(`/tracker/projects/${encodeURIComponent(key)}/members`, { method: "PUT", body: JSON.stringify(member) }),
  removeMember: (key: string, kind: "user" | "group", name: string) =>
    request<ProjectAccess>(`/tracker/projects/${encodeURIComponent(key)}/members/${kind}/${encodeURIComponent(name)}`, { method: "DELETE" }),

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
  promoteTask: (id: string, project: string) =>
    request<Issue>(`/tasks/${encodeURIComponent(id)}/promote`, { method: "POST", body: JSON.stringify({ project }) }),
  patchPasswordStore: (id: string, patch: { name?: string; values?: Record<string, string> }) =>
    request<PasswordStore>(`/passwords/stores/${encodeURIComponent(id)}`, { method: "PATCH", body: JSON.stringify(patch) }),
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
    request<Note>(`/daily/${date}` + query({ create })).then(normNote),
};

// The server sends null for an empty list on a single note; the UI expects arrays.
function normNote(n: Note): Note {
  return { ...n, tags: n.tags ?? [], tasks: n.tasks ?? [], links: n.links ?? [] };
}

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
    "issue.changed",
    "issue.deleted",
    "sprint.changed",
    "notification",
    "proposal",
    "reindex.finished",
    "integration.synced",
  ]) {
    es.addEventListener(type, handler);
  }
  return () => es.close();
}
