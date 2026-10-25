import { useEffect, useRef, useState } from "react";
import { Link, useBeforeUnload, useBlocker, useLocation, useNavigate } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../api/client";
import type { Note, NoteType, Task } from "../api/types";
import TaskRow from "../components/TaskRow";
import QuickAdd from "../components/QuickAdd";
import Markdown from "../components/Markdown";
import { errorText, isConflict, isNotFound } from "../lib/errors";

/** Edit holds the draft together with the version it was started from. */
type Edit = { draft: string; baseRevision: number; baseBody: string };

const TYPE_LABEL: Record<NoteType, string> = {
  note: "заметка",
  project: "проект",
  area: "область",
  daily: "дневная",
  meeting: "встреча",
  event: "событие",
};

export default function NotePage() {
  const location = useLocation();
  const navigate = useNavigate();
  const path = decodeURIComponent(location.pathname.replace(/^\/note\//, ""));
  const qc = useQueryClient();

  const { data: note, isLoading, error } = useQuery({
    queryKey: ["note", path],
    queryFn: () => api.note(path),
    retry: (n, err) => !isNotFound(err) && n < 1,
  });
  const { data: backlinks } = useQuery({
    queryKey: ["backlinks", path],
    queryFn: () => api.backlinks(path),
    enabled: !!note,
  });

  // The draft lives apart from the query: a refetch after an SSE event or a
  // save elsewhere must never overwrite what the person is typing.
  const [edit, setEdit] = useState<Edit | null>(null);
  const editPath = useRef(path);
  if (editPath.current !== path) {
    editPath.current = path;
    if (edit) setEdit(null);
  }

  // Wikilinks resolve by file name; show the note under its real path.
  useEffect(() => {
    if (note && note.path !== path) navigate(`/note/${note.path}`, { replace: true });
  }, [note?.path, path]);

  const dirty = !!edit && edit.draft !== edit.baseBody;
  const deletedOnDisk = !!edit && isNotFound(error);
  const changedOnDisk = !!edit && !!note && note.revision !== edit.baseRevision;

  useBeforeUnload((e) => {
    if (dirty) e.preventDefault();
  });
  const blocker = useBlocker(({ currentLocation, nextLocation }) =>
    dirty && currentLocation.pathname !== nextLocation.pathname,
  );
  useEffect(() => {
    if (blocker.state !== "blocked") return;
    if (window.confirm("В заметке есть несохранённые изменения. Уйти и потерять их?")) blocker.proceed();
    else blocker.reset();
  }, [blocker.state]);

  const startEdit = (n: Note) => setEdit({ draft: n.body, baseRevision: n.revision, baseBody: n.body });

  const save = useMutation({
    // overwrite=true saves on top of whatever is on disk now.
    mutationFn: ({ overwrite }: { overwrite: boolean }) => {
      if (deletedOnDisk) return api.createNote({ path, title: note?.title, body: edit!.draft });
      return api.putNoteBody(path, edit!.draft, overwrite ? note!.revision : edit!.baseRevision);
    },
    onSuccess: (saved) => {
      setEdit(null);
      qc.setQueryData(["note", path], saved);
      qc.invalidateQueries();
    },
    onError: (err) => {
      if (isConflict(err)) qc.invalidateQueries({ queryKey: ["note", path] });
    },
  });

  const toggleTask = useMutation({
    mutationFn: (t: Task) => api.patchTask(t.id, { status: t.status === "done" ? "todo" : "done" }),
    onSuccess: () => qc.invalidateQueries(),
  });

  const remove = useMutation({
    mutationFn: () => api.deleteNote(note!.path),
    onSuccess: () => {
      qc.invalidateQueries();
      navigate("/notes");
    },
  });

  // Ctrl/Cmd+S saves, Escape leaves the editor when nothing would be lost.
  const onEditorKey = (e: React.KeyboardEvent) => {
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "s") {
      e.preventDefault();
      if (!save.isPending && !changedOnDisk) save.mutate({ overwrite: false });
    } else if (e.key === "Escape" && !dirty) {
      setEdit(null);
    }
  };

  if (isLoading) return <div className="spinner">Загрузка…</div>;
  if (!note && !edit) {
    if (isNotFound(error)) return <MissingNote path={path} />;
    return <div className="error">Не удалось открыть заметку: {errorText(error)}</div>;
  }

  const title = note?.title ?? path;

  return (
    <div className="note-view">
      <div className="page-head">
        <h1>{title}</h1>
        <span className="sub">{note?.path ?? path}</span>
        <div className="head-actions">
          {edit ? (
            <>
              <span className="hint">{dirty ? "изменено · Ctrl S — сохранить" : "без изменений"}</span>
              <button
                className="btn primary"
                onClick={() => save.mutate({ overwrite: false })}
                disabled={save.isPending || changedOnDisk || !dirty}
              >
                {deletedOnDisk ? "Создать заново" : "Сохранить"}
              </button>
              <button
                className="btn"
                onClick={() => {
                  if (!dirty || window.confirm("Отменить несохранённые изменения?")) setEdit(null);
                }}
              >
                Отмена
              </button>
            </>
          ) : (
            <>
              <button className="btn" onClick={() => note && startEdit(note)}>
                ✎ Редактировать
              </button>
              <button
                className="btn danger"
                disabled={remove.isPending}
                onClick={() => {
                  if (window.confirm(`Переместить «${title}» в корзину vault (.lumen/trash)?`)) remove.mutate();
                }}
                title="Переместить в корзину"
              >
                Удалить
              </button>
            </>
          )}
        </div>
      </div>

      {deletedOnDisk && (
        <div className="warn">
          Файл удалён или переименован на диске. Ваш текст сохранён здесь — его можно создать заново
          или скопировать.
        </div>
      )}

      {changedOnDisk && note && (
        <div className="warn">
          Заметка изменилась на диске, пока вы редактировали (ревизия {edit!.baseRevision} → {note.revision}).
          Ваш текст не потерян.
          <div className="warn-actions">
            <button className="btn" onClick={() => navigator.clipboard?.writeText(edit!.draft)}>
              Скопировать мой текст
            </button>
            <button
              className="btn"
              onClick={() => {
                if (window.confirm("Заменить ваш текст версией с диска?")) startEdit(note);
              }}
            >
              Взять версию с диска
            </button>
            <button className="btn primary" onClick={() => save.mutate({ overwrite: true })}>
              Сохранить мою версию поверх
            </button>
          </div>
        </div>
      )}

      {save.isError && !isConflict(save.error) && (
        <div className="error" style={{ marginBottom: 10 }}>
          Не удалось сохранить: {errorText(save.error)}
        </div>
      )}
      {remove.isError && <div className="error">Не удалось удалить: {errorText(remove.error)}</div>}

      {edit ? (
        <textarea
          className="note-editor"
          value={edit.draft}
          onChange={(e) => setEdit({ ...edit, draft: e.target.value })}
          onKeyDown={onEditorKey}
          spellCheck={false}
          autoFocus
          aria-label="Текст заметки"
        />
      ) : (
        note && (
          <>
            {note.tasks && note.tasks.length > 0 && (
              <section>
                <div className="section-head">
                  Задачи {note.tasks.filter((t) => t.status === "todo" || t.status === "doing").length} /{" "}
                  {note.tasks.length}
                </div>
                {note.tasks.map((t) => (
                  <TaskRow key={t.id} task={t} showNote={false} />
                ))}
              </section>
            )}
            <QuickAdd note={note.path} section={hasHeading(note.body, "Задачи") ? "Задачи" : undefined} />

            <div className="section-head">Содержимое</div>
            {note.body.trim() ? (
              <div onDoubleClick={() => startEdit(note)}>
                <Markdown body={note.body} tasks={note.tasks} onToggleTask={(t) => toggleTask.mutate(t)} />
              </div>
            ) : (
              <div className="empty">
                Пусто. <button className="link" onClick={() => startEdit(note)}>Начать писать</button>
              </div>
            )}

            <Properties note={note} />
          </>
        )
      )}

      {backlinks && backlinks.items.length > 0 && !edit && (
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

function hasHeading(body: string, name: string): boolean {
  return new RegExp(`^#{1,6}\\s+${name}\\s*$`, "mi").test(body);
}

/** Properties shows the frontmatter and edits title, type and tags. */
function Properties({ note }: { note: Note }) {
  const qc = useQueryClient();
  const [form, setForm] = useState<{ title: string; type: string; tags: string } | null>(null);

  const save = useMutation({
    mutationFn: () =>
      api.patchNote(note.path, {
        title: form!.title.trim(),
        type: form!.type,
        tags: form!.tags
          .split(/[,\s]+/)
          .map((t) => t.replace(/^#/, ""))
          .filter(Boolean),
        revision: note.revision,
      }),
    onSuccess: () => {
      setForm(null);
      qc.invalidateQueries();
    },
  });

  return (
    <>
      <div className="section-head">
        Свойства
        {!form && (
          <button
            className="link"
            onClick={() => setForm({ title: note.title, type: note.type, tags: note.tags.join(", ") })}
          >
            изменить
          </button>
        )}
      </div>
      {form ? (
        <form
          className="props-form"
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate();
          }}
        >
          <label className="field">
            Название
            <input className="input" value={form.title} onChange={(e) => setForm({ ...form, title: e.target.value })} />
          </label>
          <label className="field">
            Тип
            <select className="input" value={form.type} onChange={(e) => setForm({ ...form, type: e.target.value })}>
              {Object.entries(TYPE_LABEL).map(([v, l]) => (
                <option key={v} value={v}>
                  {l}
                </option>
              ))}
            </select>
          </label>
          <label className="field">
            Теги
            <input className="input" value={form.tags} onChange={(e) => setForm({ ...form, tags: e.target.value })} />
          </label>
          <div className="modal-actions" style={{ justifyContent: "flex-start" }}>
            <button className="btn primary" type="submit" disabled={save.isPending}>
              Сохранить
            </button>
            <button className="btn" type="button" onClick={() => setForm(null)}>
              Отмена
            </button>
          </div>
          {save.isError && <div className="error">{errorText(save.error)}</div>}
        </form>
      ) : (
        <table className="prop-table">
          <tbody>
            <tr>
              <td>тип</td>
              <td>{TYPE_LABEL[note.type] ?? note.type}</td>
            </tr>
            <tr>
              <td>теги</td>
              <td>
                {note.tags.length
                  ? note.tags.map((t) => (
                      <Link key={t} className="md-tag" to={`/notes?tag=${encodeURIComponent(t)}`} style={{ marginRight: 6 }}>
                        #{t}
                      </Link>
                    ))
                  : "—"}
              </td>
            </tr>
            {Object.entries(note.frontmatter ?? {})
              .filter(([k]) => !["title", "type", "tags", "id"].includes(k))
              .map(([k, v]) => (
                <tr key={k}>
                  <td>{k}</td>
                  <td>{typeof v === "object" ? JSON.stringify(v) : String(v)}</td>
                </tr>
              ))}
            <tr>
              <td>ревизия</td>
              <td>{note.revision}</td>
            </tr>
          </tbody>
        </table>
      )}
    </>
  );
}

/** MissingNote is what a wikilink to a note that does not exist yet opens. */
function MissingNote({ path }: { path: string }) {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const target = path.includes("/") ? path : `10-notes/${path}`;
  const create = useMutation({
    mutationFn: () => api.createNote({ path: target }),
    onSuccess: (n) => {
      qc.invalidateQueries();
      navigate(`/note/${n.path}`, { replace: true });
    },
  });
  return (
    <div className="empty">
      <p>Заметки «{path}» пока нет.</p>
      <button className="btn primary" onClick={() => create.mutate()} disabled={create.isPending}>
        Создать {target.endsWith(".md") ? target : target + ".md"}
      </button>
      {create.isError && <div className="error" style={{ marginTop: 10 }}>{errorText(create.error)}</div>}
    </div>
  );
}
