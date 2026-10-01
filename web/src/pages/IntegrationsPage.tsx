import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../api/client";
import type { ConfigField, Integration, IntegrationKind, SyncResult } from "../api/types";

function humanTime(iso?: string): string {
  if (!iso) return "—";
  const d = new Date(iso);
  const mins = Math.round((Date.now() - d.getTime()) / 60000);
  if (mins < 1) return "только что";
  if (mins < 60) return `${mins} мин назад`;
  if (mins < 24 * 60) return `${Math.round(mins / 60)} ч назад`;
  return d.toLocaleDateString("ru");
}

/** Renders one connector setting according to its declared type. */
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
        {field.help && <span style={{ color: "var(--text-faint)", fontSize: 11.5 }}>{field.help}</span>}
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

function AddForm({ kinds, onDone }: { kinds: IntegrationKind[]; onDone: () => void }) {
  const [kind, setKind] = useState(kinds[0]?.kind ?? "");
  const [name, setName] = useState("");
  const [interval, setInterval] = useState("15m");
  const [values, setValues] = useState<Record<string, string>>({});
  const qc = useQueryClient();

  const schema = kinds.find((k) => k.kind === kind);

  const create = useMutation({
    mutationFn: () => api.addIntegration({ kind, name: name || schema?.title || kind, interval, values }),
    onSuccess: () => {
      qc.invalidateQueries();
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
        marginTop: 12,
        background: "var(--bg-panel)",
      }}
    >
      <div className="section-head" style={{ marginTop: 0 }}>
        Новая интеграция
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
      <Field
        field={{
          key: "interval",
          label: "Как часто обновлять",
          type: "duration",
          required: false,
          placeholder: "15m",
        }}
        value={interval}
        onChange={setInterval}
      />

      {schema?.fields.map((f) => (
        <Field
          key={f.key}
          field={f}
          value={values[f.key] ?? ""}
          onChange={(v) => setValues((prev) => ({ ...prev, [f.key]: v }))}
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

function Row({ item }: { item: Integration }) {
  const qc = useQueryClient();
  const [result, setResult] = useState<SyncResult | null>(null);

  const sync = useMutation({
    mutationFn: () => api.syncIntegration(item.id),
    onSuccess: (r) => {
      setResult(r);
      qc.invalidateQueries();
    },
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

  return (
    <div style={{ padding: "10px 0", borderBottom: "1px solid var(--border)" }}>
      <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
        <input
          type="checkbox"
          checked={item.enabled}
          onChange={(e) => toggle.mutate(e.target.checked)}
          title={item.enabled ? "Выключить" : "Включить"}
        />
        <strong>{item.name}</strong>
        <span className="chip">{item.title}</span>
        <span className="chip">каждые {item.interval}</span>
        {item.running && <span className="chip">синхронизация…</span>}

        <div style={{ marginLeft: "auto", display: "flex", gap: 6 }}>
          <button className="btn" onClick={() => sync.mutate()} disabled={sync.isPending}>
            {sync.isPending ? "…" : "Обновить"}
          </button>
          <button
            className="btn"
            onClick={() => {
              if (confirm(`Удалить интеграцию «${item.name}»? Созданные заметки останутся.`)) {
                remove.mutate();
              }
            }}
          >
            Удалить
          </button>
        </div>
      </div>

      <div className="task-meta" style={{ marginTop: 4 }}>
        <span>последняя синхронизация: {humanTime(item.last_sync)}</span>
        <span>элементов: {item.last_items}</span>
        {Object.entries(item.values)
          .filter(([, v]) => v)
          .map(([k, v]) => (
            <span key={k} className="chip" title={`${k}: ${v}`}>
              {k}: {v.length > 42 ? v.slice(0, 42) + "…" : v}
            </span>
          ))}
      </div>

      {error && (
        <div className="error" style={{ marginTop: 6 }}>
          {error}
        </div>
      )}
      {result && !result.error && (
        <div className="task-meta" style={{ marginTop: 6, color: "var(--done)" }}>
          создано {result.created} · обновлено {result.updated} · событий {result.events} · без
          изменений {result.skipped}
        </div>
      )}
    </div>
  );
}

export default function IntegrationsPage() {
  const [adding, setAdding] = useState(false);

  const { data, isLoading } = useQuery({ queryKey: ["integrations"], queryFn: api.integrations });
  const { data: kinds } = useQuery({
    queryKey: ["integration-kinds"],
    queryFn: api.integrationKinds,
  });

  return (
    <>
      <div className="page-head">
        <h1>Интеграции</h1>
        <span className="sub">календари и трекеры рядом с задачами</span>
        <div style={{ marginLeft: "auto" }}>
          <button className="btn primary" onClick={() => setAdding((v) => !v)}>
            + Подключить
          </button>
        </div>
      </div>

      <div style={{ color: "var(--text-dim)", fontSize: 12.5, marginBottom: 6 }}>
        Токены и пароли не хранятся в настройках: в полях-секретах указывается ссылка вида{" "}
        <code>bitwarden:имя-записи</code>, <code>keepass:база.kdbx#Запись</code>,{" "}
        <code>env:ПЕРЕМЕННАЯ</code> или <code>command:…</code>.
      </div>

      {adding && kinds && kinds.items.length > 0 && (
        <AddForm kinds={kinds.items} onDone={() => setAdding(false)} />
      )}

      {isLoading && <div className="spinner">Загрузка…</div>}
      {!isLoading && data?.items.length === 0 && !adding && (
        <div className="empty">Ничего не подключено</div>
      )}

      <div style={{ marginTop: 12 }}>
        {data?.items.map((item) => (
          <Row key={item.id} item={item} />
        ))}
      </div>
    </>
  );
}
