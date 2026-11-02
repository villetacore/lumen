import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import type { NoteSummary } from "../api/types";

/** useNoteIndex loads every note summary once, so screens can show titles instead of file paths. */
export function useNoteIndex() {
  const { data, isLoading, error } = useQuery({
    queryKey: ["notes", { index: true }],
    queryFn: () => api.notes({ limit: 5000 }),
  });
  const byPath = new Map<string, NoteSummary>();
  const byId = new Map<string, NoteSummary>();
  for (const n of data?.items ?? []) {
    byPath.set(n.path, n);
    byId.set(n.id, n);
  }
  return { items: data?.items ?? [], byPath, byId, isLoading, error };
}

/** noteLabel names the note that holds a task: daily notes are titled by date, so say so. */
export function noteLabel(note: NoteSummary | undefined, path: string): string {
  const title = note?.title ?? titleFromPath(path);
  return note?.type === "daily" ? `День ${title}` : title;
}

/** titleFromPath is the fallback label when a note is not indexed yet. */
export function titleFromPath(path: string): string {
  const file = path.split("/").pop() ?? path;
  return file.replace(/\.md$/i, "");
}

/** parentRef splits "note:<id>" / "task:<id>" references stored in frontmatter. */
export function parentRef(ref?: string): { kind: "note" | "task"; id: string } | null {
  if (!ref) return null;
  const at = ref.indexOf(":");
  const kind = ref.slice(0, at);
  if (kind !== "note" && kind !== "task") return null;
  return { kind, id: ref.slice(at + 1) };
}
