import { useEffect, useRef, useState, type FormEvent } from "react";
import { Alert, Button, Input } from "@villetacore/velessoft-ui";
import { api, ApiError } from "../api/client";
import { errorText } from "../lib/errors";

/**
 * LoginForm signs in inside Lumen: the server checks the password with Keycloak.
 * The password lives only in this form and is cleared after every attempt.
 */
export default function LoginForm({ onSignedIn, ssoHref }: { onSignedIn: () => void | Promise<unknown>; ssoHref?: string }) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [otp, setOtp] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<{ text: string; useSso: boolean } | null>(null);
  const first = useRef<HTMLInputElement>(null);
  useEffect(() => {
    const frame = requestAnimationFrame(() => first.current?.focus());
    return () => cancelAnimationFrame(frame);
  }, []);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (busy || !username.trim() || !password) return;
    setBusy(true);
    setError(null);
    try {
      await api.passwordLogin({ username: username.trim(), password, otp: otp.trim() || undefined });
      setPassword("");
      setOtp("");
      await onSignedIn();
    } catch (err) {
      setPassword("");
      setOtp("");
      // The server's words are written for this form; the general text for a 401
      // is about an expired session.
      const known = err instanceof ApiError && err.code !== "unknown";
      setError({ text: known ? err.message : errorText(err), useSso: err instanceof ApiError && err.code === "password_login_disabled" });
    } finally {
      setBusy(false);
    }
  };

  return <form className="login-form" onSubmit={submit}>
    <Input ref={first} label="Логин" autoComplete="username" autoCapitalize="none" spellCheck={false} value={username} onChange={(e) => setUsername(e.target.value)} />
    <Input label="Пароль" type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} />
    <details className="login-otp">
      <summary>Есть код двухфакторной защиты</summary>
      <Input label="Код" inputMode="numeric" autoComplete="one-time-code" value={otp} onChange={(e) => setOtp(e.target.value)} />
    </details>
    {error && <Alert tone="error" title="Не удалось войти">{error.text}</Alert>}
    <Button type="submit" variant="primary" loading={busy} disabled={!username.trim() || !password}>Войти</Button>
    {ssoHref && <a className="login-sso" href={ssoHref}>{error?.useSso ? "Войти через страницу Keycloak" : "Войти через Keycloak"}</a>}
  </form>;
}
