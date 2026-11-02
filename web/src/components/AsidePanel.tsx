import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import { DOW_SHORT, addDays, iso, monthTitle, startOfWeek, today } from "../lib/date";

/** MiniCalendar highlights the days of the current month that have anything on them. */
function MiniCalendar({ busy }: { busy: Set<string> }) {
  const now = today();
  const first = new Date(now.getFullYear(), now.getMonth(), 1);
  const start = startOfWeek(first);
  const days = Array.from({ length: 42 }, (_, i) => addDays(start, i));

  return (
    <div className="mini-cal">
      {DOW_SHORT.map((d) => (
        <div key={d} className="dow">
          {d}
        </div>
      ))}
      {days.map((d) => {
        const key = iso(d);
        const classes = [
          "d",
          d.getMonth() === now.getMonth() ? "" : "other",
          key === iso(now) ? "today" : "",
          busy.has(key) ? "has" : "",
        ]
          .filter(Boolean)
          .join(" ");
        return (
          <div key={key} className={classes} title={key}>
            {d.getDate()}
          </div>
        );
      })}
    </div>
  );
}

export default function AsidePanel() {
  const now = today();
  const from = iso(new Date(now.getFullYear(), now.getMonth(), 1));
  const to = iso(new Date(now.getFullYear(), now.getMonth() + 1, 0));

  const { data: agenda } = useQuery({
    queryKey: ["calendar", from, to],
    queryFn: () => api.calendar(from, to),
  });

  const busy = new Set((agenda?.events ?? []).map((e) => e.start.slice(0, 10)));
  const todayEvents = (agenda?.events ?? []).filter((e) => e.start.slice(0, 10) === iso(now));

  return (
    <aside className="card aside" style={{ padding: 16 }}>
      <div className="nav-title" style={{ padding: 0 }}>{monthTitle(now)}</div>
      <MiniCalendar busy={busy} />
      <div className="nav-title" style={{ padding: 0 }}>Повестка дня</div>
      {todayEvents.length === 0 && <div className="hint">На сегодня событий нет</div>}
      {todayEvents.map((e) => (
        <div key={e.id} className="agenda-item">
          <time>{e.all_day ? "весь день" : e.start.slice(11, 16)}</time>
          {e.path ? <Link to={`/note/${e.path}`}>{e.title}</Link> : <span>{e.title}</span>}
        </div>
      ))}
    </aside>
  );
}
