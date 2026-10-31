import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api/client";
import type { NoteType } from "../api/types";
import { errorText } from "../lib/errors";

export default function NewNoteDialog({ type = "note", parent, contentKind = "document", onClose }: {
  type?: NoteType; parent?: string; contentKind?: "document" | "entry"; onClose: () => void;
}) {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const label = type === "project" ? "Новый проект" : contentKind === "entry" ? "Новая запись" : "Новый документ";
  const create = useMutation({
    mutationFn: () => api.createNote({ title: title.trim(), type, parent,
      content_kind: type === "project" ? undefined : contentKind, body: body || "\n" }),
    onSuccess: (note) => {
      qc.invalidateQueries();
      onClose();
      navigate(`/note/${note.path}`);
    },
  });
  const close = () => {
    if (!create.isPending && (!(title || body) || window.confirm("Закрыть без сохранения?"))) onClose();
  };
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") close(); };
    const onUnload = (e: BeforeUnloadEvent) => { if (title || body) e.preventDefault(); };
    window.addEventListener("keydown", onKey);
    window.addEventListener("beforeunload", onUnload);
    return () => { window.removeEventListener("keydown", onKey); window.removeEventListener("beforeunload", onUnload); };
  }, [title, body, create.isPending]);

  return <div className="modal-backdrop" onMouseDown={(e) => { if (e.target === e.currentTarget) close(); }}>
    <form className="modal" role="dialog" aria-label={label} onSubmit={(e) => {
      e.preventDefault(); if (title.trim() && !create.isPending) create.mutate();
    }}>
      <h2>{label}</h2>
      <label className="field">Название
        <input className="input" value={title} onChange={(e) => setTitle(e.target.value)} autoFocus disabled={create.isPending} />
      </label>
      <label className="field">{type === "project" ? "Описание проекта" : "Содержание"}
        <textarea className="input document-input" value={body} onChange={(e) => setBody(e.target.value)}
          placeholder="Контекст, идеи, ссылки и подробности…" disabled={create.isPending} />
      </label>
      {create.isError && <div className="error">{errorText(create.error)}</div>}
      <div className="modal-actions">
        <button className="btn" type="button" onClick={close} disabled={create.isPending}>Отмена</button>
        <button className="btn primary" disabled={!title.trim() || create.isPending}>Создать</button>
      </div>
    </form>
  </div>;
}
