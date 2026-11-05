"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, Badge, Button, EmptyState, Spinner } from "@villetacore/velessoft-ui";
import { api } from "../api/client";
import type { McpConnectionInfo } from "../api/types";
import { errorText } from "../lib/errors";

function when(s?: string) {
  return s ? new Date(s).toLocaleString("ru-RU", { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" }) : "";
}

function Copyable({ label, value }: { label: string; value: string }) {
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 2000);
    } catch { /* The text is on the screen and can be selected by hand. */ }
  };
  return <div className="connect-copy">
    <code>{value}</code>
    <Button size="sm" onClick={copy} aria-label={`Скопировать: ${label}`}>{copied ? "Скопировано" : "Скопировать"}</Button>
  </div>;
}

function Steps({ info }: { info: McpConnectionInfo }) {
  const header = info.auth === "token" ? ' --header "Authorization: Bearer <токен>"' : "";
  const code = `claude mcp add --scope user --transport http lumen ${info.url}${header}`;
  const json = JSON.stringify({ mcpServers: { lumen: info.auth === "token"
    ? { url: info.url, headers: { Authorization: "Bearer <токен>" } }
    : { url: info.url } } }, null, 2);
  return <div className="connect-clients">
    {info.auth === "oauth" && <section aria-labelledby="client-claude">
      <h3 id="client-claude">Claude: веб и приложение</h3>
      <ol>
        <li>Откройте настройки Claude, раздел коннекторов, и добавьте свой коннектор.</li>
        <li>Вставьте адрес сервера.</li>
        <li>Войдите в Lumen и разрешите подключение.</li>
      </ol>
    </section>}
    <section aria-labelledby="client-code">
      <h3 id="client-code">Claude Code</h3>
      <Copyable label="команда для Claude Code" value={code} />
      {info.auth === "oauth" && <p className="sub">Затем в Claude Code выполните <code>/mcp</code> и выберите lumen: откроется браузер со входом и разрешением.</p>}
    </section>
    <section aria-labelledby="client-other">
      <h3 id="client-other">Cursor и другие клиенты</h3>
      <Copyable label="настройки MCP" value={json} />
    </section>
    {info.auth === "token" && <p className="sub">На этом сервере вход по статическому токену: его выдаёт администратор.</p>}
    {info.auth === "none" && <p className="sub">Локальный режим: вход не нужен.</p>}
  </div>;
}

/** ConnectPage shows how to connect an AI client and which ones are connected. */
export default function ConnectPage() {
  const qc = useQueryClient();
  const info = useQuery({ queryKey: ["mcp-connection"], queryFn: api.mcpConnection });
  const list = useQuery({ queryKey: ["mcp-connections"], queryFn: api.mcpConnections, enabled: info.data?.auth === "oauth" });
  const disconnect = useMutation({
    mutationFn: api.mcpDisconnect,
    onSettled: () => qc.invalidateQueries({ queryKey: ["mcp-connections"] }),
  });
  const items = list.data?.items ?? [];

  return <div className="page">
    <div className="page-head">
      <div><span className="kicker">Настройка</span><h1>AI-клиенты</h1>
        <span className="sub">Claude и другие клиенты MCP могут читать документацию и задачи, которые видны вам. Изменения они только предлагают: применяются те, что вы подтвердите.</span></div>
    </div>
    {info.isLoading && <Spinner label="Загрузка" />}
    {[info.error, list.error, disconnect.error].map((e, i) => e && <Alert key={i} tone="error" title="Не выполнено">{errorText(e)}</Alert>)}
    {info.data && <section aria-labelledby="connect-url">
      <h2 id="connect-url" className="nav-title">Адрес сервера</h2>
      <Copyable label="адрес сервера" value={info.data.url} />
      <Steps info={info.data} />
    </section>}
    {info.data?.auth === "oauth" && <section aria-labelledby="connected-title">
      <h2 id="connected-title" className="nav-title">Подключённые клиенты · {items.length}</h2>
      {list.isLoading ? <Spinner label="Загрузка" /> : items.length ? <ul className="connections">
        {items.map((c) => <li key={c.id} className="connection">
          <div>
            <strong>{c.client_name}</strong> <Badge tone="neutral">{c.redirect_host}</Badge>
            <div className="sub">Подключён {when(c.connected_at)}{c.last_used_at ? ` · использовался ${when(c.last_used_at)}` : ""} · действует до {when(c.expires_at)}</div>
          </div>
          <Button variant="danger" size="sm" disabled={disconnect.isPending} onClick={() => disconnect.mutate(c.id)} aria-label={`Отключить ${c.client_name}`}>Отключить</Button>
        </li>)}
      </ul> : <EmptyState title="Пока ничего не подключено" description="Подключённый клиент появится здесь. Его можно будет отключить в любой момент." />}
    </section>}
  </div>;
}
