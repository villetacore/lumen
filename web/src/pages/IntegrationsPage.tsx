import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, Badge, Button, Dialog, EmptyState, Input, Select, Spinner, Switch } from "@villetacore/velessoft-ui";
import { api } from "../api/client";
import type { Integration, IntegrationKind, SyncResult } from "../api/types";
import ConfigFields, { SECRET_HELP } from "../components/ConfigFields";
import Icon from "../components/Icon";
import { errorText } from "../lib/errors";

function humanTime(iso?: string): string {
  if (!iso) return "ещё не было";
  const mins = Math.round((Date.now() - new Date(iso).getTime()) / 60000);
  if (mins < 1) return "только что";
  if (mins < 60) return `${mins} мин назад`;
  if (mins < 24 * 60) return `${Math.round(mins / 60)} ч назад`;
  return new Date(iso).toLocaleDateString("ru");
}

/** The server sends Go durations: "15m0s", "1h0m0s". */
function humanInterval(d: string): string {
  const [, h, m] = d.match(/^(?:(\d+)h)?(?:(\d+)m)?/) ?? [];
  const parts = [Number(h) ? `${Number(h)} ч` : "", Number(m) ? `${Number(m)} мин` : ""].filter(Boolean);
  return parts.join(" ") || d;
}

const INTERVALS = [{ value: "5m", label: "Каждые 5 минут" }, { value: "15m", label: "Каждые 15 минут" }, { value: "1h", label: "Каждый час" }, { value: "6h", label: "Каждые 6 часов" }];

function AddDialog({ kinds, onClose }: { kinds: IntegrationKind[]; onClose: () => void }) {
  const [kind, setKind] = useState(kinds.find((k) => k.kind === "gitlab")?.kind ?? kinds[0]?.kind ?? "");
  const [name, setName] = useState("");
  const [interval, setInterval] = useState("15m");
  const [values, setValues] = useState<Record<string, string>>({});
  const qc = useQueryClient();
  const schema = kinds.find((k) => k.kind === kind);
  const create = useMutation({
    mutationFn: () => api.addIntegration({ kind, name: name || schema?.title || kind, interval, values }),
    onSuccess: (added) => {
      qc.invalidateQueries();
      onClose();
      // The first result, or the first error, should show right away. A failure
      // lands in the row's last_error, so there is nothing to catch here.
      api.syncIntegration(added.id).catch(() => {}).finally(() => qc.invalidateQueries());
    },
  });
  const ready = !!schema && schema.fields.every((f) => !f.required || values[f.key]?.trim());

  return <Dialog open onClose={() => !create.isPending && onClose()} title="Подключить сервис" footer={<>
    <Button onClick={onClose} disabled={create.isPending}>Отмена</Button>
    <Button type="submit" form="new-integration" variant="primary" loading={create.isPending} disabled={!ready}>Подключить</Button>
  </>}>
    <form id="new-integration" className="issue-form" onSubmit={(e) => { e.preventDefault(); if (ready) create.mutate(); }}>
      <Select label="Сервис" value={kind} onChange={(e) => { setKind(e.target.value); setValues({}); }}>
        {kinds.map((k) => <option key={k.kind} value={k.kind}>{k.title}</option>)}
      </Select>
      <Input label="Название" value={name} onChange={(e) => setName(e.target.value)} placeholder={schema?.title} />
      {schema && <ConfigFields fields={schema.fields} values={values} onChange={setValues} />}
      <Select label="Как часто обновлять" value={interval} onChange={(e) => setInterval(e.target.value)}>
        {INTERVALS.map((i) => <option key={i.value} value={i.value}>{i.label}</option>)}
      </Select>
      <p className="hint">{SECRET_HELP}</p>
      {create.isError && <Alert tone="error" title="Не подключено">{errorText(create.error)}</Alert>}
    </form>
  </Dialog>;
}

function Row({ item, secretKeys }: { item: Integration; secretKeys: Set<string> }) {
  const qc = useQueryClient();
  const [result, setResult] = useState<SyncResult | null>(null);
  const sync = useMutation({
    mutationFn: () => api.syncIntegration(item.id),
    onSuccess: (r) => { setResult(r); qc.invalidateQueries(); },
  });
  const toggle = useMutation({
    mutationFn: (enabled: boolean) => api.patchIntegration(item.id, { enabled }),
    onSuccess: () => qc.invalidateQueries(),
  });
  const remove = useMutation({
    mutationFn: () => api.deleteIntegration(item.id),
    onSuccess: () => qc.invalidateQueries(),
  });
  const error = result?.error ?? item.last_error;
  // Flags and secret references say nothing to someone scanning the list.
  const facts = Object.entries(item.values).filter(([k, v]) => v && v !== "true" && v !== "false" && !secretKeys.has(k));

  return <article className="card service-row">
    <div className="service-head">
      <strong>{item.name}</strong>
      <Badge tone="neutral">{item.title}</Badge>
      {item.running && <Badge tone="info">обновляется</Badge>}
      <div className="service-actions">
        <Switch label="Включено" checked={item.enabled} onChange={(e) => toggle.mutate(e.target.checked)} />
        <Button size="sm" onClick={() => sync.mutate()} loading={sync.isPending}>Обновить</Button>
        <Button size="sm" onClick={() => { if (confirm(`Отключить «${item.name}»? Созданные заметки останутся.`)) remove.mutate(); }}>Отключить</Button>
      </div>
    </div>
    <p className="service-meta">Обновлено: {humanTime(item.last_sync)} · элементов: {item.last_items} · каждые {humanInterval(item.interval)}</p>
    {!!facts.length && <p className="service-facts">{facts.map(([k, v]) => <span key={k} title={k}>{v}</span>)}</p>}
    {error && <Alert tone="error" title="Ошибка обновления">{error}</Alert>}
    {result && !result.error && <p className="service-meta">Создано {result.created} · обновлено {result.updated} · событий {result.events} · без изменений {result.skipped}</p>}
  </article>;
}

export default function IntegrationsPage() {
  const [adding, setAdding] = useState(false);
  const qc = useQueryClient();
  const { data, isLoading, error } = useQuery({ queryKey: ["integrations"], queryFn: api.integrations });
  const { data: kinds } = useQuery({ queryKey: ["integration-kinds"], queryFn: api.integrationKinds });
  const reindex = useMutation({ mutationFn: api.reindex, onSuccess: () => qc.invalidateQueries() });
  const secretKeys = new Set(kinds?.items.flatMap((k) => k.fields.filter((f) => f.type === "secret").map((f) => f.key)));

  return <div className="page">
    <div className="page-head">
      <div>
        <span className="kicker">Настройка</span>
        <h1>Интеграции</h1>
        <span className="sub">GitLab, календари и трекеры. Lumen только читает их: в сервисах ничего не меняется.</span>
      </div>
      <div className="head-actions">
        <Button onClick={() => reindex.mutate()} loading={reindex.isPending} title="Заново прочитать файлы vault с диска">Перечитать файлы</Button>
        <Button variant="primary" disabled={!kinds} onClick={() => setAdding(true)}><Icon name="plus" size={16} />Подключить</Button>
      </div>
    </div>
    {isLoading && <Spinner label="Загрузка" />}
    {error && <Alert tone="error" title="Не загружено">{errorText(error)}</Alert>}
    {reindex.isError && <Alert tone="error" title="Файлы не перечитаны">{errorText(reindex.error)}</Alert>}
    {data?.items.length === 0 && <EmptyState title="Ничего не подключено" description="Подключите GitLab или календарь: задачи и события появятся рядом с вашими."
      actions={<Button variant="primary" disabled={!kinds} onClick={() => setAdding(true)}>Подключить</Button>} />}
    <div className="service-list">{data?.items.map((item) => <Row key={item.id} item={item} secretKeys={secretKeys} />)}</div>
    {adding && kinds && <AddDialog kinds={kinds.items} onClose={() => setAdding(false)} />}
  </div>;
}
