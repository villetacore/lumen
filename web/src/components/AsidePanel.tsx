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
    <aside className="aside">
      <div className="section-head" style={{ marginTop: 0 }}>
        {monthTitle(now)}
      </div>
      <MiniCalendar busy={busy} />

      <div className="section-head">Повестка дня</div>
      {todayEvents.length === 0 && <div style={{ color: "var(--text-faint)" }}>на сегодня пусто</div>}
      {todayEvents.map((e) => (
        <div key={e.id} style={{ display: "flex", gap: 8, padding: "2px 0" }}>
          <span style={{ color: "var(--text-faint)", fontVariantNumeric: "tabular-nums" }}>
            {e.all_day ? "—" : e.start.slice(11, 16)}
          </span>
          {e.path ? (
            <Link to={`/note/${e.path}`}>{e.title}</Link>
          ) : (
            <span>{e.title}</span>
          )}
        </div>
      ))}

      <div className="section-head">Подсказки</div>
      <div style={{ color: "var(--text-dim)", fontSize: 12, lineHeight: 1.8 }}>
        <div>
          <kbd>Ctrl K</kbd> поиск
        </div>
        <div>
          <code>!A</code> приоритет в быстром вводе
        </div>
        <div>
          <code>@завтра</code> срок в быстром вводе
        </div>
        <div>Перетащите задачу в календаре, чтобы перенести её</div>
      </div>
    </aside>
  );
}
