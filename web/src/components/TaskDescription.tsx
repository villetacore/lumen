import { useEffect, useState } from "react";
import { useBeforeUnload, useBlocker } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../api/client";
import { errorText, isConflict } from "../lib/errors";
import Markdown from "./Markdown";

export default function TaskDescription({ parent, externalDirty = false }: { parent: string; externalDirty?: boolean }) {
  const qc = useQueryClient();
  const list = useQuery({ queryKey: ["notes", { parent, content_kind: "description" }],
    queryFn: () => api.notes({ parent, content_kind: "description" }) });
  const summary = list.data?.items[0];
  const content = useQuery({ queryKey: ["note", summary?.path], queryFn: () => api.note(summary!.path), enabled: !!summary });
  const [edit, setEdit] = useState<{ body: string; base: string; revision: number; path?: string } | null>(null);
  const dirty = !!edit && edit.body !== edit.base;
  useBeforeUnload((e) => { if (dirty || externalDirty) e.preventDefault(); });
  const blocker = useBlocker(() => dirty || externalDirty);
  useEffect(() => {
    if (blocker.state === "blocked") {
      if (window.confirm("Есть несохранённые изменения. Уйти без сохранения?")) blocker.proceed();
      else blocker.reset();
    }
  }, [blocker.state]);
  const save = useMutation({
    mutationFn: () => edit!.path
      ? api.putNoteBody(edit!.path, edit!.body, edit!.revision)
      : api.createNote({ parent, content_kind: "description", title: "Описание", body: edit!.body }),
    onSuccess: () => { setEdit(null); qc.invalidateQueries(); },
    onError: () => { qc.invalidateQueries({ queryKey: ["notes"] }); qc.invalidateQueries({ queryKey: ["note"] }); },
  });
  const changed = !!edit && !!content.data && (edit.revision !== content.data.revision || edit.path !== content.data.path);
  if (!edit && (list.isLoading || (summary && content.isLoading))) return <div className="spinner">Загрузка описания…</div>;
  if (!edit && (list.error || content.error)) return <div className="error">{errorText(list.error ?? content.error)}</div>;
  return <section>
    <div className="section-head">Описание</div>
    {edit ? <>
      <textarea className="note-editor" aria-label="Описание задачи" value={edit.body} disabled={save.isPending}
        onChange={(e) => setEdit({ ...edit, body: e.target.value })} />
      {(changed || isConflict(save.error)) && <p className="warn">Описание изменилось в другом окне. Ваш текст сохранён здесь. Скопируйте его перед загрузкой актуальной версии.</p>}
      {save.isError && <p className="error">{errorText(save.error)}</p>}
      <div className="modal-actions">
        {(changed || isConflict(save.error)) && <button className="btn" onClick={() => {
          if (window.confirm("Загрузить актуальное описание вместо вашего текста?")) { setEdit(null); save.reset(); }
        }}>Загрузить актуальную версию</button>}
        <button className="btn" disabled={save.isPending} onClick={() => {
          if (!dirty || window.confirm("Отменить изменения описания?")) { setEdit(null); save.reset(); }
        }}>Отмена</button>
        <button className="btn primary" disabled={save.isPending || changed || !dirty} onClick={() => save.mutate()}>Сохранить описание</button>
      </div>
    </> : <>
      {content.data?.body.trim() ? <Markdown body={content.data.body} /> : <p className="hint">Контекст, требования, ссылки и ожидаемый результат.</p>}
      <button className="btn" onClick={() => setEdit({ body: content.data?.body ?? "", base: content.data?.body ?? "", revision: content.data?.revision ?? 0, path: content.data?.path })}>
        {summary ? "Изменить описание" : "Добавить описание"}
      </button>
    </>}
  </section>;
}
