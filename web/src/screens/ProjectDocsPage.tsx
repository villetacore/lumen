"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Alert, Button, EmptyState, Spinner } from "@villetacore/velessoft-ui";
import { api } from "../api/client";
import Crumbs from "../components/Crumbs";
import Icon from "../components/Icon";
import NewNoteDialog from "../components/NewNoteDialog";
import PageTree from "../components/PageTree";
import ProjectTabs from "../components/ProjectTabs";
import { errorText } from "../lib/errors";
import { useRouteParam } from "../lib/navigation";
import { useNoteIndex } from "../lib/notes";

/** ProjectDocsPage is a project's own tree of pages. */
export default function ProjectDocsPage() {
  const key = useRouteParam(1);
  const { data: project, error } = useQuery({ queryKey: ["tracker", "project", key], queryFn: () => api.trackerProject(key) });
  const notes = useNoteIndex();
  const [creating, setCreating] = useState<string | null>(null);
  if (error) return <Alert tone="error" title="Проект не найден">{errorText(error)}</Alert>;
  if (!project) return <Spinner label="Загрузка проекта" />;
  const root = `note:${project.note_id}`;
  const pages = notes.items.filter((n) => n.content_kind === "document");
  const roots = pages.filter((n) => n.parent === root);
  return <div className="page">
    <Crumbs items={[{ label: "Проекты", to: "/tracker" }, { label: `${project.key} · ${project.title}` }]} />
    <div className="page-head">
      <div><h1>{project.title}</h1><ProjectTabs projectKey={project.key} /></div>
      <div className="head-actions"><Button variant="primary" onClick={() => setCreating(root)}><Icon name="plus" size={16} />Страница</Button></div>
    </div>
    {notes.isLoading && <Spinner label="Загрузка страниц" />}
    {notes.error && <Alert tone="error" title="Страницы не загружены">{errorText(notes.error)}</Alert>}
    {!notes.isLoading && !notes.error && roots.length === 0 && <EmptyState icon={<Icon name="book" size={24} />} title="Страниц пока нет"
      description="Соберите здесь решения, регламенты и инструкции проекта. Страницы можно вкладывать друг в друга."
      actions={<Button variant="primary" onClick={() => setCreating(root)}>Создать страницу</Button>} />}
    {roots.length > 0 && <PageTree pages={pages} roots={roots} rootRef={root} onCreate={setCreating} />}
    {creating && <NewNoteDialog parent={creating} onClose={() => setCreating(null)} />}
  </div>;
}
