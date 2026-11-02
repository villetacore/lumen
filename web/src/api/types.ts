// Mirrors the DTOs in server/internal/domain. Keep the two in step.

export type TaskStatus = "todo" | "doing" | "done" | "cancelled" | "deferred";
export type Priority = "A" | "B" | "C" | "D" | "";
export type NoteType = "note" | "project" | "area" | "daily" | "meeting" | "event" | "issue" | "sprint";
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
  parent?: string;
  content_kind?: string;
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
  /** Workspace role Lumen applies (ADR-0012). */
  role?: WorkspaceRole;
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

// Tracker (spec/FORMAT.md §7, server/internal/domain/issue.go).

export type StatusCategory = "todo" | "in_progress" | "done";

export interface WorkflowStatus {
  id: string;
  name: string;
  category: StatusCategory;
  wip?: number;
}

export interface TrackerProject {
  key: string;
  title: string;
  note_id: string;
  path: string;
  workflow: WorkflowStatus[];
  issue_types: string[];
  transitions?: Record<string, string[]>;
  issue_seq: number;
  revision: number;
}

export interface IssueLink {
  type: string;
  target: string;
}

export interface IssueComment {
  id: string;
  author?: string;
  at: string;
  body: string;
  line: number;
}

export interface Issue {
  key: string;
  project: string;
  path: string;
  title: string;
  issue_type: string;
  status: string;
  status_category?: StatusCategory;
  priority?: string;
  assignee?: string;
  reporter?: string;
  watchers?: string[];
  labels?: string[];
  epic?: string;
  parent?: string;
  sprint?: string;
  start: string | null;
  due: string | null;
  estimate?: string;
  points?: number;
  rank?: string;
  links?: IssueLink[];
  created?: string;
  updated?: string;
  revision: number;
  description?: string;
  comments?: IssueComment[];
  checklist?: Task[];
}

export interface IssueFilter {
  project?: string;
  status?: string[];
  category?: StatusCategory | "";
  assignee?: string;
  watcher?: string;
  type?: string;
  label?: string[];
  sprint?: string;
  epic?: string;
  q?: string;
  limit?: number;
}

export interface IssueInput {
  project: string;
  title: string;
  issue_type?: string;
  status?: string;
  priority?: string;
  assignee?: string;
  labels?: string[];
  epic?: string;
  parent?: string;
  sprint?: string;
  start?: string;
  due?: string;
  estimate?: string;
  points?: number;
  description?: string;
}

export type IssuePatch = Partial<Omit<IssueInput, "project">> & { watchers?: string[]; revision?: number };

export type SprintState = "planned" | "active" | "closed";

export interface Sprint {
  id: string;
  project: string;
  path: string;
  name: string;
  goal?: string;
  state: SprintState;
  start: string | null;
  end: string | null;
  completed: string | null;
  revision: number;
}

export type SprintInput = { name?: string; goal?: string; start?: string; end?: string };

export type WorkspaceRole = "viewer" | "member" | "admin";

export interface Person {
  username: string;
  name: string;
  email?: string;
  groups: string[];
  role: WorkspaceRole;
  first_seen: string;
  last_seen: string;
}

export interface PersonGroup {
  name: string;
  users: number;
}

export interface ProjectMember {
  project: string;
  kind: "user" | "group";
  name: string;
  role: WorkspaceRole;
  added_at: string;
  added_by?: string;
}

export interface ProjectAccess {
  open: boolean;
  my_role: WorkspaceRole | "none";
  items: ProjectMember[];
}

export type NotificationKind = "assigned" | "mentioned" | "commented" | "status";

export interface AppNotification {
  id: number;
  kind: NotificationKind;
  issue: string;
  ref?: string;
  actor?: string;
  created_at: string;
  read: boolean;
  title: string;
  project: string;
  status: string;
}

export interface Inbox {
  items: AppNotification[];
  unread: number;
}

export interface IssueEvent {
  id: number;
  issue: string;
  project?: string;
  kind: "created" | "changed" | "deleted";
  field?: string;
  old?: string;
  new?: string;
  actor?: string;
  source: "lumen" | "external";
  at: string;
}

export type ProposalState = "pending" | "applying" | "applied" | "rejected" | "expired" | "stale" | "failed";

/** A change an AI client asked for through MCP, waiting for the person (ADR-0013). */
export interface Proposal {
  id: string;
  requester: string;
  kind: "create_issue" | "update_issue" | "change_status" | "add_comment";
  target?: string;
  args: Record<string, unknown>;
  preview: string;
  state: ProposalState;
  result?: string;
  error?: string;
  request_id?: string;
  created_at: string;
  expires_at: string;
  decided_at?: string;
}
