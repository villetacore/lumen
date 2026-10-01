import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../api/client";
import type { ConfigField, PasswordItem, PasswordKind, PasswordStore } from "../api/types";

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
};

function Secret({
  storeId,
  item,
  field,
  onError,
}: {
  storeId: string;
  item: PasswordItem;
  field: string;
  onError: (m: string) => void;
}) {
  const [value, setValue] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  // Значение исчезает, как только пользователь ушёл с записи.
  useEffect(() => () => setValue(null), [item.id, storeId]);

  const reveal = useMutation({
    mutationFn: () => api.revealPassword(storeId, item.id, field),
    onSuccess: (r) => setValue(r.value),
    onError: (e) => onError(String((e as Error).message)),
  });

  const copy = async () => {
    const v = value ?? (await api.revealPassword(storeId, item.id, field)).value;
    await navigator.clipboard.writeText(v);
    setCopied(true);
    // Буфер обмена чистится сам: пароль не должен лежать там до конца дня.
    window.setTimeout(() => {
      navigator.clipboard.writeText("").catch(() => {});
      setCopied(false);
    }, 30_000);
  };

  return (
    <div style={{ display: "flex", alignItems: "center", gap: 8, padding: "3px 0" }}>
      <span style={{ minWidth: 74, color: "var(--text-faint)", fontSize: 11.5 }}>
        {FIELD_LABEL[field] ?? field}
      </span>
      <code
        style={{
          flex: 1,
          fontFamily: "var(--mono)",
          fontSize: 12.5,
          background: "var(--bg-sunken)",
          border: "1px solid var(--border)",
          borderRadius: 4,
          padding: "2px 8px",
          overflowWrap: "anywhere",
          minHeight: 22,
        }}
      >
        {value ?? "••••••••••••"}
      </code>
      {value === null ? (
        <button className="btn" onClick={() => reveal.mutate()} disabled={reveal.isPending}>
          Показать
        </button>
      ) : (
        <button className="btn" onClick={() => setValue(null)}>
          Скрыть
        </button>
      )}
      <button className="btn" onClick={copy} title="Скопировать; буфер очистится через 30 секунд">
        {copied ? "✓ 30с" : "Копировать"}
      </button>
    </div>
  );
}

function ItemRow({ storeId, item }: { storeId: string; item: PasswordItem }) {
  const [open, setOpen] = useState(false);
  const [error, setError] = useState("");

  return (
    <div style={{ borderBottom: "1px solid var(--border)", padding: "6px 0" }}>
      <div
        style={{ display: "flex", alignItems: "center", gap: 10, cursor: "pointer" }}
        onClick={() => setOpen((v) => !v)}
      >
        <span style={{ color: "var(--text-faint)" }}>{open ? "▾" : "▸"}</span>
        <strong>{item.name}</strong>
        {item.username && <span style={{ color: "var(--text-dim)" }}>{item.username}</span>}
        {item.folder && <span className="chip">{item.folder}</span>}
        {item.uri && (
          <a
            href={item.uri}
            target="_blank"
            rel="noreferrer"
            className="chip"
            onClick={(e) => e.stopPropagation()}
          >
            ↗
          </a>
        )}
      </div>

      {open && (
        <div style={{ padding: "4px 0 4px 22px" }}>
          {item.fields.map((f) => (
            <Secret key={f} storeId={storeId} item={item} field={f} onError={setError} />
          ))}
          {error && (
            <div className="error" style={{ marginTop: 6 }}>
              {error}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function Field({
  field,
  value,
  onChange,
}: {
  field: ConfigField;
  value: string;
  onChange: (v: string) => void;
}) {
  if (field.type === "bool") {
    return (
      <label style={{ display: "flex", gap: 8, alignItems: "center", padding: "4px 0" }}>
        <input
          type="checkbox"
          checked={value === "true"}
          onChange={(e) => onChange(e.target.checked ? "true" : "false")}
        />
        <span>{field.label}</span>
      </label>
    );
  }
  return (
    <label style={{ display: "block", padding: "4px 0" }}>
      <div style={{ fontSize: 11.5, color: "var(--text-dim)" }}>
        {field.label}
        {field.required && <span style={{ color: "var(--overdue)" }}> *</span>}
      </div>
      <input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={field.placeholder}
        spellCheck={false}
        style={{
          width: "100%",
          padding: "5px 8px",
          background: "var(--bg-sunken)",
          border: "1px solid var(--border)",
          borderRadius: "var(--radius)",
          outline: "none",
        }}
      />
      {field.help && (
        <div style={{ fontSize: 11.5, color: "var(--text-faint)", marginTop: 2 }}>{field.help}</div>
      )}
    </label>
  );
}

function AddStore({ kinds, onDone }: { kinds: PasswordKind[]; onDone: () => void }) {
  const [kind, setKind] = useState(kinds[0]?.kind ?? "");
  const [name, setName] = useState("");
  const [values, setValues] = useState<Record<string, string>>({});
  const qc = useQueryClient();
  const schema = kinds.find((k) => k.kind === kind);

  const create = useMutation({
    mutationFn: () => api.addPasswordStore({ kind, name: name || schema?.title || kind, values }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["password-stores"] });
      onDone();
    },
  });

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        create.mutate();
      }}
      style={{
        border: "1px solid var(--border)",
        borderRadius: "var(--radius)",
        padding: "12px 14px",
        margin: "12px 0",
        background: "var(--bg-panel)",
      }}
    >
      <div className="section-head" style={{ marginTop: 0 }}>
        Подключить хранилище
      </div>

      <label style={{ display: "block", padding: "4px 0" }}>
        <div style={{ fontSize: 11.5, color: "var(--text-dim)" }}>Тип</div>
        <select
          value={kind}
          onChange={(e) => {
            setKind(e.target.value);
            setValues({});
          }}
          style={{
            width: "100%",
            padding: "5px 8px",
            background: "var(--bg-sunken)",
            border: "1px solid var(--border)",
            borderRadius: "var(--radius)",
          }}
        >
          {kinds.map((k) => (
            <option key={k.kind} value={k.kind}>
              {k.title}
            </option>
          ))}
        </select>
      </label>

      <Field
        field={{ key: "name", label: "Название", type: "text", required: false }}
        value={name}
        onChange={setName}
      />
      {schema?.fields.map((f) => (
        <Field
          key={f.key}
          field={f}
          value={values[f.key] ?? ""}
          onChange={(v) => setValues((p) => ({ ...p, [f.key]: v }))}
        />
      ))}

      {create.isError && (
        <div className="error" style={{ marginTop: 8 }}>
          {String((create.error as Error).message)}
        </div>
      )}
      <div style={{ display: "flex", gap: 8, marginTop: 10 }}>
        <button className="btn primary" type="submit" disabled={create.isPending}>
          Подключить
        </button>
        <button className="btn" type="button" onClick={onDone}>
          Отмена
        </button>
      </div>
    </form>
  );
}

function StorePanel({ store }: { store: PasswordStore }) {
  const [passphrase, setPassphrase] = useState("");
  const [query, setQuery] = useState("");
  const qc = useQueryClient();

  const unlock = useMutation({
    mutationFn: () => api.unlockPasswordStore(store.id, passphrase),
    onSuccess: () => {
      setPassphrase(""); // мастер-пароль не задерживается в памяти вкладки
      qc.invalidateQueries({ queryKey: ["password-stores"] });
    },
  });
  const lock = useMutation({
    mutationFn: () => api.lockPasswordStore(store.id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["password-stores"] }),
  });
  const remove = useMutation({
    mutationFn: () => api.deletePasswordStore(store.id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["password-stores"] }),
  });

  const { data: items, isFetching } = useQuery({
    queryKey: ["password-items", store.id, query],
    queryFn: () => api.passwordItems(store.id, query),
    enabled: store.unlocked,
    // Список не кэшируем: закрыли хранилище — данных больше нет.
    gcTime: 0,
    staleTime: 0,
  });

  return (
    <section style={{ marginBottom: 22 }}>
      <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 6 }}>
        <span>{store.unlocked ? "🔓" : "🔒"}</span>
        <strong>{store.name}</strong>
        <span className="chip">{store.title}</span>
        {store.locks_at && (
          <span className="chip" title="Закроется само при простое">
            до {new Date(store.locks_at).toLocaleTimeString("ru", { hour: "2-digit", minute: "2-digit" })}
          </span>
        )}
        <div style={{ marginLeft: "auto", display: "flex", gap: 6 }}>
          {store.unlocked && (
            <button className="btn" onClick={() => lock.mutate()}>
              Закрыть
            </button>
          )}
          <button
            className="btn"
            onClick={() => {
              if (confirm(`Убрать хранилище «${store.name}» из Lumen? Сама база не изменится.`)) {
                remove.mutate();
              }
            }}
          >
            Удалить
          </button>
        </div>
      </div>

      {store.last_error && <div className="error">{store.last_error}</div>}

      {!store.unlocked ? (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            unlock.mutate();
          }}
          style={{ display: "flex", gap: 8, alignItems: "center" }}
        >
          {store.needs_passphrase && (
            <input
              type="password"
              value={passphrase}
              onChange={(e) => setPassphrase(e.target.value)}
              placeholder="Мастер-пароль"
              autoComplete="off"
              style={{
                flex: "0 1 280px",
                padding: "5px 8px",
                background: "var(--bg-sunken)",
                border: "1px solid var(--border)",
                borderRadius: "var(--radius)",
                outline: "none",
              }}
            />
          )}
          <button className="btn primary" type="submit" disabled={unlock.isPending}>
            {unlock.isPending ? "Открываю…" : "Открыть"}
          </button>
          {!store.needs_passphrase && (
            <span style={{ color: "var(--text-faint)", fontSize: 12 }}>
              пароль не нужен — используется сессия из настроек
            </span>
          )}
        </form>
      ) : (
        <>
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Поиск по записям…"
            style={{
              width: "100%",
              padding: "5px 8px",
              marginBottom: 6,
              background: "var(--bg-sunken)",
              border: "1px solid var(--border)",
              borderRadius: "var(--radius)",
              outline: "none",
            }}
          />
          {isFetching && <div className="spinner">Загрузка…</div>}
          {items?.items.length === 0 && !isFetching && <div className="empty">Ничего не найдено</div>}
          {items?.items.map((it) => (
            <ItemRow key={it.id} storeId={store.id} item={it} />
          ))}
        </>
      )}
    </section>
  );
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

  return (
    <>
      <div className="page-head">
        <h1>Пароли</h1>
        <span className="sub">чтение из вашего менеджера паролей</span>
        <div style={{ marginLeft: "auto", display: "flex", gap: 8 }}>
          <button className="btn" onClick={() => lockAll.mutate()}>
            Закрыть все
          </button>
          <button className="btn primary" onClick={() => setAdding((v) => !v)}>
            + Подключить
          </button>
        </div>
      </div>

      <div style={{ color: "var(--text-dim)", fontSize: 12.5, marginBottom: 8 }}>
        Lumen ничего не записывает в вашу базу и не сохраняет мастер-пароль: он нужен один раз,
        чтобы получить сессию, которая живёт только в памяти сервера и закрывается сама при простое.
        Пароли не попадают ни в заметки, ни в поиск, ни в резервные копии.
      </div>

      {error && <div className="error">{String((error as Error).message)}</div>}
      {adding && kinds && kinds.items.length > 0 && (
        <AddStore kinds={kinds.items} onDone={() => setAdding(false)} />
      )}

      {isLoading && <div className="spinner">Загрузка…</div>}
      {!isLoading && data?.items.length === 0 && !adding && (
        <div className="empty">Хранилища не подключены</div>
      )}

      {data?.items.map((s) => (
        <StorePanel key={s.id} store={s} />
      ))}
    </>
  );
}
