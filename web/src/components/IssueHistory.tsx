import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import type { IssueEvent, TrackerProject } from "../api/types";
import { priorityOf, statusName, typeLabel } from "../lib/tracker";

const FIELD: Record<string, string> = {
  title: "Название", status: "Статус", issue_type: "Тип", priority: "Приоритет", assignee: "Исполнитель",
  reporter: "Автор", watchers: "Наблюдатели", labels: "Метки", epic: "Эпик", parent: "Родитель",
  sprint: "Спринт", start: "Начало", due: "Срок", estimate: "Оценка", points: "Story points",
  description: "Описание", comments: "Комментарии",
};

function shown(field: string | undefined, v: string | undefined, project?: TrackerProject): string {
  if (!v) return "—";
  if (field === "status") return statusName(project, v);
  if (field === "issue_type") return typeLabel(v);
  if (field === "priority") return priorityOf(v)?.label ?? v;
  return v;
}

function what(e: IssueEvent, project?: TrackerProject): React.ReactNode {
  if (e.kind === "created") return "создал(а) тикет";
  if (e.kind === "deleted") return "удалил(а) тикет";
  if (e.field === "comments") return Number(e.new) > Number(e.old || 0) ? "добавил(а) комментарий" : "удалил(а) комментарий";
  if (e.field === "description") return "изменил(а) описание";
  return <>{FIELD[e.field ?? ""] ?? e.field}: <del>{shown(e.field, e.old, project)}</del> → <ins>{shown(e.field, e.new, project)}</ins></>;
}

/**
 * IssueHistory lists who changed what. Edits made outside Lumen (Obsidian,
 * git, Nextcloud) have no author and say so.
 */
export default function IssueHistory({ issueKey, project }: { issueKey: string; project?: TrackerProject }) {
  const { data, isLoading } = useQuery({ queryKey: ["history", issueKey], queryFn: () => api.issueHistory(issueKey) });
  if (isLoading) return <p className="hint">Загрузка истории…</p>;
  const items = [...(data?.items ?? [])].reverse();
  if (!items.length) return <p className="hint">Изменений с начала учёта нет. История ведётся с момента, когда сервер впервые увидел тикет.</p>;
  return <ol className="issue-history">
    {items.map((e) => <li key={e.id}>
      <time dateTime={e.at}>{new Date(e.at).toLocaleString("ru-RU", { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" })}</time>
      <span><strong>{e.source === "external" ? "Вне Lumen" : e.actor || "Lumen"}</strong> {what(e, project)}</span>
    </li>)}
  </ol>;
}
