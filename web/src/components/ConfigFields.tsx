import { Input, Switch } from "@villetacore/velessoft-ui";
import type { ConfigField } from "../api/types";

const isSaved = (value?: string) => !!value?.startsWith("stored:");

/**
 * ConfigFields renders a connector's or a password store's settings from the
 * schema the server declares. A secret field takes the value itself and hides it;
 * the server encrypts it and keeps only a reference (ADR-0017). What is already
 * saved never comes back to the form: leaving the field empty keeps it.
 */
export default function ConfigFields({ fields, values, saved = {}, onChange }: {
  fields: ConfigField[];
  values: Record<string, string>;
  /** The settings being edited, when this is not a new connection. */
  saved?: Record<string, string>;
  onChange: (next: Record<string, string>) => void;
}) {
  const set = (key: string, value: string) => onChange({ ...values, [key]: value });
  const [plain, flags] = [fields.filter((f) => f.type !== "bool"), fields.filter((f) => f.type === "bool")];
  return <>
    {plain.map((f) => f.type === "secret"
      ? <Input key={f.key} label={f.label} required={f.required && !saved[f.key]} type="password" autoComplete="new-password" spellCheck={false}
          value={values[f.key] ?? ""} placeholder={isSaved(saved[f.key]) ? "Сохранён. Введите новый, чтобы заменить" : saved[f.key] || f.placeholder}
          hint={f.help} onChange={(e) => set(f.key, e.target.value)} />
      : <Input key={f.key} label={f.label} required={f.required} value={values[f.key] ?? saved[f.key] ?? ""} spellCheck={false}
          type={f.type === "url" ? "url" : "text"} placeholder={f.placeholder} hint={f.help} onChange={(e) => set(f.key, e.target.value)} />)}
    {flags.map((f) => <div key={f.key}>
      <Switch label={f.label} checked={(values[f.key] ?? saved[f.key]) === "true"} onChange={(e) => set(f.key, e.target.checked ? "true" : "false")} />
      {f.help && <p className="hint config-hint">{f.help}</p>}
    </div>)}
  </>;
}

/** requiredFilled tells whether a form may be sent; a saved secret counts. */
export function requiredFilled(fields: ConfigField[], values: Record<string, string>, saved: Record<string, string> = {}) {
  return fields.every((f) => !f.required || (values[f.key] ?? saved[f.key] ?? "").trim());
}
