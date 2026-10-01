import { Link, useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";

export default function SearchPage() {
  const [params] = useSearchParams();
  const q = params.get("q") ?? "";

  const { data, isLoading } = useQuery({
    queryKey: ["search", q],
    queryFn: () => api.search(q),
    enabled: q.length > 0,
  });

  return (
    <>
      <div className="page-head">
        <h1>Поиск</h1>
        <span className="sub">
          «{q}» — {data?.total ?? 0} совпадений
        </span>
      </div>

      {isLoading && <div className="spinner">Ищем…</div>}
      {!isLoading && data?.items.length === 0 && <div className="empty">Ничего не найдено</div>}

      {data?.items.map((hit) => (
        <div key={hit.path} style={{ padding: "8px 0", borderBottom: "1px solid var(--border)" }}>
          <Link to={`/note/${hit.path}`} style={{ fontWeight: 550 }}>
            {hit.title}
          </Link>
          <span className="path" style={{ marginLeft: 8, color: "var(--text-faint)", fontSize: 11.5 }}>
            {hit.path}
          </span>
          <div style={{ color: "var(--text-dim)", fontFamily: "var(--mono)", fontSize: 12 }}>
            {hit.snippet}
          </div>
        </div>
      ))}
    </>
  );
}
