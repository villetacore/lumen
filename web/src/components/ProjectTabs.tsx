import { NavLink, useLocation } from "react-router-dom";
import { projectUrl } from "../lib/tracker";

/** ProjectTabs switches between a project's list and board, keeping the filter. */
export default function ProjectTabs({ projectKey }: { projectKey: string }) {
  const { search } = useLocation();
  const base = projectUrl(projectKey);
  const tab = ({ isActive }: { isActive: boolean }) => "project-tab" + (isActive ? " active" : "");
  return <nav className="project-tabs" aria-label="Вид проекта">
    <NavLink end to={base + search} className={tab}>Список</NavLink>
    <NavLink to={`${base}/board${search}`} className={tab}>Доска</NavLink>
  </nav>;
}
