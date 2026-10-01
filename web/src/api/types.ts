// Mirrors the DTOs in server/internal/domain. Keep the two in step.

export type TaskStatus = "todo" | "doing" | "done" | "cancelled" | "deferred";
export type Priority = "A" | "B" | "C" | "D" | "";
export type NoteType = "note" | "project" | "area" | "daily" | "meeting" | "event";
export type EventSource = "note" | "task" | "daily" | "ics";

export interface Task {
  id: string;
  block_id?: string;
  note_id: string;
  note_path: string;
  line: number;
  indent: number;
  status: TaskStatus;
  marker: string;
  text: string;
  tags?: string[];
  due: string | null;
  scheduled: string | null;
  start: string | null;
  done: string | null;
  priority?: Priority;
  estimate?: string;
  project?: string;
  repeat?: string;
  fields?: Record<string, string>;
}

export interface NoteSummary {
  id: string;
  path: string;
  title: string;
  type: NoteType;
  tags: string[];
  tasks: number;
  open_tasks: number;
  revision: number;
  updated?: string;
}

export interface Note extends Omit<NoteSummary, "tasks" | "open_tasks"> {
  frontmatter?: Record<string, unknown>;
  body: string;
  links?: WikiLink[];
  tasks?: Task[];
  hash: string;
  size: number;
}

export interface WikiLink {
  target: string;
  heading: string;
  alias: string;
  kind: "wiki" | "markdown";
  line: number;
}

export interface CalendarEvent {
  id: string;
  source: EventSource;
  ref_id: string;
  path?: string;
  title: string;
  start: string;
  end: string;
  all_day: boolean;
  status?: TaskStatus;
  calendar?: string;
  location?: string;
  url?: string;
  integration?: string;
}

export interface Agenda {
  from: string;
  to: string;
  events: CalendarEvent[];
}

export interface SearchHit {
  note_id: string;
  path: string;
  title: string;
  snippet: string;
  line: number;
  score: number;
}

export interface TagCount {
  tag: string;
  count: number;
}

export interface Stats {
  notes: number;
  tasks: number;
  open_tasks: number;
  tags: number;
  last_indexed: string;
  index_took_ms: number;
  vault_path: string;
  encrypted: boolean;
  ui_embedded: boolean;
  auth_required: boolean;
}

export interface Integration {
  id: string;
  kind: string;
  title: string;
  name: string;
  enabled: boolean;
  interval: string;
  folder?: string;
  values: Record<string, string>;
  last_sync?: string;
  next_sync?: string;
  last_error?: string;
  last_items: number;
  running: boolean;
}

export type FieldType = "text" | "url" | "secret" | "bool" | "duration";

export interface ConfigField {
  key: string;
  label: string;
  type: FieldType;
  required: boolean;
  placeholder?: string;
  help?: string;
}

export interface IntegrationKind {
  kind: string;
  title: string;
  fields: ConfigField[];
}

export interface SyncResult {
  id: string;
  items: number;
  created: number;
  updated: number;
  events: number;
  skipped: number;
  took_ms: number;
  error?: string;
}

export interface Identity {
  authenticated: boolean;
  mode: "none" | "token" | "oidc";
  subject?: string;
  name?: string;
  email?: string;
  username?: string;
  roles?: string[];
  login_url?: string;
  logout_url?: string;
}

export interface PasswordStore {
  id: string;
  kind: string;
  title: string;
  name: string;
  values: Record<string, string>;
  unlocked: boolean;
  needs_passphrase: boolean;
  locks_at?: string;
  last_error?: string;
}

export interface PasswordItem {
  id: string;
  name: string;
  username?: string;
  uri?: string;
  folder?: string;
  fields: string[];
}

export interface PasswordKind {
  kind: string;
  title: string;
  fields: ConfigField[];
}

export interface ListResponse<T> {
  items: T[];
  total: number;
  limit: number;
  offset: number;
}

export type ViewName =
  | "today"
  | "inbox"
  | "week"
  | "overdue"
  | "upcoming"
  | "someday"
  | "done"
  | "all";

export interface TaskFilter {
  view?: ViewName;
  status?: TaskStatus[];
  tag?: string[];
  project?: string;
  prefix?: string;
  q?: string;
  sort?: string[];
  limit?: number;
}

export interface TaskPatch {
  status?: TaskStatus;
  text?: string;
  due?: string;
  scheduled?: string;
  priority?: string;
  estimate?: string;
  project?: string;
}

export interface ServerEvent {
  type: string;
  path?: string;
  id?: string;
  status?: string;
  revision?: number;
}
