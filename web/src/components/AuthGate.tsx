import { useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, Button, Input, Spinner } from "@villetacore/velessoft-ui";
import { api, UNAUTHORIZED_EVENT } from "../api/client";
import Brand from "./Brand";
import { errorText } from "../lib/errors";

const here = () => window.location.pathname + window.location.search;
const loginHref = (url: string, returnTo: string) => `${url}?return_to=${encodeURIComponent(returnTo)}`;

/**
 * AuthGate is the entrance. With Keycloak it goes straight to the sign-in page,
 * so nobody meets an empty workspace; a live Keycloak session makes that
 * invisible. A session that ends while the app is open must not take unsaved
 * text with it, so then it only offers to sign in again in another tab.
 */
export default function AuthGate({ children, go = (url) => window.location.assign(url) }: { children: ReactNode; go?: (url: string) => void }) {
  const qc = useQueryClient();
  const { data: me, error, isLoading, refetch } = useQuery({ queryKey: ["me"], queryFn: api.me });
  const entered = useRef(false);
  if (me?.authenticated) entered.current = true;

  const authError = new URLSearchParams(window.location.search).get("auth_error");
  const redirect = me?.mode === "oidc" && !me.authenticated && !entered.current && !authError && !!me.login_url;
  useEffect(() => {
    if (redirect) go(loginHref(me!.login_url!, here()));
  }, [redirect]);

  useEffect(() => {
    const onUnauthorized = () => qc.invalidateQueries({ queryKey: ["me"] });
    window.addEventListener(UNAUTHORIZED_EVENT, onUnauthorized);
    return () => window.removeEventListener(UNAUTHORIZED_EVENT, onUnauthorized);
  }, [qc]);

  if (entered.current) {
    return <>
      {me && !me.authenticated && <div className="session-banner">
        <Alert tone="warning" title="Сессия закончилась">
          Несохранённый текст на этой странице цел.{" "}
          {me.login_url
            ? <a href={loginHref(me.login_url, "/")} target="_blank" rel="noreferrer">Войдите снова в новой вкладке</a>
            : "Обновите токен доступа."}
          {me.login_url && " и вернитесь сюда."}
        </Alert>
      </div>}
      {children}
    </>;
  }

  if (error) {
    return <Entrance title="Сервер не отвечает">
      <p>{errorText(error)}</p>
      <Button onClick={() => refetch()}>Повторить</Button>
    </Entrance>;
  }
  if (isLoading || redirect || !me) return <Entrance><Spinner label={redirect ? "Переходим ко входу" : "Загрузка"} /></Entrance>;
  if (authError && me.mode === "oidc") {
    return <Entrance title={authError === "forbidden" ? "Нет доступа к Lumen" : "Не удалось войти"}>
      <p>{authError === "forbidden"
        ? "Вход выполнен, но у этой учётной записи нет нужной группы в Keycloak. Попросите администратора добавить вас."
        : "Keycloak не подтвердил вход. Попробуйте ещё раз."}</p>
      {/* Logging out of Keycloak too, or the same account would come straight back. */}
      <a className="btn primary" href={authError === "forbidden" ? me.logout_url : loginHref(me.login_url!, "/")}>
        {authError === "forbidden" ? "Войти под другой учётной записью" : "Войти снова"}
      </a>
    </Entrance>;
  }
  if (me.mode === "token") return <TokenEntrance />;
  return <>{children}</>;
}

function Entrance({ title, children }: { title?: string; children: ReactNode }) {
  return <main className="entrance">
    <div className="entrance-card">
      <Brand />
      {title && <h1>{title}</h1>}
      {children}
    </div>
  </main>;
}

function TokenEntrance() {
  const [token, setToken] = useState("");
  const submit = (e: FormEvent) => {
    e.preventDefault();
    try { localStorage.setItem("lumen.token", token.trim()); } catch { /* The next load asks again. */ }
    window.location.reload();
  };
  return <Entrance title="Нужен токен доступа">
    <form className="issue-form" onSubmit={submit}>
      <Input label="Токен" type="password" autoComplete="off" value={token} onChange={(e) => setToken(e.target.value)} />
      <Button type="submit" variant="primary" disabled={!token.trim()}>Войти</Button>
    </form>
  </Entrance>;
}
