import { useEffect, useState } from "react";
import { useBeforeUnload, useBlocker } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../api/client";
import { errorText, isConflict } from "../lib/errors";
import Markdown from "./Markdown";
import { Alert, Button, Spinner } from "@villetacore/velessoft-ui";

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
  if (!edit && (list.isLoading || (summary && content.isLoading))) return <Spinner label="Загрузка описания" />;
  if (!edit && (list.error || content.error)) return <Alert tone="error" title="Описание не загрузилось">{errorText(list.error ?? content.error)}</Alert>;
  const conflict = changed || isConflict(save.error);
  return <section>
    <div className="section-head">Описание
      {!edit && <div className="head-actions">
        <Button size="sm" onClick={() => setEdit({ body: content.data?.body ?? "", base: content.data?.body ?? "", revision: content.data?.revision ?? 0, path: content.data?.path })}>
          {summary ? "Изменить описание" : "Добавить описание"}
        </Button>
      </div>}
    </div>
    {edit ? <>
      <textarea className="note-editor short" aria-label="Описание задачи" value={edit.body} disabled={save.isPending}
        onChange={(e) => setEdit({ ...edit, body: e.target.value })} autoFocus />
      {conflict && <Alert tone="warning" title="Описание изменилось в другом окне">Ваш текст сохранён здесь. Скопируйте его перед загрузкой актуальной версии.</Alert>}
      {save.isError && !isConflict(save.error) && <Alert tone="error" title="Не удалось сохранить">{errorText(save.error)}</Alert>}
      <div className="modal-actions">
        {conflict && <Button onClick={() => {
          if (window.confirm("Загрузить актуальное описание вместо вашего текста?")) { setEdit(null); save.reset(); }
        }}>Загрузить актуальную версию</Button>}
        <Button disabled={save.isPending} onClick={() => {
          if (!dirty || window.confirm("Отменить изменения описания?")) { setEdit(null); save.reset(); }
        }}>Отмена</Button>
        <Button variant="primary" loading={save.isPending} disabled={changed || !dirty} onClick={() => save.mutate()}>Сохранить описание</Button>
      </div>
    </> : content.data?.body.trim()
      ? <div className="card" style={{ padding: 16 }}><Markdown body={content.data.body} /></div>
      : <p className="hint">Контекст, требования, ссылки и ожидаемый результат.</p>}
  </section>;
}
