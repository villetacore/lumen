import { useEffect, useState } from "react";
import { Link, useLocation } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../api/client";
import TaskRow from "../components/TaskRow";
import QuickAdd from "../components/QuickAdd";

export default function NotePage() {
  const location = useLocation();
  const path = decodeURIComponent(location.pathname.replace(/^\/note\//, ""));
  const qc = useQueryClient();

  const { data: note, isLoading, error } = useQuery({
    queryKey: ["note", path],
    queryFn: () => api.note(path),
  });
  const { data: backlinks } = useQuery({
    queryKey: ["backlinks", path],
    queryFn: () => api.backlinks(path),
    enabled: !!note,
  });

  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState("");

  // Reset the draft whenever a different note (or a newer revision) loads.
  useEffect(() => {
    if (note) setDraft(note.body);
    setEditing(false);
  }, [note?.path, note?.revision]);

  const save = useMutation({
    mutationFn: () => api.putNoteBody(path, draft, note!.revision),
    onSuccess: () => {
      setEditing(false);
      qc.invalidateQueries();
    },
  });

  if (isLoading) return <div className="spinner">Загрузка…</div>;
  if (error || !note) return <div className="error">Заметка не найдена: {path}</div>;

  return (
    <div className="note-view">
      <div className="page-head">
        <h1>{note.title}</h1>
        <span className="sub">{note.path}</span>
        <div style={{ marginLeft: "auto", display: "flex", gap: 8 }}>
          {editing ? (
            <>
              <button className="btn primary" onClick={() => save.mutate()} disabled={save.isPending}>
                Сохранить
              </button>
              <button className="btn" onClick={() => { setDraft(note.body); setEditing(false); }}>
                Отмена
              </button>
            </>
          ) : (
            <button className="btn" onClick={() => setEditing(true)}>
              ✎ Редактировать
            </button>
          )}
        </div>
      </div>

      {save.isError && (
        <div className="error" style={{ marginBottom: 10 }}>
          {String(save.error)} — заметка изменилась на диске, перезагрузите страницу.
        </div>
      )}

      {note.tasks && note.tasks.length > 0 && !editing && (
        <section>
          <div className="section-head">Задачи {note.tasks.length}</div>
          {note.tasks.map((t) => (
            <TaskRow key={t.id} task={t} />
          ))}
          <QuickAdd note={note.path} />
        </section>
      )}

      <div className="section-head">Содержимое</div>
      {editing ? (
        <textarea
          className="note-body"
          style={{ width: "100%", minHeight: 420, resize: "vertical" }}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          spellCheck={false}
        />
      ) : (
        <div className="note-body">{note.body}</div>
      )}

      <div className="section-head">Свойства</div>
      <table className="prop-table">
        <tbody>
          <tr>
            <td>тип</td>
            <td>{note.type}</td>
          </tr>
          <tr>
            <td>теги</td>
            <td>{note.tags.length ? note.tags.map((t) => `#${t}`).join(" ") : "—"}</td>
          </tr>
          <tr>
            <td>ревизия</td>
            <td>{note.revision}</td>
          </tr>
          {Object.entries(note.frontmatter ?? {})
            .filter(([k]) => !["title", "type", "tags", "id"].includes(k))
            .map(([k, v]) => (
              <tr key={k}>
                <td>{k}</td>
                <td>{String(v)}</td>
              </tr>
            ))}
        </tbody>
      </table>

      {backlinks && backlinks.items.length > 0 && (
        <>
          <div className="section-head">Обратные ссылки {backlinks.total}</div>
          {backlinks.items.map((b) => (
            <div key={b.path} className="note-row">
              <span style={{ color: "var(--text-faint)" }}>←</span>
              <Link to={`/note/${b.path}`}>{b.title}</Link>
              <span className="path">{b.path}</span>
            </div>
          ))}
        </>
      )}
    </div>
  );
}
