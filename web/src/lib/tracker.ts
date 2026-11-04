import { useEffect } from "react";
import { useBeforeUnload, useBlocker } from "react-router-dom";
import type { IssueFilter, StatusCategory, TrackerProject } from "../api/types";

type Tone = "neutral" | "info" | "success" | "warning" | "error";

export const TYPE_LABEL: Record<string, string> = { task: "Задача", bug: "Ошибка", story: "История", epic: "Эпик" };
export const typeLabel = (t: string) => TYPE_LABEL[t] ?? t;

export const PRIORITIES: { id: string; label: string; tone: Tone }[] = [
  { id: "highest", label: "Наивысший", tone: "error" },
  { id: "high", label: "Высокий", tone: "warning" },
  { id: "medium", label: "Средний", tone: "info" },
  { id: "low", label: "Низкий", tone: "neutral" },
  { id: "lowest", label: "Наименьший", tone: "neutral" },
];
export const priorityOf = (id?: string) => PRIORITIES.find((p) => p.id === id);

export const CATEGORY_TONE: Record<StatusCategory, Tone> = { todo: "neutral", in_progress: "info", done: "success" };
export const CATEGORY_LABEL: Record<StatusCategory, string> = { todo: "К работе", in_progress: "В работе", done: "Готово" };

export function statusName(p: TrackerProject | undefined, id: string): string {
  return p?.workflow.find((s) => s.id === id)?.name ?? id;
}

/** allowedStatuses lists the columns an issue may move to, its own included. */
export function allowedStatuses(p: TrackerProject, from: string) {
  const allowed = p.transitions?.[from];
  return p.workflow.filter((s) => s.id === from || !allowed || allowed.includes(s.id));
}

export const issueUrl = (key: string) => `/issue/${encodeURIComponent(key)}`;
export const projectUrl = (key: string) => `/tracker/${encodeURIComponent(key)}`;

const TRANSLIT: Record<string, string> = {
  а: "a", б: "b", в: "v", г: "g", д: "d", е: "e", ё: "e", ж: "zh", з: "z", и: "i", й: "j", к: "k", л: "l", м: "m", н: "n",
  о: "o", п: "p", р: "r", с: "s", т: "t", у: "u", ф: "f", х: "h", ц: "c", ч: "ch", ш: "sh", щ: "sch", ъ: "", ы: "y", ь: "", э: "e", ю: "yu", я: "ya",
};

/**
 * suggestKey makes an issue key from a project name: the first letters of its
 * words, or the start of a single word. Empty when no letter can start it.
 */
export function suggestKey(title: string): string {
  const words = [...title.toLowerCase()].map((c) => TRANSLIT[c] ?? c).join("").match(/[a-z0-9]+/g) ?? [];
  const key = (words.length > 1 ? words.map((w) => w[0]).join("") : (words[0] ?? "").slice(0, 4)).toUpperCase().slice(0, 10);
  return /^[A-Z]/.test(key) ? key : "";
}

/** splitList turns "web, ui  board" into ["web", "ui", "board"]. */
export function splitList(s: string): string[] {
  return s.split(/[,\s]+/).map((x) => x.trim().replace(/^#/, "")).filter(Boolean);
}

// Filters live in the URL so a filtered list can be shared and reloaded.
const FILTER_KEYS = ["status", "category", "assignee", "type", "label", "sprint", "q"] as const;

export function filterFromParams(params: URLSearchParams): IssueFilter {
  const f: IssueFilter = {};
  for (const k of FILTER_KEYS) {
    const v = params.get(k);
    if (!v) continue;
    if (k === "status" || k === "label") f[k] = v.split(",").filter(Boolean);
    else if (k === "category") f.category = v as StatusCategory;
    else f[k] = v;
  }
  return f;
}

export function filterToParams(f: IssueFilter): URLSearchParams {
  const p = new URLSearchParams();
  for (const k of FILTER_KEYS) {
    const v = f[k];
    if (Array.isArray(v) ? v.length : v) p.set(k, Array.isArray(v) ? v.join(",") : String(v));
  }
  return p;
}

/** Saved filters are a per-browser convenience; the list works without them. */
export type SavedFilter = { name: string; query: string };
const savedKey = (project: string) => `lumen.issueFilters.${project}`;

export function loadSavedFilters(project: string): SavedFilter[] {
  try {
    const raw = localStorage.getItem(savedKey(project));
    const list = raw ? JSON.parse(raw) : [];
    return Array.isArray(list) ? list.filter((x) => typeof x?.name === "string" && typeof x?.query === "string") : [];
  } catch {
    return [];
  }
}

export function storeSavedFilters(project: string, list: SavedFilter[]) {
  try { localStorage.setItem(savedKey(project), JSON.stringify(list)); } catch { /* Storage is optional. */ }
}

/**
 * useDraftGuard asks before leaving a page with unsaved text. React Router
 * allows one blocker at a time, so a page combines its drafts into one flag.
 */
export function useDraftGuard(dirty: boolean) {
  useBeforeUnload((e) => {
    if (dirty) e.preventDefault();
  });
  const blocker = useBlocker(({ currentLocation, nextLocation }) =>
    dirty && currentLocation.pathname !== nextLocation.pathname,
  );
  useEffect(() => {
    if (blocker.state !== "blocked") return;
    if (window.confirm("Есть несохранённые изменения. Уйти и потерять их?")) blocker.proceed();
    else blocker.reset();
  }, [blocker.state]);
}
