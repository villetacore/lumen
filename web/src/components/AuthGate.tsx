import { useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, Button, Dialog, Input, Spinner } from "@villetacore/velessoft-ui";
import { api, UNAUTHORIZED_EVENT } from "../api/client";
import type { Identity } from "../api/types";
import Brand from "./Brand";
import LoginForm from "./LoginForm";
import { errorText } from "../lib/errors";

const here = () => window.location.pathname + window.location.search;
const loginHref = (url: string, returnTo: string) => `${url}?return_to=${encodeURIComponent(returnTo)}`;

/**
 * AuthGate is the entrance: nobody meets the workspace without signing in. The
 * form is Lumen's own and Keycloak checks the password behind it. If the session
 * ends mid-work the same form opens over the page, so unsaved text stays where
 * it is. A server without the form sends people to Keycloak's page instead.
 */
export default function AuthGate({ children, go = (url) => window.location.assign(url) }: { children: ReactNode; go?: (url: string) => void }) {
  const qc = useQueryClient();
  const { data: me, error, isLoading, refetch } = useQuery({ queryKey: ["me"], queryFn: api.me });
  const entered = useRef(false);
  if (me?.authenticated) entered.current = true;

  const authError = new URLSearchParams(window.location.search).get("auth_error");
  const out = me?.mode === "oidc" && !me.authenticated;
  const redirect = out && !entered.current && !authError && !me.password_login && !!me.login_url;
  useEffect(() => {
    if (redirect) go(loginHref(me!.login_url!, here()));
  }, [redirect]);

  useEffect(() => {
    const onUnauthorized = () => qc.invalidateQueries({ queryKey: ["me"] });
    window.addEventListener(UNAUTHORIZED_EVENT, onUnauthorized);
    return () => window.removeEventListener(UNAUTHORIZED_EVENT, onUnauthorized);
  }, [qc]);

  // Everything fetched before the sign-in is either empty or a 401.
  const signedIn = async () => {
    if (authError) window.history.replaceState({}, "", window.location.pathname);
    await qc.invalidateQueries();
  };

  if (entered.current) {
    return <>
      {me && !me.authenticated && <SessionEnded me={me} onSignedIn={signedIn} />}
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
  if (out && me.password_login) {
    return <Entrance title="Вход в Lumen">
      {authError && <Alert tone="error" title={authError === "forbidden" ? "Нет доступа к Lumen" : "Не удалось войти"}>
        {authError === "forbidden" ? "У этой учётной записи нет нужной группы. Попросите администратора добавить вас." : "Keycloak не подтвердил вход. Попробуйте ещё раз."}
      </Alert>}
      <LoginForm onSignedIn={signedIn} ssoHref={me.login_url && loginHref(me.login_url, here())} />
    </Entrance>;
  }
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

/** The page stays underneath: closing the form gives access to unsaved text. */
function SessionEnded({ me, onSignedIn }: { me: Identity; onSignedIn: () => Promise<unknown> }) {
  const [open, setOpen] = useState(true);
  const sso = me.login_url ? loginHref(me.login_url, "/") : undefined;
  return <>
    <div className="session-banner">
      <Alert tone="warning" title="Сессия закончилась">
        Несохранённый текст на этой странице цел.{" "}
        {me.password_login
          ? <button className="link-button" onClick={() => setOpen(true)}>Войти снова</button>
          : sso ? <a href={sso} target="_blank" rel="noreferrer">Войдите снова в новой вкладке</a> : "Обновите токен доступа."}
      </Alert>
    </div>
    {me.password_login && open && <Dialog open onClose={() => setOpen(false)} title="Войдите снова">
      <LoginForm onSignedIn={onSignedIn} />
    </Dialog>}
  </>;
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
