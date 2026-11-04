import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, Button, Dialog, Input, Select } from "@villetacore/velessoft-ui";
import { api } from "../api/client";
import { errorText } from "../lib/errors";
import { projectUrl, suggestKey } from "../lib/tracker";

const VALID_KEY = /^[A-Z][A-Z0-9]{0,9}$/;

/**
 * NewProjectDialog starts a project with its board. The name is enough: the key
 * follows the name until the person edits it. A project that so far is only a
 * note can get a board here too.
 */
export default function NewProjectDialog({ onClose }: { onClose: () => void }) {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [title, setTitle] = useState("");
  const [typedKey, setTypedKey] = useState<string | null>(null);
  const [note, setNote] = useState("");
  const titleInput = useRef<HTMLInputElement>(null);
  useEffect(() => {
    const frame = requestAnimationFrame(() => titleInput.current?.focus());
    return () => cancelAnimationFrame(frame);
  }, []);

  const { data: plain } = useQuery({ queryKey: ["notes", { type: "project" }], queryFn: () => api.notes({ type: "project" }) });
  const { data: tracked } = useQuery({ queryKey: ["tracker", "projects"], queryFn: api.trackerProjects });
  const used = new Set(tracked?.items.map((p) => p.path));
  const candidates = plain?.items.filter((n) => !used.has(n.path)) ?? [];
  const noteTitle = candidates.find((n) => n.path === note)?.title ?? "";
  const key = typedKey ?? suggestKey(note ? noteTitle : title);

  const create = useMutation({
    mutationFn: () => api.createTrackerProject({ key, title: note ? undefined : title.trim(), note: note || undefined }),
    onSuccess: (p) => { qc.invalidateQueries(); onClose(); navigate(`${projectUrl(p.key)}/board`); },
  });
  const validKey = VALID_KEY.test(key);
  const ready = validKey && (!!note || !!title.trim());

  return <Dialog open onClose={() => !create.isPending && onClose()} title="Новый проект" footer={<>
    <Button onClick={onClose} disabled={create.isPending}>Отмена</Button>
    <Button type="submit" form="new-project" variant="primary" loading={create.isPending} disabled={!ready}>Создать</Button>
  </>}>
    <form id="new-project" className="issue-form" onSubmit={(e) => { e.preventDefault(); if (ready) create.mutate(); }}>
      {!note && <Input ref={titleInput} label="Название" required value={title} onChange={(e) => setTitle(e.target.value)} placeholder="Сайт студии" />}
      <Input label="Ключ" required value={key} maxLength={10} onChange={(e) => setTypedKey(e.target.value.toUpperCase())}
        hint={`Начало номеров тикетов: ${validKey ? key : "WEB"}-1, ${validKey ? key : "WEB"}-2…`}
        error={key && !validKey ? "Латиница и цифры, с буквы: WEB, LUM2" : undefined} />
      {!!candidates.length && <Select label="Или добавить доску к существующему" value={note} onChange={(e) => setNote(e.target.value)}>
        <option value="">Нет, новый проект</option>
        {candidates.map((n) => <option key={n.path} value={n.path}>{n.title}</option>)}
      </Select>}
      {create.isError && <Alert tone="error" title="Проект не создан">{errorText(create.error)}</Alert>}
    </form>
  </Dialog>;
}
