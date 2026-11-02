import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import Crumbs, { type Crumb } from "./Crumbs";

/**
 * ParentLink shows where a material lives: section → parent project/task → this page.
 * Without a parent it still names the section, so every page has a way back.
 */
export default function ParentLink({ parent, title, kind }: { parent?: unknown; title: string; kind?: string }) {
  const ref = typeof parent === "string" ? parent : "";
  const at = ref.indexOf(":");
  const refKind = ref.slice(0, at);
  const id = ref.slice(at + 1);
  const { data } = useQuery({
    queryKey: ["parent", ref], enabled: at > 0 && (refKind === "note" || refKind === "task"),
    queryFn: async (): Promise<Crumb> => {
      if (refKind === "task") {
        const task = await api.task(id);
        return { label: task.text, to: `/task/${encodeURIComponent(task.id)}` };
      }
      const note = await api.note(id);
      return { label: note.title, to: `/note/${note.path}` };
    },
  });
  const section: Crumb = kind === "document" ? { label: "Документация", to: "/docs" } : kind === "project" ? { label: "Проекты", to: "/notes" } : { label: "Материалы", to: "/notes" };
  return <Crumbs items={[section, ...(data ? [data] : []), { label: title }]} />;
}
