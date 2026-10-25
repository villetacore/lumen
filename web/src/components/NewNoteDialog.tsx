import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../api/client";
import type { NoteType } from "../api/types";
import { errorText } from "../lib/errors";

const TYPES: { type: NoteType; label: string; folder: string }[] = [
  { type: "note", label: "Заметка", folder: "10-notes" },
  { type: "project", label: "Проект", folder: "20-projects" },
  { type: "meeting", label: "Встреча", folder: "50-meetings" },
  { type: "area", label: "Область", folder: "30-areas" },
];

/** fileName turns a title into a file name Obsidian and every OS accept. */
function fileName(title: string): string {
  return title.replace(/[\\/:*?"<>|#^[\]]/g, " ").replace(/\s+/g, " ").trim();
}

function initialBody(type: NoteType, title: string): string {
  switch (type) {
    case "project":
      return `# ${title}\n\nЦель: \n\n## Задачи\n\n## Заметки\n`;
    case "meeting":
      return `# ${title}\n\nУчастники: \n\n## Повестка\n\n## Решения\n\n## Задачи\n`;
    default:
      return `# ${title}\n\n`;
  }
}

export default function NewNoteDialog({
  type: initialType = "note",
  onClose,
}: {
  type?: NoteType;
  onClose: () => void;
}) {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [title, setTitle] = useState("");
  const [type, setType] = useState<NoteType>(initialType);
  const [folder, setFolder] = useState(TYPES.find((t) => t.type === initialType)?.folder ?? "10-notes");
  const [tags, setTags] = useState("");

  const { data: notes } = useQuery({ queryKey: ["notes", {}], queryFn: () => api.notes({ limit: 2000 }) });
  const folders = [
    ...new Set([
      ...TYPES.map((t) => t.folder),
      ...(notes?.items ?? []).map((n) => n.path.split("/").slice(0, -1).join("/")).filter(Boolean),
    ]),
  ].sort();

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  const name = fileName(title);
  const path = (folder.trim() ? folder.trim().replace(/^\/+|\/+$/g, "") + "/" : "") + name + ".md";

  const create = useMutation({
    mutationFn: () =>
      api.createNote({
        path,
        title: title.trim(),
        type,
        tags: tags
          .split(/[,\s]+/)
          .map((t) => t.replace(/^#/, ""))
          .filter(Boolean),
        body: initialBody(type, title.trim()),
      }),
    onSuccess: (note) => {
      qc.invalidateQueries();
      onClose();
      navigate(`/note/${note.path}`);
    },
  });

  return (
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <form
        className="modal"
        role="dialog"
        aria-label="Новая заметка"
        onSubmit={(e) => {
          e.preventDefault();
          if (name) create.mutate();
        }}
      >
        <h2>Новая запись</h2>
        <div className="seg">
          {TYPES.map((t) => (
            <button
              key={t.type}
              type="button"
              className={"btn" + (type === t.type ? " active" : "")}
              onClick={() => {
                setType(t.type);
                setFolder(t.folder);
              }}
            >
              {t.label}
            </button>
          ))}
        </div>
        <label className="field">
          Название
          <input className="input" value={title} onChange={(e) => setTitle(e.target.value)} autoFocus />
        </label>
        <label className="field">
          Папка
          <input className="input" list="lumen-folders" value={folder} onChange={(e) => setFolder(e.target.value)} />
          <datalist id="lumen-folders">
            {folders.map((f) => (
              <option key={f} value={f} />
            ))}
          </datalist>
        </label>
        <label className="field">
          Теги
          <input className="input" value={tags} onChange={(e) => setTags(e.target.value)} placeholder="work, идеи" />
        </label>
        <div className="hint">{name ? `Файл: ${path}` : "Введите название"}</div>
        {create.isError && <div className="error">{errorText(create.error)}</div>}
        <div className="modal-actions">
          <button className="btn" type="button" onClick={onClose}>
            Отмена
          </button>
          <button className="btn primary" type="submit" disabled={!name || create.isPending}>
            Создать
          </button>
        </div>
      </form>
    </div>
  );
}
