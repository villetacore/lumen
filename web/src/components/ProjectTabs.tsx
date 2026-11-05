import { NavLink, useLocation } from "../lib/navigation";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import { projectUrl } from "../lib/tracker";

/** ProjectTabs switches between a project's views; those with issues share the filter. */
export default function ProjectTabs({ projectKey }: { projectKey: string }) {
  const { search } = useLocation();
  const { data: project } = useQuery({ queryKey: ["tracker", "project", projectKey], queryFn: () => api.trackerProject(projectKey) });
  const base = projectUrl(projectKey);
  const tab = ({ isActive }: { isActive: boolean }) => "project-tab" + (isActive ? " active" : "");
  return <nav className="project-tabs" aria-label="Вид проекта">
    <NavLink href={`${base}/board${search}`} className={tab}>Доска</NavLink>
    <NavLink end href={base + search} className={tab}>Список</NavLink>
    <NavLink href={`${base}/backlog`} className={tab}>Бэклог</NavLink>
    <NavLink href={`${base}/timeline${search}`} className={tab}>Таймлайн</NavLink>
    {project && <NavLink href={`/note/${project.path}`} className={tab}>О проекте</NavLink>}
    <NavLink href={`${base}/access`} className={tab}>Доступ</NavLink>
  </nav>;
}
