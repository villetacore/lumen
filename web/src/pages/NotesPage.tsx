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
  const [filter, setFilter] = useState("");
  const [sort, setSort] = useState<Sort>("folder");
  const [creating, setCreating] = useState(false);

  const { data, isLoading, error } = useQuery({
    queryKey: ["notes", { tag }],
    queryFn: () => api.notes({ tag: tag ? [tag] : undefined, limit: 2000 }),
  });

  const needle = filter.trim().toLowerCase();
  const items = (data?.items ?? []).filter(
    (n) => !needle || n.title.toLowerCase().includes(needle) || n.path.toLowerCase().includes(needle),
  );

  return (
    <>
      <div className="page-head">
        <h1>{tag ? `#${tag}` : "Все заметки"}</h1>
        <span className="sub">{items.length}</span>
        {tag && (
          <button className="link" onClick={() => setParams({})}>
            сбросить тег
          </button>
        )}
        <div className="head-actions">
          <button className="btn primary" onClick={() => setCreating(true)}>
            ＋ Новая заметка
          </button>
        </div>
      </div>

      <div className="toolbar">
        <input
          className="input"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="Фильтр по названию и пути"
          aria-label="Фильтр заметок"
          style={{ flex: 1 }}
        />
        <select className="input" value={sort} onChange={(e) => setSort(e.target.value as Sort)} aria-label="Порядок">
          <option value="folder">по папкам</option>
          <option value="updated">недавно изменённые</option>
        </select>
      </div>

      {isLoading && <div className="spinner">Загрузка…</div>}
      {error && <div className="error">Не удалось загрузить заметки: {errorText(error)}</div>}
      {!isLoading && items.length === 0 && (
        <div className="empty">
          {needle || tag ? "Ничего не найдено." : "Заметок пока нет — создайте первую."}
        </div>
      )}

      {sort === "updated" ? (
        <div className="note-list">
          {[...items]
            .sort((a, b) => (b.updated ?? "").localeCompare(a.updated ?? ""))
            .map((n) => (
              <NoteRow key={n.path} note={n} showPath />
            ))}
        </div>
      ) : (
        groupByFolder(items).map(([folder, notes]) => (
          <section key={folder}>
            <div className="section-head">
              {folder || "корень"} <span style={{ opacity: 0.7 }}>{notes.length}</span>
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

function NoteRow({ note: n, showPath = false }: { note: NoteSummary; showPath?: boolean }) {
  return (
    <div className="note-row">
      <span style={{ color: "var(--text-faint)" }}>{n.type === "project" ? "◆" : "▭"}</span>
      <Link to={`/note/${n.path}`}>{n.title}</Link>
      {n.open_tasks > 0 && <span className="chip">{n.open_tasks} задач</span>}
      {n.tags.slice(0, 3).map((t) => (
        <Link key={t} className="chip tag" to={`/notes?tag=${encodeURIComponent(t)}`}>
          #{t}
        </Link>
      ))}
      <span className="path">{showPath ? n.path : n.updated ? n.updated.slice(0, 10) : ""}</span>
    </div>
  );
}

function groupByFolder(notes: NoteSummary[]): [string, NoteSummary[]][] {
  const groups = new Map<string, NoteSummary[]>();
  for (const n of notes) {
    const folder = n.path.split("/").slice(0, -1).join("/");
    const list = groups.get(folder) ?? [];
    list.push(n);
    groups.set(folder, list);
  }
  for (const list of groups.values()) list.sort((a, b) => a.title.localeCompare(b.title, "ru"));
  return [...groups.entries()].sort(([a], [b]) => a.localeCompare(b));
}
