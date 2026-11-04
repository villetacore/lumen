"use client";

import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, Badge, Button, Dialog, EmptyState, Input, Select, Spinner } from "@villetacore/velessoft-ui";
import { api } from "../api/client";
import type { PasswordItem, PasswordKind, PasswordStore } from "../api/types";
import ConfigFields, { requiredFilled } from "../components/ConfigFields";
import Icon from "../components/Icon";
import { errorText } from "../lib/errors";

/**
 * Раскрытые значения живут только в состоянии компонента и стираются при
 * закрытии хранилища, смене записи и уходе со страницы. Ни в кэш запросов,
 * ни в localStorage они не попадают.
 */
const FIELD_LABEL: Record<string, string> = {
  password: "Пароль",
  username: "Логин",
  totp: "Код 2FA",
  notes: "Заметка",
  uri: "Адрес",
  cardholder: "Владелец карты",
  number: "Номер карты",
  code: "Код CVV",
  exp_month: "Месяц",
  exp_year: "Год",
  public_key: "Открытый ключ",
  private_key: "Закрытый ключ",
};
const TYPE_LABEL: Record<string, string> = { note: "Заметка", card: "Карта", identity: "Личные данные", ssh: "SSH-ключ" };

/** A custom field arrives as "field:Имя", part of an identity as "identity:phone". */
const fieldLabel = (f: string) => FIELD_LABEL[f] ?? f.replace(/^(field|identity):/, "");

function Secret({ storeId, item, field, onError }: { storeId: string; item: PasswordItem; field: string; onError: (m: string) => void }) {
  const [value, setValue] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  // Значение исчезает, как только пользователь ушёл с записи.
  useEffect(() => () => setValue(null), [item.id, storeId]);

  const reveal = useMutation({
    mutationFn: () => api.revealPassword(storeId, item.id, field),
    onSuccess: (r) => setValue(r.value),
    onError: (e) => onError(errorText(e)),
  });

  const copy = async () => {
    try {
      const v = field === "totp" || value === null ? (await api.revealPassword(storeId, item.id, field)).value : value;
      await navigator.clipboard.writeText(v);
      setCopied(true);
      // Буфер обмена чистится сам: пароль не должен лежать там до конца дня.
      window.setTimeout(() => {
        navigator.clipboard.writeText("").catch(() => {});
        setCopied(false);
      }, 30_000);
    } catch (e) {
      onError(errorText(e));
    }
  };

  return <div className="secret-row">
    <span className="secret-label">{fieldLabel(field)}</span>
    <code className="secret-value">{value ?? "••••••••••••"}</code>
    {value === null
      ? <Button size="sm" onClick={() => reveal.mutate()} loading={reveal.isPending}>Показать</Button>
      : <Button size="sm" onClick={() => setValue(null)}>Скрыть</Button>}
    <Button size="sm" onClick={copy} title="Скопировать; буфер очистится через 30 секунд">{copied ? "Скопировано" : "Копировать"}</Button>
  </div>;
}

function ItemRow({ storeId, item }: { storeId: string; item: PasswordItem }) {
  const [open, setOpen] = useState(false);
  const [error, setError] = useState("");
  return <li className="secret-item">
    <button className="secret-item-head" aria-expanded={open} onClick={() => setOpen((v) => !v)} disabled={!item.fields.length}>
      <Icon name="chevron" size={14} className={open ? "is-open" : ""} />
      <strong>{item.name}</strong>
      {item.username && <span className="sub">{item.username}</span>}
      {item.type && TYPE_LABEL[item.type] && <Badge tone="info">{TYPE_LABEL[item.type]}</Badge>}
      {item.folder && <Badge tone="neutral">{item.folder}</Badge>}
    </button>
    {open && <div className="secret-fields">
      {item.uri && /^https?:\/\//i.test(item.uri) && <a href={item.uri} target="_blank" rel="noreferrer">Открыть сайт</a>}
      {item.fields.map((f) => <Secret key={f} storeId={storeId} item={item} field={f} onError={setError} />)}
      {error && <Alert tone="error" title="Значение не получено">{error}</Alert>}
    </div>}
  </li>;
}

function AddDialog({ kinds, onClose }: { kinds: PasswordKind[]; onClose: () => void }) {
  const [kind, setKind] = useState(kinds[0]?.kind ?? "");
  const [name, setName] = useState("");
  const [values, setValues] = useState<Record<string, string>>({});
  const qc = useQueryClient();
  const schema = kinds.find((k) => k.kind === kind);
  const create = useMutation({
    mutationFn: () => api.addPasswordStore({ kind, name: name || schema?.title || kind, values }),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["password-stores"] }); onClose(); },
  });
  const ready = !!schema && requiredFilled(schema.fields, values);

  return <Dialog open onClose={() => !create.isPending && onClose()} title="Подключить хранилище" footer={<>
    <Button onClick={onClose} disabled={create.isPending}>Отмена</Button>
    <Button type="submit" form="new-store" variant="primary" loading={create.isPending} disabled={!ready}>Подключить</Button>
  </>}>
    <form id="new-store" className="issue-form" onSubmit={(e) => { e.preventDefault(); if (ready) create.mutate(); }}>
      <Select label="Тип" value={kind} onChange={(e) => { setKind(e.target.value); setValues({}); }}>
        {kinds.map((k) => <option key={k.kind} value={k.kind}>{k.title}</option>)}
      </Select>
      <Input label="Название" value={name} onChange={(e) => setName(e.target.value)} placeholder={schema?.title} />
      {schema && <ConfigFields fields={schema.fields} values={values} onChange={setValues} />}
      {create.isError && <Alert tone="error" title="Не подключено">{errorText(create.error)}</Alert>}
    </form>
  </Dialog>;
}

function EditDialog({ store, kind, onClose }: { store: PasswordStore; kind?: PasswordKind; onClose: () => void }) {
  const [name, setName] = useState(store.name);
  const [values, setValues] = useState<Record<string, string>>({});
  const qc = useQueryClient();
  const fields = kind?.fields ?? [];
  const save = useMutation({
    // An empty secret field means "keep it"; the rest is sent as typed.
    mutationFn: () => api.patchPasswordStore(store.id, { name, values: Object.fromEntries(Object.entries(values).filter(([k, v]) => v || fields.find((f) => f.key === k)?.type !== "secret")) }),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["password-stores"] }); onClose(); },
  });
  const ready = requiredFilled(fields, values, store.values);
  return <Dialog open onClose={() => !save.isPending && onClose()} title={`Настройки: ${store.name}`} footer={<>
    <Button onClick={onClose} disabled={save.isPending}>Отмена</Button>
    <Button type="submit" form="edit-store" variant="primary" loading={save.isPending} disabled={!ready}>Сохранить</Button>
  </>}>
    <form id="edit-store" className="issue-form" onSubmit={(e) => { e.preventDefault(); if (ready) save.mutate(); }}>
      <Input label="Название" value={name} onChange={(e) => setName(e.target.value)} />
      <ConfigFields fields={fields} values={values} saved={store.values} onChange={setValues} />
      <p className="hint">После сохранения хранилище нужно открыть заново.</p>
      {save.isError && <Alert tone="error" title="Не сохранено">{errorText(save.error)}</Alert>}
    </form>
  </Dialog>;
}

function StorePanel({ store, kind }: { store: PasswordStore; kind?: PasswordKind }) {
  const [editing, setEditing] = useState(false);
  const [passphrase, setPassphrase] = useState("");
  const [query, setQuery] = useState("");
  const qc = useQueryClient();
  const refresh = () => qc.invalidateQueries({ queryKey: ["password-stores"] });

  const unlock = useMutation({
    mutationFn: () => api.unlockPasswordStore(store.id, passphrase),
    onSuccess: () => { setPassphrase(""); refresh(); }, // мастер-пароль не задерживается в памяти вкладки
    onError: () => { setPassphrase(""); refresh(); },
  });
  const lock = useMutation({ mutationFn: () => api.lockPasswordStore(store.id), onSuccess: refresh });
  const remove = useMutation({ mutationFn: () => api.deletePasswordStore(store.id), onSuccess: refresh });

  const { data: items, isFetching, error } = useQuery({
    queryKey: ["password-items", store.id, query],
    queryFn: () => api.passwordItems(store.id, query),
    enabled: store.unlocked,
    // Список не кэшируем: закрыли хранилище — данных больше нет.
    gcTime: 0,
    staleTime: 0,
  });

  return <section className="card service-row" aria-label={store.name}>
    <div className="service-head">
      <Icon name={store.unlocked ? "unlock" : "lock"} size={16} />
      <strong>{store.name}</strong>
      <Badge tone="neutral">{store.title}</Badge>
      {store.locks_at && <span className="sub" title="Закроется само при простое">
        до {new Date(store.locks_at).toLocaleTimeString("ru", { hour: "2-digit", minute: "2-digit" })}
      </span>}
      <div className="service-actions">
        {store.unlocked && <Button size="sm" onClick={() => lock.mutate()}>Закрыть</Button>}
        <Button size="sm" onClick={() => setEditing(true)}>Настроить</Button>
        <Button size="sm" onClick={() => { if (confirm(`Убрать хранилище «${store.name}» из Lumen? Сама база не изменится.`)) remove.mutate(); }}>Отключить</Button>
      </div>
    </div>

    {store.last_error && <Alert tone="error" title="Ошибка хранилища">{store.last_error}</Alert>}

    {!store.unlocked
      ? <form className="unlock-form" onSubmit={(e) => { e.preventDefault(); unlock.mutate(); }}>
          {store.needs_passphrase && <Input label="Мастер-пароль" type="password" autoComplete="off" value={passphrase} onChange={(e) => setPassphrase(e.target.value)} />}
          <Button type="submit" variant="primary" loading={unlock.isPending} disabled={store.needs_passphrase && !passphrase}>Открыть</Button>
          {!store.needs_passphrase && <span className="hint">Пароль не нужен: используется сессия из настроек.</span>}
        </form>
      : <>
          <Input label="Поиск" type="search" value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Название, логин или адрес" />
          {isFetching && !items && <Spinner label="Загрузка записей" />}
          {error && <Alert tone="error" title="Записи не получены">{errorText(error)}</Alert>}
          {items?.items.length === 0 && !isFetching && <p className="hint">Ничего не найдено.</p>}
          {!!items?.items.length && <ul className="secret-list">{items.items.map((it) => <ItemRow key={it.id} storeId={store.id} item={it} />)}</ul>}
        </>}
    {editing && <EditDialog store={store} kind={kind} onClose={() => setEditing(false)} />}
  </section>;
}

export default function PasswordsPage() {
  const [adding, setAdding] = useState(false);
  const qc = useQueryClient();
  const { data, isLoading, error } = useQuery({
    queryKey: ["password-stores"],
    queryFn: api.passwordStores,
    refetchInterval: 30_000, // чтобы автозакрытие было видно
  });
  const { data: kinds } = useQuery({ queryKey: ["password-kinds"], queryFn: api.passwordKinds });
  const lockAll = useMutation({
    mutationFn: api.lockAllPasswordStores,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["password-stores"] }),
  });

  return <div className="page">
    <div className="page-head">
      <div>
        <span className="kicker">Настройка</span>
        <h1>Пароли</h1>
        <span className="sub">Lumen только читает ваш менеджер паролей. Мастер-пароль не сохраняется, хранилище закрывается само через 15 минут простоя.</span>
      </div>
      <div className="head-actions">
        {data?.items.some((s) => s.unlocked) && <Button onClick={() => lockAll.mutate()}>Закрыть все</Button>}
        <Button variant="primary" disabled={!kinds} onClick={() => setAdding(true)}><Icon name="plus" size={16} />Подключить</Button>
      </div>
    </div>
    {error && <Alert tone="error" title="Не загружено">{errorText(error)}</Alert>}
    {isLoading && <Spinner label="Загрузка" />}
    {data?.items.length === 0 && <EmptyState title="Хранилища не подключены" description="Подключите Vaultwarden: понадобятся адрес сервера и почта."
      actions={<Button variant="primary" disabled={!kinds} onClick={() => setAdding(true)}>Подключить</Button>} />}
    <div className="service-list">{data?.items.map((s) => <StorePanel key={s.id} store={s} kind={kinds?.items.find((k) => k.kind === s.kind)} />)}</div>
    {adding && kinds && <AddDialog kinds={kinds.items} onClose={() => setAdding(false)} />}
  </div>;
}
