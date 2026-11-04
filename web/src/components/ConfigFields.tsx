import { Input, Switch } from "@villetacore/velessoft-ui";
import type { ConfigField } from "../api/types";

/**
 * ConfigFields renders a connector's or a password store's settings from the
 * schema the server declares. A "secret" field holds a reference, not a value.
 */
export default function ConfigFields({ fields, values, onChange }: {
  fields: ConfigField[];
  values: Record<string, string>;
  onChange: (next: Record<string, string>) => void;
}) {
  const set = (key: string, value: string) => onChange({ ...values, [key]: value });
  const [plain, flags] = [fields.filter((f) => f.type !== "bool"), fields.filter((f) => f.type === "bool")];
  return <>
    {plain.map((f) => <Input key={f.key} label={f.label} required={f.required} value={values[f.key] ?? ""} spellCheck={false}
      type={f.type === "url" ? "url" : "text"} placeholder={f.placeholder} hint={f.help} onChange={(e) => set(f.key, e.target.value)} />)}
    {flags.map((f) => <div key={f.key}>
      <Switch label={f.label} checked={values[f.key] === "true"} onChange={(e) => set(f.key, e.target.checked ? "true" : "false")} />
      {f.help && <p className="hint config-hint">{f.help}</p>}
    </div>)}
  </>;
}

/** The note every form with secret references needs. */
export const SECRET_HELP = "Токены и ключи здесь не хранятся: в их полях указывается ссылка на секрет, например env:ИМЯ_ПЕРЕМЕННОЙ или file:/run/secrets/имя.";
