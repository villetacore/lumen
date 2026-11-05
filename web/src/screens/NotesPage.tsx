"use client";

import Icon from "../components/Icon";
import { useState } from "react";
import { Link, Navigate, useQueryParams } from "../lib/navigation";
import { useQuery } from "@tanstack/react-query";
import { Alert, Badge, Button, EmptyState, Input, SegmentedControl, Spinner, Tag } from "@villetacore/velessoft-ui";
import { api } from "../api/client";
import type { NoteSummary } from "../api/types";
import NewNoteDialog from "../components/NewNoteDialog";
import { errorText } from "../lib/errors";

type Sort = "folder" | "updated";

const KIND_LABEL: Record<string, string> = { document: "документ", entry: "запись" };
const TYPE_LABEL: Record<string, string> = { project: "проект", daily: "день", meeting: "встреча", area: "область", event: "событие" };

type View = "notes" | "meetings" | "imported" | "all";
const VIEWS: { value: View; label: string }[] = [
  { value: "notes", label: "Заметки" },
  { value: "meetings", label: "Встречи и дни" },
  { value: "imported", label: "Из сервисов" },
  { value: "all", label: "Все файлы" },
];
const IMPORTED = "70-integrations/";

/** Tickets, projects and documents have their own places; "Все файлы" still shows them. */
function inView(n: NoteSummary, view: View): boolean {
  if (view === "all") return true;
  const imported = n.path.startsWith(IMPORTED);
  if (view === "imported") return imported;
  if (imported) return false;
  if (view === "meetings") return n.type === "meeting" || n.type === "daily";
  return (n.type === "note" || n.type === "area") && n.content_kind !== "document";
}

export default function NotesPage() {
  const [params, setParams] = useQueryParams();
  const tag = params.get("tag") ?? undefined;
  const [filter, setFilter] = useState("");
  const [sort, setSort] = useState<Sort>("folder");
  const [creating, setCreating] = useState(false);
  const view = (VIEWS.some((v) => v.value === params.get("view")) ? params.get("view") : "notes") as View;

  const { data, isLoading, error } = useQuery({
    queryKey: ["notes", { tag }],
    queryFn: () => api.notes({ tag: tag ? [tag] : undefined, limit: 2000 }),
  });

  // The old documentation filter lives on its own page now.
  if (params.get("section") === "documents") return <Navigate href="/docs" />;

  const needle = filter.trim().toLowerCase();
  const items = (data?.items ?? []).filter((n) =>
    // A tag asks for everything that carries it; the views sort out the rest.
    n.content_kind !== "description" && (tag ? true : inView(n, view)) && (!needle || n.title.toLowerCase().includes(needle)),
  );

  return (
    <div className="page">
      <div className="page-head">
        <div>
          <h1>{tag ? `#${tag}` : "Заметки"}</h1>
          <span className="sub">Личные и рабочие записи. Тикеты живут в проектах, а документы — в документации.</span>
        </div>
        <div className="head-actions">
          <Button variant="primary" onClick={() => setCreating(true)}><Icon name="plus" size={16} />Новая заметка</Button>
        </div>
      </div>

      <div className="toolbar">
        <div className="grow">
          <Input label="Поиск" type="search" value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Найти по названию" startAddon={<Icon name="search" size={16} />} />
        </div>
        {!tag && <SegmentedControl label="Что показать" value={view} options={VIEWS} onChange={(v) => setParams(v === "notes" ? {} : { view: v }, { replace: true })} />}
        <SegmentedControl label="Порядок" value={sort} onChange={(v) => setSort(v as Sort)}
          options={[{ value: "folder", label: "По разделам" }, { value: "updated", label: "Недавние" }]} />
        {tag && <Tag onRemove={() => setParams({})} removeLabel={`Сбросить тег ${tag}`}>#{tag}</Tag>}
      </div>

      {isLoading && <Spinner label="Загрузка заметок" />}
      {error && <Alert tone="error" title="Не удалось загрузить заметки">{errorText(error)}</Alert>}
      {!isLoading && !error && items.length === 0 && (
        <EmptyState icon={<Icon name="file" size={24} />}
          title={needle || tag ? "Ничего не найдено" : "Заметок пока нет"}
          description={needle || tag ? "Измените запрос или сбросьте тег." : "Создайте документ или проект — они сохранятся как Markdown в vault."} />
      )}

      {items.length > 0 && (sort === "updated" ? (
        <div className="list">
          <div className="note-list">
            {[...items].sort((a, b) => (b.updated ?? "").localeCompare(a.updated ?? "")).map((n) => <NoteRow key={n.path} note={n} />)}
          </div>
        </div>
      ) : (
        <div className="list">
          {groupByFolder(items).map(([folder, notes]) => (
            <section key={folder} className="list-group" aria-label={folder}>
              <div className="group-head">{folder} <span className="count">{notes.length}</span></div>
              <div className="note-list">{notes.map((n) => <NoteRow key={n.path} note={n} />)}</div>
            </section>
          ))}
        </div>
      ))}

      {creating && <NewNoteDialog contentKind="entry" onClose={() => setCreating(false)} />}
    </div>
  );
}

function NoteRow({ note: n }: { note: NoteSummary }) {
  const kind = n.content_kind ? KIND_LABEL[n.content_kind] : TYPE_LABEL[n.type];
  return (
    <div className="note-row">
      <Icon name={n.type === "project" ? "folder" : n.content_kind === "document" ? "book" : "file"} size={16} />
      <Link href={`/note/${n.path}`}>{n.title}</Link>
      {kind && <Badge tone={n.type === "project" ? "info" : "neutral"}>{kind}</Badge>}
      {n.open_tasks > 0 && <Badge>{n.open_tasks} задач</Badge>}
      {n.tags.slice(0, 3).map((t) => (
        <Link key={t} className="md-tag" href={`/notes?tag=${encodeURIComponent(t)}`}>#{t}</Link>
      ))}
      <span className="path">{n.updated ? n.updated.slice(0, 10) : ""}</span>
    </div>
  );
}

const FOLDER_ORDER = ["Проекты", "Документация", "Рабочие заметки", "Встречи", "Дневные заметки", "Другие заметки"];

function groupByFolder(notes: NoteSummary[]): [string, NoteSummary[]][] {
  const groups = new Map<string, NoteSummary[]>();
  for (const n of notes) {
    const folder = n.type === "project" ? "Проекты" : n.content_kind === "document" ? "Документация"
      : n.content_kind === "entry" ? "Рабочие заметки" : n.type === "meeting" ? "Встречи" : n.type === "daily" ? "Дневные заметки" : "Другие заметки";
    const list = groups.get(folder) ?? [];
    list.push(n);
    groups.set(folder, list);
  }
  for (const list of groups.values()) list.sort((a, b) => a.title.localeCompare(b.title, "ru"));
  return [...groups.entries()].sort(([a], [b]) => FOLDER_ORDER.indexOf(a) - FOLDER_ORDER.indexOf(b));
}
