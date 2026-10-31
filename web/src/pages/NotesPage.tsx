import Icon from "../components/Icon";
import { useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import type { NoteSummary } from "../api/types";
import NewNoteDialog from "../components/NewNoteDialog";
import { errorText } from "../lib/errors";

type Sort = "folder" | "updated";

export default function NotesPage() {
  const [params, setParams] = useSearchParams();
  const tag = params.get("tag") ?? undefined;
  const documentsOnly = params.get("section") === "documents";
  const [filter, setFilter] = useState("");
  const [sort, setSort] = useState<Sort>("folder");
  const [creating, setCreating] = useState(false);

  const { data, isLoading, error } = useQuery({
    queryKey: ["notes", { tag }],
    queryFn: () => api.notes({ tag: tag ? [tag] : undefined, limit: 2000 }),
  });

  const needle = filter.trim().toLowerCase();
  const items = (data?.items ?? []).filter((n) =>
    n.content_kind !== "description" && (!documentsOnly || n.content_kind === "document") &&
    (!needle || n.title.toLowerCase().includes(needle))
  );

  return (
    <>
      <div className="page-head">
        <h1>{tag ? `#${tag}` : documentsOnly ? "Документация" : "Материалы"}</h1>
        <span className="sub">{items.length}</span>
        {tag && (
          <button className="link" onClick={() => setParams({})}>
            сбросить тег
          </button>
        )}
        <div className="head-actions">
          <button className="btn primary" onClick={() => setCreating(true)}>
            <Icon name="plus" size={16} /> Новый документ
          </button>
        </div>
      </div>

      <div className="toolbar">
        <input
          className="input"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="Найти по названию"
          aria-label="Поиск материалов"
          style={{ flex: 1 }}
        />
        <select className="input" value={sort} onChange={(e) => setSort(e.target.value as Sort)} aria-label="Порядок">
          <option value="folder">по разделам</option>
          <option value="updated">недавно изменённые</option>
        </select>
      </div>

      {isLoading && <div className="spinner">Загрузка…</div>}
      {error && <div className="error">Не удалось загрузить материалы: {errorText(error)}</div>}
      {!isLoading && items.length === 0 && (
        <div className="empty">
          {needle || tag ? "Ничего не найдено." : "Здесь будет ваша документация и рабочие материалы."}
        </div>
      )}

      {sort === "updated" ? (
        <div className="note-list">
          {[...items]
            .sort((a, b) => (b.updated ?? "").localeCompare(a.updated ?? ""))
            .map((n) => (
              <NoteRow key={n.path} note={n} />
            ))}
        </div>
      ) : (
        groupByFolder(items).map(([folder, notes]) => (
          <section key={folder}>
            <div className="section-head">
              {folder} <span style={{ opacity: 0.7 }}>{notes.length}</span>
            </div>
            <div className="note-list">
              {notes.map((n) => (
                <NoteRow key={n.path} note={n} />
              ))}
            </div>
          </section>
        ))
      )}

      {creating && <NewNoteDialog onClose={() => setCreating(false)} />}
    </>
  );
}

function NoteRow({ note: n }: { note: NoteSummary }) {
  return (
    <div className="note-row">
      <span className="note-icon"><Icon name={n.type === "project" ? "folder" : "file"} size={19} /></span>
      <Link to={`/note/${n.path}`}>{n.title}</Link>
      {n.open_tasks > 0 && <span className="chip">{n.open_tasks} задач</span>}
      {n.tags.slice(0, 3).map((t) => (
        <Link key={t} className="chip tag" to={`/notes?tag=${encodeURIComponent(t)}`}>
          #{t}
        </Link>
      ))}
      <span className="path">{n.updated ? n.updated.slice(0, 10) : ""}</span>
    </div>
  );
}

function groupByFolder(notes: NoteSummary[]): [string, NoteSummary[]][] {
  const groups = new Map<string, NoteSummary[]>();
  for (const n of notes) {
    const folder = n.type === "project" ? "Проекты" : n.content_kind === "document" ? "Документация" : n.content_kind === "entry" ? "Рабочие записи" : "Другие материалы";
    const list = groups.get(folder) ?? [];
    list.push(n);
    groups.set(folder, list);
  }
  for (const list of groups.values()) list.sort((a, b) => a.title.localeCompare(b.title, "ru"));
  return [...groups.entries()].sort(([a], [b]) => a.localeCompare(b));
}
