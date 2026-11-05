"use client";

import { Link } from "../lib/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, Badge, Button, EmptyState, Spinner } from "@villetacore/velessoft-ui";
import { api } from "../api/client";
import type { Proposal, ProposalState } from "../api/types";
import { errorText } from "../lib/errors";
import { issueUrl } from "../lib/tracker";

const KIND: Record<Proposal["kind"], string> = {
  create_issue: "Новый тикет",
  update_issue: "Изменение полей",
  change_status: "Смена статуса",
  add_comment: "Комментарий",
};

const STATE: Record<ProposalState, { label: string; tone: "neutral" | "info" | "success" | "warning" | "error" }> = {
  pending: { label: "ждёт решения", tone: "info" },
  applying: { label: "применяется", tone: "info" },
  applied: { label: "применено", tone: "success" },
  rejected: { label: "отклонено", tone: "neutral" },
  expired: { label: "истекло", tone: "neutral" },
  stale: { label: "устарело", tone: "warning" },
  failed: { label: "ошибка", tone: "error" },
};

function at(s?: string) {
  return s ? new Date(s).toLocaleString("ru-RU", { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" }) : "";
}

/**
 * ApprovalsPage is where a person applies or rejects changes their AI client
 * proposed through MCP. Nothing proposed reaches the vault without this click.
 */
export default function ApprovalsPage() {
  const qc = useQueryClient();
  const { data: me } = useQuery({ queryKey: ["me"], queryFn: api.me });
  const { data, error, isLoading } = useQuery({ queryKey: ["proposals"], queryFn: () => api.proposals() });
  const done = () => qc.invalidateQueries();
  const approve = useMutation({ mutationFn: api.approveProposal, onSettled: done });
  const reject = useMutation({ mutationFn: api.rejectProposal, onSettled: done });

  const items = data?.items ?? [];
  const pending = items.filter((p) => p.state === "pending");
  const decided = items.filter((p) => p.state !== "pending").slice(0, 30);
  const busy = approve.isPending || reject.isPending;

  const card = (p: Proposal) => <li key={p.id} className={`proposal is-${p.state}`}>
    <div className="proposal-head">
      <strong>{KIND[p.kind]}</strong>
      {p.target && <Link className="issue-key" href={p.kind === "create_issue" ? `/tracker/${encodeURIComponent(p.target)}` : issueUrl(p.target)}>{p.target}</Link>}
      <Badge tone={STATE[p.state].tone}>{STATE[p.state].label}</Badge>
      <span className="sub">{at(p.created_at)}{p.state === "pending" ? ` · до ${at(p.expires_at)}` : p.decided_at ? ` · решено ${at(p.decided_at)}` : ""}</span>
    </div>
    <pre className="proposal-preview">{p.preview}</pre>
    {p.error && <Alert tone={p.state === "stale" ? "warning" : "error"} title={p.state === "stale" ? "Не применено: данные изменились" : "Не применено"}>{p.error}</Alert>}
    {p.state === "applied" && p.result && <p className="sub">Готово: <Link href={issueUrl(p.result)}>{p.result}</Link></p>}
    {p.state === "pending" && <div className="proposal-actions">
      <Button variant="primary" onClick={() => approve.mutate(p.id)} disabled={busy}>Подтвердить</Button>
      <Button onClick={() => reject.mutate(p.id)} disabled={busy}>Отклонить</Button>
    </div>}
  </li>;

  return <div className="page">
    <div className="page-head">
      <div><span className="kicker">AI-клиенты</span><h1>Подтверждения</h1>
        <span className="sub">Изменения, которые ваш AI-клиент предложил через MCP. Они попадают в Lumen только после вашего подтверждения — с вашими правами.</span></div>
    </div>
    {me && !me.username && me.mode !== "none" && <Alert tone="info" title="Нужен вход">Подтверждения адресуются вошедшему пользователю.</Alert>}
    {isLoading && <Spinner label="Загрузка" />}
    {[error, approve.error, reject.error].map((e, i) => e && <Alert key={i} tone="error" title="Не выполнено">{errorText(e)}</Alert>)}
    <section aria-labelledby="pending-title">
      <h2 id="pending-title" className="nav-title">Ждут решения · {pending.length}</h2>
      {pending.length ? <ul className="proposals">{pending.map(card)}</ul>
        : <EmptyState title="Нечего подтверждать" description="Когда AI-клиент предложит изменение через MCP-инструменты propose_*, оно появится здесь." actions={<Link href="/connect">Подключить AI-клиент</Link>} />}
    </section>
    {!!decided.length && <section aria-labelledby="decided-title">
      <h2 id="decided-title" className="nav-title">Недавние решения</h2>
      <ul className="proposals">{decided.map(card)}</ul>
    </section>}
  </div>;
}
