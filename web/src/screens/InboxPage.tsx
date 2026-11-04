"use client";

import { useState } from "react";
import { Link, useNavigate } from "../lib/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, Button, EmptyState, SegmentedControl, Spinner } from "@villetacore/velessoft-ui";
import { api } from "../api/client";
import type { AppNotification } from "../api/types";
import Icon from "../components/Icon";
import { errorText } from "../lib/errors";
import { issueUrl } from "../lib/tracker";

const VERB: Record<AppNotification["kind"], string> = {
  assigned: "назначил(а) вам",
  mentioned: "упомянул(а) вас в",
  commented: "прокомментировал(а)",
  status: "сменил(а) статус",
};

function when(at: string): string {
  const d = new Date(at);
  const today = new Date();
  const sameDay = d.toDateString() === today.toDateString();
  return d.toLocaleString("ru-RU", sameDay ? { hour: "2-digit", minute: "2-digit" } : { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" });
}

/** InboxPage lists the caller's notifications; opening one marks it read. */
export default function InboxPage() {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [unreadOnly, setUnreadOnly] = useState(false);
  const { data: me } = useQuery({ queryKey: ["me"], queryFn: api.me });
  const { data, error, isLoading } = useQuery({ queryKey: ["notifications", unreadOnly], queryFn: () => api.notifications(unreadOnly), enabled: !!me?.username });
  const read = useMutation({
    mutationFn: (ids?: number[]) => api.readNotifications(ids),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["notifications"] }),
  });
  const open = (n: AppNotification) => {
    if (!n.read) read.mutate([n.id]);
    navigate(issueUrl(n.issue) + (n.ref && n.kind !== "status" ? `#${n.ref}` : ""));
  };

  return <div className="page">
    <div className="page-head">
      <div><span className="kicker">Трекер</span><h1>Уведомления</h1>
        <span className="sub">Назначения, упоминания, комментарии и смена статуса в тикетах, за которыми вы следите.</span></div>
      <div className="head-actions">
        <SegmentedControl label="Показывать" value={unreadOnly ? "unread" : "all"} onChange={(v) => setUnreadOnly(v === "unread")}
          options={[{ value: "all", label: "Все" }, { value: "unread", label: "Непрочитанные" }]} />
        {!!data?.unread && <Button onClick={() => read.mutate(undefined)} loading={read.isPending}>Прочитать все</Button>}
      </div>
    </div>
    {me && !me.username && <Alert tone="info" title="Уведомления — для вошедших через Keycloak">
      В {me.mode === "token" ? "режиме токена" : "локальном режиме"} нет пользователя, которому их адресовать.
    </Alert>}
    {isLoading && <Spinner label="Загрузка уведомлений" />}
    {error && <Alert tone="error" title="Уведомления не загружены">{errorText(error)}</Alert>}
    {data && data.items.length === 0 && <EmptyState title={unreadOnly ? "Всё прочитано" : "Уведомлений нет"}
      description="Они появятся, когда вам назначат тикет, упомянут вас через @логин или ответят в тикете, за которым вы следите." />}
    {!!data?.items.length && <ul className="inbox">
      {data.items.map((n) => <li key={n.id} className={`inbox-item${n.read ? "" : " is-unread"}`}>
        <button className="inbox-open" onClick={() => open(n)}>
          <span className="inbox-dot" aria-hidden="true" />
          <span className="inbox-text">
            <strong>{n.actor || "Кто-то"}</strong> {VERB[n.kind]} <span className="issue-key">{n.issue}</span> «{n.title}»
            {n.kind === "status" && n.ref && <> → {n.ref}</>}
          </span>
          <time dateTime={n.created_at}>{when(n.created_at)}</time>
          {!n.read && <span className="sr-only">не прочитано</span>}
        </button>
      </li>)}
    </ul>}
    <p className="hint"><Icon name="eye" size={14} /> Следить за тикетом можно кнопкой «Следить» в его карточке; автор и исполнитель следят автоматически. <Link href="/my">Мои задачи</Link></p>
  </div>;
}
