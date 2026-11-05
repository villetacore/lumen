"use client";

import { useState } from "react";
import { Link } from "../lib/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, Button, Dialog, EmptyState, IconButton, Select, Spinner } from "@villetacore/velessoft-ui";
import { api } from "../api/client";
import type { TrackerProject } from "../api/types";
import Icon from "../components/Icon";
import NewProjectDialog from "../components/NewProjectDialog";
import { errorText } from "../lib/errors";
import { projectUrl } from "../lib/tracker";
import { buildTree, descendantsOf, flattenTree } from "../lib/tree";

const keyOf = (p: TrackerProject) => p.key;
const parentOf = (p: TrackerProject) => p.parent;

/** TrackerPage shows the tree of projects: start one, nest it, move it elsewhere. */
export default function TrackerPage() {
  const [creating, setCreating] = useState<{ parent?: string } | null>(null);
  const [moving, setMoving] = useState<TrackerProject | null>(null);
  const { data, error, isLoading } = useQuery({ queryKey: ["tracker", "projects"], queryFn: api.trackerProjects });
  const { data: open } = useQuery({ queryKey: ["issues", { category: "open" }], queryFn: () => api.issues({}) });
  const openBy = new Map<string, number>();
  for (const is of open?.items ?? []) {
    if (is.status_category !== "done") openBy.set(is.project, (openBy.get(is.project) ?? 0) + 1);
  }
  const items = data?.items ?? [];
  const rows = flattenTree(buildTree(items, keyOf, parentOf, (a, b) => a.title.localeCompare(b.title, "ru")), new Set(), keyOf);
  const nestedOpen = (p: TrackerProject) => {
    let sum = 0;
    for (const k of descendantsOf(items, p.key, keyOf, parentOf)) if (k !== p.key) sum += openBy.get(k) ?? 0;
    return sum;
  };

  return <div className="page">
    <div className="page-head">
      <div><h1>Проекты</h1><span className="sub">Тикеты хранятся Markdown-файлами в папке проекта. Проекты можно вкладывать друг в друга.</span></div>
      <div className="head-actions"><Button variant="primary" onClick={() => setCreating({})}><Icon name="plus" size={16} />Проект</Button></div>
    </div>
    {isLoading && <Spinner label="Загрузка проектов" />}
    {error && <Alert tone="error" title="Проекты не загружены">{errorText(error)}</Alert>}
    {data && items.length === 0 && <EmptyState title="Проектов пока нет"
      description="Создайте проект: у него сразу будет доска для тикетов."
      actions={<Button variant="primary" onClick={() => setCreating({})}>Создать проект</Button>} />}
    {items.length > 0 && <ul className="project-tree">
      {rows.map(({ item: p, depth }) => {
        const nested = nestedOpen(p);
        return <li key={p.key} className="project-tree-row" style={{ marginLeft: depth * 24 }}>
          <Link href={`${projectUrl(p.key)}/board`} className="project-tree-link">
            <span className="issue-key">{p.key}</span>
            <strong>{p.title}</strong>
            <span className="sub">{openBy.get(p.key) ?? 0} открыто{nested > 0 && ` · ещё ${nested} в подпроектах`}</span>
          </Link>
          <span className="project-tree-actions">
            <IconButton size="sm" variant="ghost" label={`Подпроект: ${p.title}`} onClick={() => setCreating({ parent: p.key })}><Icon name="plus" size={14} /></IconButton>
            <IconButton size="sm" variant="ghost" label={`Переместить: ${p.title}`} onClick={() => setMoving(p)}><Icon name="move" size={14} /></IconButton>
          </span>
        </li>;
      })}
    </ul>}
    {creating && <NewProjectDialog parent={creating.parent} onClose={() => setCreating(null)} />}
    {moving && <MoveProjectDialog project={moving} all={items} onClose={() => setMoving(null)} />}
  </div>;
}

function MoveProjectDialog({ project, all, onClose }: { project: TrackerProject; all: TrackerProject[]; onClose: () => void }) {
  const qc = useQueryClient();
  const [parent, setParent] = useState(project.parent ?? "");
  const blocked = descendantsOf(all, project.key, keyOf, parentOf);
  const move = useMutation({
    mutationFn: () => api.patchTrackerProject(project.key, { parent, revision: project.revision }),
    onSuccess: () => { qc.invalidateQueries(); onClose(); },
  });
  return <Dialog open onClose={() => !move.isPending && onClose()} title={`Переместить ${project.key}`} footer={<>
    <Button onClick={onClose} disabled={move.isPending}>Отмена</Button>
    <Button variant="primary" loading={move.isPending} disabled={parent === (project.parent ?? "")} onClick={() => move.mutate()}>Переместить</Button>
  </>}>
    <Select label="Входит в проект" value={parent} onChange={(e) => setParent(e.target.value)}>
      <option value="">Нет, самостоятельный</option>
      {all.filter((p) => !blocked.has(p.key)).map((p) => <option key={p.key} value={p.key}>{p.key} · {p.title}</option>)}
    </Select>
    <p className="hint">Тикеты и ссылки не изменятся. Если у проекта нет своего списка участников, доступ станет как у нового родителя: такой перенос может сделать только его администратор.</p>
    {move.isError && <Alert tone="error" title="Проект не перемещён">{errorText(move.error)}</Alert>}
  </Dialog>;
}
