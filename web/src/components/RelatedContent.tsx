import { useState } from "react";
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import { errorText } from "../lib/errors";
import NewNoteDialog from "./NewNoteDialog";

export default function RelatedContent({ parent }: { parent: string }) {
  const [creating, setCreating] = useState<"entry" | "document" | null>(null);
  const { data, isLoading, error } = useQuery({
    queryKey: ["notes", { parent }], queryFn: () => api.notes({ parent, limit: 2000 }),
  });
  const entries = data?.items.filter((n) => n.content_kind !== "description") ?? [];
  return <section className="related-content">
    <div className="section-head">Материалы
      <div className="head-actions">
        <button className="btn" onClick={() => setCreating("entry")}>＋ Запись</button>
        <button className="btn" onClick={() => setCreating("document")}>＋ Документ</button>
      </div>
    </div>
    {isLoading && <div className="spinner">Загрузка…</div>}
    {error && <div className="error">{errorText(error)}</div>}
    {!isLoading && !error && !entries.length && <p className="hint">Добавьте подробности, рабочие записи или документацию.</p>}
    {entries.map((n) => <div className="note-row" key={n.id}>
      <span>{n.content_kind === "document" ? "▤" : "▭"}</span>
      <Link to={`/note/${n.path}`}>{n.title}</Link>
      <span className="chip">{n.content_kind === "document" ? "Документ" : "Запись"}</span>
    </div>)}
    {creating && <NewNoteDialog parent={parent} contentKind={creating} onClose={() => setCreating(null)} />}
  </section>;
}
