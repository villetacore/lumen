import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import type { Person, WorkspaceRole } from "../api/types";

export const ROLE_LABEL: Record<WorkspaceRole | "none", string> = {
  admin: "Администратор",
  member: "Участник",
  viewer: "Читатель",
  none: "Нет доступа",
};

export const ROLE_TONE: Record<WorkspaceRole | "none", "info" | "success" | "neutral" | "error"> = {
  admin: "info",
  member: "success",
  viewer: "neutral",
  none: "error",
};

/** usePeople loads the directory once per session view; it changes on sign-ins only. */
export function usePeople() {
  return useQuery({ queryKey: ["users"], queryFn: () => api.users(), staleTime: 60_000 });
}

export function personLabel(p: Person | undefined, username: string): string {
  return p?.name ? `${p.name} (${p.username})` : username;
}

/**
 * PeopleOptions is a <datalist> of known usernames for text inputs, so an
 * assignee can be picked from the directory or still typed freely.
 */
export function PeopleOptions({ id }: { id: string }) {
  const { data } = usePeople();
  return <datalist id={id}>
    {data?.items.map((p) => <option key={p.username} value={p.username}>{p.name || p.username}</option>)}
  </datalist>;
}
