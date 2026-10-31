import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";

export default function ParentLink({ parent }: { parent?: unknown }) {
  const ref = typeof parent === "string" ? parent : "";
  const kind = ref.slice(0, ref.indexOf(":"));
  const id = ref.slice(ref.indexOf(":") + 1);
  const { data } = useQuery({
    queryKey: ["parent", ref], enabled: !!id && (kind === "note" || kind === "task"),
    queryFn: async () => {
      if (kind === "task") {
        const task = await api.task(id);
        return { title: task.text, url: `/task/${encodeURIComponent(task.id)}` };
      }
      const note = await api.note(id);
      return { title: note.title, url: `/note/${note.path}` };
    },
  });
  return data ? <p><Link to={data.url}>← {data.title}</Link></p> : null;
}
