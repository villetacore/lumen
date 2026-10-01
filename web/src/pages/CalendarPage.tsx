import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../api/client";
import type { CalendarEvent } from "../api/types";
import { DOW_SHORT, addDays, iso, monthTitle, startOfWeek, today } from "../lib/date";

export default function CalendarPage() {
  const [cursor, setCursor] = useState(() => {
    const n = today();
    return new Date(n.getFullYear(), n.getMonth(), 1);
  });
  const [dropTarget, setDropTarget] = useState<string | null>(null);
  const qc = useQueryClient();

  const gridStart = startOfWeek(cursor);
  const days = useMemo(
    () => Array.from({ length: 42 }, (_, i) => addDays(gridStart, i)),
    [gridStart.getTime()],
  );

  const from = iso(days[0]);
  const to = iso(days[days.length - 1]);

  const { data, isLoading } = useQuery({
    queryKey: ["calendar", from, to],
    queryFn: () => api.calendar(from, to),
  });

  const move = useMutation({
    mutationFn: ({ ref, date }: { ref: string; date: string }) => api.moveEvent(ref, date),
    onSuccess: () => qc.invalidateQueries(),
  });

  const byDay = new Map<string, CalendarEvent[]>();
  for (const e of data?.events ?? []) {
    const key = e.start.slice(0, 10);
    const list = byDay.get(key) ?? [];
    list.push(e);
    byDay.set(key, list);
  }

  const shiftMonth = (n: number) =>
    setCursor(new Date(cursor.getFullYear(), cursor.getMonth() + n, 1));

  return (
    <>
      <div className="page-head">
        <h1>Календарь</h1>
        <span className="sub">задачи и события рядом, а не в разных приложениях</span>
      </div>

      <div className="cal-toolbar">
        <button className="btn" onClick={() => shiftMonth(-1)} aria-label="Предыдущий месяц">
          ‹
        </button>
        <strong style={{ minWidth: 150, textAlign: "center" }}>{monthTitle(cursor)}</strong>
        <button className="btn" onClick={() => shiftMonth(1)} aria-label="Следующий месяц">
          ›
        </button>
        <button
          className="btn"
          onClick={() => {
            const n = today();
            setCursor(new Date(n.getFullYear(), n.getMonth(), 1));
          }}
        >
          Сегодня
        </button>
        {(isLoading || move.isPending) && <span className="spinner">…</span>}
      </div>

      <div className="cal-grid">
        {DOW_SHORT.map((d) => (
          <div key={d} className="cal-dow">
            {d}
          </div>
        ))}

        {days.map((d) => {
          const key = iso(d);
          const items = byDay.get(key) ?? [];
          const classes = [
            "cal-day",
            d.getMonth() === cursor.getMonth() ? "" : "other",
            key === iso(today()) ? "today" : "",
            dropTarget === key ? "drop-target" : "",
          ]
            .filter(Boolean)
            .join(" ");

          return (
            <div
              key={key}
              className={classes}
              onDragOver={(e) => {
                e.preventDefault();
                setDropTarget(key);
              }}
              onDragLeave={() => setDropTarget((t) => (t === key ? null : t))}
              onDrop={(e) => {
                e.preventDefault();
                setDropTarget(null);
                const ref = e.dataTransfer.getData("text/lumen-ref");
                if (ref.startsWith("task:")) move.mutate({ ref, date: key });
              }}
            >
              <span className="cal-daynum">{d.getDate()}</span>
              {items.map((e) => (
                <div
                  key={e.id}
                  className={`cal-item source-${e.source} ${e.status === "done" ? "is-done" : ""}`}
                  draggable={e.source === "task"}
                  onDragStart={(ev) => ev.dataTransfer.setData("text/lumen-ref", e.id)}
                  title={[e.title, e.location, e.calendar].filter(Boolean).join(" · ")}
                >
                  {!e.all_day && <span style={{ opacity: 0.7 }}>{e.start.slice(11, 16)} </span>}
                  {e.path ? (
                    <Link to={`/note/${e.path}`}>{e.title}</Link>
                  ) : e.url ? (
                    <a href={e.url} target="_blank" rel="noreferrer">
                      {e.title}
                    </a>
                  ) : (
                    e.title
                  )}
                </div>
              ))}
            </div>
          );
        })}
      </div>
    </>
  );
}
