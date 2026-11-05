import Icon from "./Icon";
import { useState } from "react";
import { Link } from "../lib/navigation";
import { useQuery } from "@tanstack/react-query";
import { Alert, Badge, Button, Spinner } from "@villetacore/velessoft-ui";
import { api } from "../api/client";
import { errorText } from "../lib/errors";
import { comparePages } from "../lib/tree";
import NewNoteDialog from "./NewNoteDialog";

/** RelatedContent lists documents and working entries attached to a project, note or task. */
export default function RelatedContent({ parent, subpages = false }: { parent: string; subpages?: boolean }) {
  const [creating, setCreating] = useState<"entry" | "document" | null>(null);
  const { data, isLoading, error } = useQuery({
    queryKey: ["notes", { parent }], queryFn: () => api.notes({ parent, limit: 2000 }),
  });
  const entries = (data?.items.filter((n) => n.content_kind !== "description") ?? [])
    .sort((a, b) => (a.content_kind === b.content_kind ? comparePages(a, b) : a.content_kind === "document" ? -1 : 1));
  return <section className="related-content">
    <div className="section-head">{subpages ? "Подстраницы и записи" : "Документы и записи"} {entries.length > 0 && <span>{entries.length}</span>}
      <div className="head-actions">
        <Button size="sm" onClick={() => setCreating("document")}><Icon name="plus" size={14} />{subpages ? "Подстраница" : "Документ"}</Button>
        <Button size="sm" onClick={() => setCreating("entry")}><Icon name="plus" size={14} />Заметка</Button>
      </div>
    </div>
    {isLoading && <Spinner label="Загрузка материалов" />}
    {error && <Alert tone="error" title="Материалы не загрузились">{errorText(error)}</Alert>}
    {!isLoading && !error && !entries.length && <p className="hint">Здесь будут технические документы, решения и рабочие записи по этой теме.</p>}
    {entries.length > 0 && <div className="note-list">
      {entries.map((n) => <div className="note-row" key={n.id}>
        <Icon name={n.content_kind === "document" ? "book" : "file"} size={16} />
        <Link href={`/note/${n.path}`}>{n.title}</Link>
        <Badge tone={n.content_kind === "document" ? "info" : "neutral"}>{n.content_kind === "document" ? "документ" : "заметка"}</Badge>
        {n.open_tasks > 0 && <Badge>{n.open_tasks} задач</Badge>}
        <span className="path">{n.updated?.slice(0, 10)}</span>
      </div>)}
    </div>}
    {creating && <NewNoteDialog parent={parent} contentKind={creating} onClose={() => setCreating(null)} />}
  </section>;
}
