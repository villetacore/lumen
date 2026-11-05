"use client";

import { useState } from "react";
import { Link } from "../lib/navigation";
import { useQueries } from "@tanstack/react-query";
import { Alert, Badge, Button, EmptyState, IconButton, Input, SegmentedControl, Spinner, Switch } from "@villetacore/velessoft-ui";
import { api } from "../api/client";
import type { NoteSummary } from "../api/types";
import Icon, { type IconName } from "../components/Icon";
import NewNoteDialog from "../components/NewNoteDialog";
import { errorText } from "../lib/errors";
import { parentRef, useNoteIndex } from "../lib/notes";

type Group = { key: string; label: string; icon: IconName; to?: string; parent?: string; order: number; docs: NoteSummary[] };
type Sort = "title" | "updated";

/** Notes that read as knowledge, not as a journal or a calendar entry. */
const KNOWLEDGE_TYPES = new Set(["note", "area"]);

/**
 * DocsPage is the technical documentation section: every document grouped by
 * the project or task it belongs to, with a table of contents. Plain knowledge
 * notes from the vault can be shown alongside, so existing Markdown is not lost.
 */
export default function DocsPage() {
  const notes = useNoteIndex();
  const [filter, setFilter] = useState("");
  const [sort, setSort] = useState<Sort>("title");
  const [withNotes, setWithNotes] = useState(false);
  const [creating, setCreating] = useState<{ parent?: string } | null>(null);

  const needle = filter.trim().toLowerCase();
  const docs = notes.items.filter((n) =>
    (n.content_kind === "document" || (withNotes && !n.content_kind && !n.parent && KNOWLEDGE_TYPES.has(n.type) && !n.path.startsWith("00-inbox/"))) &&
    (!needle || n.title.toLowerCase().includes(needle) || n.tags.some((t) => t.toLowerCase().includes(needle))),
  );

  // Documents attached to a task need the task text for their group title.
  const taskIds = [...new Set(docs.map((d) => parentRef(d.parent)).filter((p) => p?.kind === "task").map((p) => p!.id))];
  const tasks = useQueries({
    queries: taskIds.map((id) => ({ queryKey: ["task", id], queryFn: () => api.task(id), retry: false })),
  });
  const taskById = new Map(taskIds.map((id, i) => [id, tasks[i]]));

  const groups = new Map<string, Group>();
  for (const d of docs) {
    const ref = parentRef(d.parent);
    let g: Omit<Group, "docs">;
    if (!ref) {
      g = d.content_kind === "document"
        ? { key: "general", label: "Общая документация", icon: "book", order: 0 }
        : { key: "knowledge", label: "Заметки базы знаний", icon: "file", order: 3 };
    } else if (ref.kind === "note") {
      const p = notes.byId.get(ref.id);
      g = { key: d.parent!, label: p?.title ?? "Удалённый раздел", icon: p?.type === "project" ? "folder" : "file",
        to: p ? `/note/${p.path}` : undefined, parent: d.parent, order: p?.type === "project" ? 1 : 2 };
    } else {
      const q = taskById.get(ref.id);
      const t = q?.data;
      g = { key: d.parent!, label: t ? t.text : q?.isError ? "Удалённая задача" : "Задача", icon: "check",
        to: t ? `/task/${encodeURIComponent(t.id)}` : undefined, parent: d.parent, order: 2 };
    }
    const group = groups.get(g.key) ?? { ...g, docs: [] };
    group.docs.push(d);
    groups.set(g.key, group);
  }
  const ordered = [...groups.values()].sort((a, b) => a.order - b.order || a.label.localeCompare(b.label, "ru"));
  for (const g of ordered) {
    g.docs.sort(sort === "updated"
      ? (a, b) => (b.updated ?? "").localeCompare(a.updated ?? "")
      : (a, b) => a.title.localeCompare(b.title, "ru"));
  }
  const total = notes.items.filter((n) => n.content_kind === "document").length;

  return (
    <div className="page">
      <div className="page-head">
        <div>
          <span className="kicker">Знания / {total} документов</span>
          <h1>Документация</h1>
          <span className="sub">Технические документы, решения и инструкции по проектам и задачам.</span>
        </div>
        <div className="head-actions">
          <Button variant="primary" onClick={() => setCreating({})}><Icon name="plus" size={16} />Новый документ</Button>
        </div>
      </div>

      <div className="toolbar">
        <div className="grow">
          <Input label="Поиск" type="search" value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Название или тег" startAddon={<Icon name="search" size={16} />} />
        </div>
        <SegmentedControl label="Порядок" value={sort} onChange={(v) => setSort(v as Sort)}
          options={[{ value: "title", label: "По алфавиту" }, { value: "updated", label: "Недавние" }]} />
        <Switch label="Заметки базы знаний" checked={withNotes} onChange={(e) => setWithNotes(e.target.checked)} />
      </div>

      {notes.isLoading && <Spinner label="Загрузка документации" />}
      {notes.error && <Alert tone="error" title="Не удалось загрузить документацию">{errorText(notes.error)}</Alert>}
      {!notes.isLoading && !notes.error && ordered.length === 0 && (
        <EmptyState
          icon={<Icon name="book" size={24} />}
          title={needle ? "Ничего не найдено" : "Документации пока нет"}
          description={needle ? "Измените запрос или включите заметки базы знаний." : "Создайте первый документ — общий или в разделе проекта. Документы задач тоже появятся здесь."}
          actions={needle ? <Button onClick={() => setFilter("")}>Сбросить поиск</Button> : <Button variant="primary" onClick={() => setCreating({})}>Новый документ</Button>}
        />
      )}

      {ordered.length > 0 && (
        <div className="docs-layout">
          <nav className="docs-tree" aria-label="Разделы документации">
            <div className="nav-title">Разделы</div>
            {ordered.map((g) => (
              <a key={g.key} className="nav-item" href={`#docs-${encodeURIComponent(g.key)}`}>
                <span className="icon"><Icon name={g.icon} size={16} /></span>{g.label}<span className="count">{g.docs.length}</span>
              </a>
            ))}
          </nav>
          <div className="list">
            {ordered.map((g) => (
              <section key={g.key} id={`docs-${encodeURIComponent(g.key)}`} className="docs-group list-group" aria-label={g.label}>
                <div className="group-head">
                  <Icon name={g.icon} size={16} />
                  {g.to ? <Link href={g.to}>{g.label}</Link> : g.label}
                  <span className="count">{g.docs.length}</span>
                  {g.key !== "knowledge" && (
                    <IconButton size="sm" variant="ghost" label={`Новый документ: ${g.label}`} style={{ marginLeft: "auto" }}
                      onClick={() => setCreating({ parent: g.parent })}>
                      <Icon name="plus" size={14} />
                    </IconButton>
                  )}
                </div>
                <div className="note-list">
                  {g.docs.map((d) => (
                    <div key={d.id} className="note-row">
                      <Icon name={d.content_kind === "document" ? "book" : "file"} size={16} />
                      <Link href={`/note/${d.path}`}>{d.title}</Link>
                      {d.open_tasks > 0 && <Badge tone="info">{d.open_tasks} задач</Badge>}
                      {d.tags.slice(0, 3).map((t) => <Link key={t} className="md-tag" href={`/notes?tag=${encodeURIComponent(t)}`}>#{t}</Link>)}
                      <span className="path">{d.updated?.slice(0, 10)}</span>
                    </div>
                  ))}
                </div>
              </section>
            ))}
          </div>
        </div>
      )}

      {creating && <NewNoteDialog parent={creating.parent} onClose={() => setCreating(null)} />}
    </div>
  );
}
