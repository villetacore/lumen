import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, Button, Dialog, Input, Select, Textarea } from "@villetacore/velessoft-ui";
import { api } from "../api/client";
import type { NoteType } from "../api/types";
import { errorText } from "../lib/errors";

/**
 * NewNoteDialog creates a project, a document or a working entry. A document
 * created outside a project or task may be attached to a project here, so the
 * documentation section can show it in that project's group.
 */
export default function NewNoteDialog({ type = "note", parent, contentKind = "document", onClose }: {
  type?: NoteType; parent?: string; contentKind?: "document" | "entry"; onClose: () => void;
}) {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [context, setContext] = useState("");
  const titleInput = useRef<HTMLInputElement>(null);
  // showModal() focuses the dialog's first button; move focus to the title once it is open.
  useEffect(() => {
    const frame = requestAnimationFrame(() => titleInput.current?.focus());
    return () => cancelAnimationFrame(frame);
  }, []);
  const pickContext = type !== "project" && !parent;
  const { data: projects } = useQuery({
    queryKey: ["notes", { type: "project" }],
    queryFn: () => api.notes({ type: "project" }),
    enabled: pickContext,
  });
  const label = type === "project" ? "Новый проект" : contentKind === "entry" ? "Новая запись" : "Новый документ";
  const dirty = !!(title || body);
  const create = useMutation({
    mutationFn: () => api.createNote({ title: title.trim(), type, parent: parent ?? (context || undefined),
      content_kind: type === "project" ? undefined : contentKind, body: body || "\n" }),
    onSuccess: (note) => {
      qc.invalidateQueries();
      onClose();
      navigate(`/note/${note.path}`);
    },
  });
  const close = () => {
    if (!create.isPending && (!dirty || window.confirm("Закрыть без сохранения?"))) onClose();
  };
  useEffect(() => {
    const onUnload = (e: BeforeUnloadEvent) => { if (dirty) e.preventDefault(); };
    window.addEventListener("beforeunload", onUnload);
    return () => window.removeEventListener("beforeunload", onUnload);
  }, [dirty]);

  return <Dialog open onClose={close} title={label} closeOnBackdrop={!dirty} footer={<>
    <Button onClick={close} disabled={create.isPending}>Отмена</Button>
    <Button type="submit" form="new-note" variant="primary" loading={create.isPending} loadingText="Создание…" disabled={!title.trim()}>Создать</Button>
  </>}>
    <form id="new-note" onSubmit={(e) => {
      e.preventDefault(); if (title.trim() && !create.isPending) create.mutate();
    }}>
      <Input ref={titleInput} label="Название" required value={title} onChange={(e) => setTitle(e.target.value)} disabled={create.isPending} />
      {pickContext && !!projects?.items.length && (
        <Select label="Проект" hint="Документ появится в разделе документации этого проекта" value={context}
          onChange={(e) => setContext(e.target.value)} disabled={create.isPending}>
          <option value="">Общая документация</option>
          {projects.items.map((p) => <option key={p.id} value={`note:${p.id}`}>{p.title}</option>)}
        </Select>
      )}
      <Textarea label={type === "project" ? "Описание проекта" : "Содержание"} className="document-input" value={body}
        onChange={(e) => setBody(e.target.value)} hint="Markdown: заголовки, списки, [[ссылки]], - [ ] задачи"
        placeholder="Контекст, решения, ссылки и подробности…" disabled={create.isPending} />
      {create.isError && <Alert tone="error" title="Не удалось создать">{errorText(create.error)}</Alert>}
    </form>
  </Dialog>;
}
