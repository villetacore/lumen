import { Link } from "react-router-dom";

export type Crumb = { label: string; to?: string };

/** Crumbs renders Velessoft breadcrumbs with router links, so navigation keeps unsaved-draft guards. */
export default function Crumbs({ items }: { items: Crumb[] }) {
  return (
    <nav aria-label="Навигационная цепочка" className="vs-breadcrumbs">
      <ol>
        {items.map((item, i) => {
          const last = i === items.length - 1;
          return (
            <li key={i}>
              {last || !item.to ? <span aria-current={last ? "page" : undefined}>{item.label}</span> : <Link to={item.to}>{item.label}</Link>}
            </li>
          );
        })}
      </ol>
    </nav>
  );
}
