import { Link, useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { EmptyState, Spinner } from "@villetacore/velessoft-ui";
import { api } from "../api/client";
import TaskRow from "../components/TaskRow";
import Icon from "../components/Icon";
import { noteLabel, useNoteIndex } from "../lib/notes";

export default function SearchPage() {
  const [params] = useSearchParams();
  const q = params.get("q") ?? "";
  const notes = useNoteIndex();

  const { data, isLoading } = useQuery({
    queryKey: ["search", q],
    queryFn: () => api.search(q),
    enabled: q.length > 0,
  });

  const { data: tasks } = useQuery({
    queryKey: ["tasks", "search", q],
    queryFn: () => api.tasks({ q, status: ["todo", "doing"], limit: 50 }),
    enabled: q.length > 0,
  });

  return (
    <div className="page">
      <div className="page-head">
        <div>
          <span className="kicker">Поиск / {(data?.total ?? 0) + (tasks?.total ?? 0)}</span>
          <h1>«{q}»</h1>
        </div>
      </div>

      {tasks && tasks.items.length > 0 && (
        <section className="list">
          <div className="group-head">Открытые задачи <span className="count">{tasks.total}</span></div>
          {tasks.items.map((t) => (
            <TaskRow key={t.id} task={t} noteTitle={noteLabel(notes.byPath.get(t.note_path), t.note_path)} />
          ))}
        </section>
      )}

      {isLoading && <Spinner label="Ищем" />}
      {!isLoading && data?.items.length === 0 && !tasks?.items.length && (
        <EmptyState icon={<Icon name="search" size={24} />} title="Ничего не найдено" description="Попробуйте другое слово или часть названия." />
      )}

      {!!data?.items.length && (
        <section className="list">
          <div className="group-head">Материалы <span className="count">{data.total}</span></div>
          <div className="note-list">
            {data.items.map((hit) => (
              <div key={hit.path} className="note-row" style={{ display: "block" }}>
                <Link to={`/note/${hit.path}`}>{hit.title}</Link>
                <div className="hint mono">{hit.snippet}</div>
              </div>
            ))}
          </div>
        </section>
      )}
    </div>
  );
}
