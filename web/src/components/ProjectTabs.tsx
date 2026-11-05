import { Link, NavLink, useLocation } from "../lib/navigation";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import { ancestorsOf } from "../lib/tree";
import { projectUrl } from "../lib/tracker";

/** ProjectTabs switches between a project's views; those with issues share the filter. */
export default function ProjectTabs({ projectKey }: { projectKey: string }) {
  const { search } = useLocation();
  const { data: project } = useQuery({ queryKey: ["tracker", "project", projectKey], queryFn: () => api.trackerProject(projectKey) });
  const { data: all } = useQuery({ queryKey: ["tracker", "projects"], queryFn: api.trackerProjects });
  const base = projectUrl(projectKey);
  const list = all?.items ?? [];
  const ancestors = ancestorsOf(list, project?.parent);
  const children = list.filter((p) => p.parent === projectKey);
  const tab = ({ isActive }: { isActive: boolean }) => "project-tab" + (isActive ? " active" : "");
  return <>
  {(ancestors.length > 0 || children.length > 0) && <div className="project-family">
    {ancestors.length > 0 && <span>Входит в: {ancestors.map((p, i) => <span key={p.key}>{i > 0 && " / "}<Link href={`${projectUrl(p.key)}/board`}>{p.title}</Link></span>)}</span>}
    {children.length > 0 && <span>Подпроекты: {children.map((p, i) => <span key={p.key}>{i > 0 && ", "}<Link href={`${projectUrl(p.key)}/board`}>{p.title}</Link></span>)}</span>}
  </div>}
  <nav className="project-tabs" aria-label="Вид проекта">
    <NavLink href={`${base}/board${search}`} className={tab}>Доска</NavLink>
    <NavLink end href={base + search} className={tab}>Список</NavLink>
    <NavLink href={`${base}/backlog`} className={tab}>Бэклог</NavLink>
    <NavLink href={`${base}/timeline${search}`} className={tab}>Таймлайн</NavLink>
    <NavLink href={`${base}/docs`} className={tab}>Страницы</NavLink>
    <NavLink href={`${base}/automations`} className={tab}>Автоматизации</NavLink>
    {project && <NavLink href={`/note/${project.path}`} className={tab}>О проекте</NavLink>}
    <NavLink href={`${base}/access`} className={tab}>Доступ</NavLink>
  </nav>
  </>;
}
