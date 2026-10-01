import { Link, useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";

export default function NotesPage() {
  const [params] = useSearchParams();
  const tag = params.get("tag") ?? undefined;

  const { data, isLoading } = useQuery({
    queryKey: ["notes", { tag }],
    queryFn: () => api.notes({ tag: tag ? [tag] : undefined }),
  });

  return (
    <>
      <div className="page-head">
        <h1>{tag ? `#${tag}` : "Все заметки"}</h1>
        <span className="sub">{data?.total ?? 0}</span>
      </div>

      {isLoading && <div className="spinner">Загрузка…</div>}

      <div className="note-list">
        {data?.items.map((n) => (
          <div key={n.path} className="note-row">
            <span style={{ color: "var(--text-faint)" }}>▭</span>
            <Link to={`/note/${n.path}`}>{n.title}</Link>
            {n.open_tasks > 0 && <span className="chip">{n.open_tasks} задач</span>}
            {n.tags.slice(0, 3).map((t) => (
              <span key={t} className="chip tag">
                #{t}
              </span>
            ))}
            <span className="path">{n.path}</span>
          </div>
        ))}
      </div>
    </>
  );
}
