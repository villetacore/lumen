import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, Button, Dialog, IconButton, Select } from "@villetacore/velessoft-ui";
import { api } from "../api/client";
import type { NoteSummary } from "../api/types";
import { errorText } from "../lib/errors";
import { Link } from "../lib/navigation";
import { parentRef } from "../lib/notes";
import { comparePages, descendantsOf, type DropZone, placement, useCollapsed } from "../lib/tree";
import Icon from "./Icon";

type Row = { page: NoteSummary; depth: number; parent?: string; siblings: string[]; kids: string[] };
type Move = { id: string; parent: string; before?: string; after?: string };

// One drag at a time in a page, across every tree on it: documents are shown in
// several groups, each its own PageTree. The event's data is unreadable until the drop.
let dragged: string | null = null;

const parentNoteId = (n: NoteSummary) => {
  const r = parentRef(n.parent);
  return r?.kind === "note" ? r.id : undefined;
};

/**
 * PageTree draws documents as a tree. Pages are dragged between, before or into each
 * other; on the keyboard Alt+↑/↓ reorders and Alt+←/→ changes the level. "Переместить"
 * does the same from a list, which also works on touch screens.
 */
export default function PageTree({ pages, roots, rootRef, onCreate }: {
  /** Every document the reader can see; children are looked up here. */
  pages: NoteSummary[];
  roots: NoteSummary[];
  rootRef?: string;
  onCreate: (parent: string) => void;
}) {
  const qc = useQueryClient();
  const { collapsed, toggle } = useCollapsed("lumen.pages.collapsed");
  const [dragging, setDragging] = useState<string | null>(null);
  const [over, setOver] = useState<{ id: string; zone: DropZone } | null>(null);
  const [moving, setMoving] = useState<NoteSummary | null>(null);
  const [said, setSaid] = useState("");

  const kidsOf = useMemo(() => {
    const m = new Map<string, NoteSummary[]>();
    for (const p of pages) {
      const up = parentNoteId(p);
      if (up) m.set(up, [...(m.get(up) ?? []), p]);
    }
    for (const list of m.values()) list.sort(comparePages);
    return m;
  }, [pages]);

  const rows: Row[] = [];
  const seen = new Set<string>();
  const walk = (list: NoteSummary[], depth: number, parent?: string) => {
    const ids = list.map((p) => p.id);
    for (const page of list) {
      if (seen.has(page.id)) continue;
      seen.add(page.id);
      const kids = kidsOf.get(page.id) ?? [];
      rows.push({ page, depth, parent, siblings: ids, kids: kids.map((k) => k.id) });
      if (!collapsed.has(page.id)) walk(kids, depth + 1, `note:${page.id}`);
    }
  };
  walk([...roots].sort(comparePages), 0, rootRef);
  const byId = new Map(rows.map((r) => [r.page.id, r]));

  const move = useMutation({
    mutationFn: (m: Move) => api.movePage(m),
    onSuccess: () => qc.invalidateQueries(),
  });
  const under = (id: string) => descendantsOf(pages, id, (p) => p.id, parentNoteId);

  const dropTo = (dragId: string, target: Row, zone: DropZone) => {
    if (under(dragId).has(target.page.id)) return false;
    move.mutate({ id: dragId, ...placement(dragId, target.page.id, zone, target.parent, target.siblings, target.kids) });
    if (zone === "inside") toggle(target.page.id, true);
    return true;
  };

  const onKey = (e: React.KeyboardEvent, r: Row) => {
    if (!e.altKey || !["ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight"].includes(e.key)) return;
    e.preventDefault();
    const i = r.siblings.indexOf(r.page.id);
    const prev = byId.get(r.siblings[i - 1]);
    const next = byId.get(r.siblings[i + 1]);
    const up = r.parent ? byId.get(r.parent.slice("note:".length)) : undefined;
    const title = r.page.title;
    if (e.key === "ArrowUp" && prev) { dropTo(r.page.id, prev, "before"); setSaid(`«${title}» выше`); }
    if (e.key === "ArrowDown" && next) { dropTo(r.page.id, next, "after"); setSaid(`«${title}» ниже`); }
    if (e.key === "ArrowRight" && prev) { dropTo(r.page.id, prev, "inside"); setSaid(`«${title}» теперь в «${prev.page.title}»`); }
    if (e.key === "ArrowLeft" && up) { dropTo(r.page.id, up, "after"); setSaid(`«${title}» на уровень выше`); }
  };

  return <>
    {move.isError && <Alert tone="error" title="Страница не перемещена">{errorText(move.error)}</Alert>}
    <div role="tree" className="page-tree" aria-label="Дерево страниц">
      {rows.map((r) => {
        const { page: p } = r;
        const zoneClass = over?.id === p.id ? ` drop-${over.zone}` : "";
        return <div key={p.id} role="treeitem" aria-level={r.depth + 1} aria-selected={false}
          aria-expanded={r.kids.length ? !collapsed.has(p.id) : undefined}
          className={`page-row${zoneClass}${dragging === p.id ? " dragging" : ""}`}
          style={{ paddingLeft: 4 + r.depth * 20 }}
          draggable
          onDragStart={(e) => { e.dataTransfer.setData("text/plain", p.id); e.dataTransfer.effectAllowed = "move"; dragged = p.id; setDragging(p.id); }}
          onDragEnd={() => { dragged = null; setDragging(null); setOver(null); }}
          onDragOver={(e) => {
            if (!dragged || under(dragged).has(p.id)) return;
            e.preventDefault();
            const box = e.currentTarget.getBoundingClientRect();
            const y = (e.clientY - box.top) / box.height;
            setOver({ id: p.id, zone: y < 0.25 ? "before" : y > 0.75 ? "after" : "inside" });
          }}
          onDragLeave={() => setOver((o) => (o?.id === p.id ? null : o))}
          onDrop={(e) => {
            e.preventDefault();
            if (dragged && over?.id === p.id) dropTo(dragged, r, over.zone);
            dragged = null; setDragging(null); setOver(null);
          }}>
          {r.kids.length > 0
            ? <button type="button" className="tree-toggle" aria-label={`${collapsed.has(p.id) ? "Развернуть" : "Свернуть"} ${p.title}`} onClick={() => toggle(p.id)}>
                <Icon name="chevron" size={14} className={collapsed.has(p.id) ? "" : "is-open"} />
              </button>
            : <span className="tree-toggle-space" />}
          <Icon name="book" size={16} />
          <Link href={`/note/${p.path}`} onKeyDown={(e) => onKey(e, r)} aria-keyshortcuts="Alt+ArrowUp Alt+ArrowDown Alt+ArrowLeft Alt+ArrowRight">{p.title}</Link>
          {r.kids.length > 0 && <span className="count">{r.kids.length}</span>}
          <span className="page-row-actions">
            <IconButton size="sm" variant="ghost" label={`Подстраница: ${p.title}`} onClick={() => onCreate(`note:${p.id}`)}><Icon name="plus" size={14} /></IconButton>
            <IconButton size="sm" variant="ghost" label={`Переместить: ${p.title}`} onClick={() => setMoving(p)}><Icon name="move" size={14} /></IconButton>
          </span>
          <span className="path">{p.updated?.slice(0, 10)}</span>
        </div>;
      })}
    </div>
    <div className="sr-only" aria-live="polite">{said}</div>
    {moving && <MovePageDialog page={moving} pages={pages} onClose={() => setMoving(null)} />}
  </>;
}

/** MovePageDialog picks a new place from a list. */
function MovePageDialog({ page, pages, onClose }: { page: NoteSummary; pages: NoteSummary[]; onClose: () => void }) {
  const qc = useQueryClient();
  const [parent, setParent] = useState(page.parent ?? "");
  const blocked = descendantsOf(pages, page.id, (p) => p.id, parentNoteId);
  const move = useMutation({
    mutationFn: () => api.movePage({ id: page.id, parent }),
    onSuccess: () => { qc.invalidateQueries(); onClose(); },
  });
  const options = pages.filter((p) => !blocked.has(p.id)).sort((a, b) => a.title.localeCompare(b.title, "ru"));
  const { data: projects } = useQuery({ queryKey: ["notes", { type: "project" }], queryFn: () => api.notes({ type: "project" }) });
  return <Dialog open onClose={() => !move.isPending && onClose()} title={`Переместить «${page.title}»`} footer={<>
    <Button onClick={onClose} disabled={move.isPending}>Отмена</Button>
    <Button variant="primary" loading={move.isPending} onClick={() => move.mutate()}>Переместить</Button>
  </>}>
    <ParentOptions value={parent} onChange={setParent} pages={options} projects={projects?.items ?? []} />
    <p className="hint">Подстраницы переедут вместе со страницей. В закрытом проекте её увидят только его участники.</p>
    {move.isError && <Alert tone="error" title="Страница не перемещена">{errorText(move.error)}</Alert>}
  </Dialog>;
}

function ParentOptions({ value, onChange, pages, projects }: { value: string; onChange: (v: string) => void; pages: NoteSummary[]; projects: NoteSummary[] }) {
  const keep = value && ![...pages, ...projects].some((p) => `note:${p.id}` === value);
  return <Select label="Куда" value={value} onChange={(e) => onChange(e.target.value)}>
    <option value="">Общая документация, верхний уровень</option>
    {keep && <option value={value}>Текущее место</option>}
    {projects.length > 0 && <optgroup label="Проекты">
      {projects.map((p) => <option key={p.id} value={`note:${p.id}`}>{p.title}</option>)}
    </optgroup>}
    <optgroup label="Страницы">
      {pages.map((p) => <option key={p.id} value={`note:${p.id}`}>{p.title}</option>)}
    </optgroup>
  </Select>;
}
