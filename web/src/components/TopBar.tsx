import Icon from "./Icon";
import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { IconButton, Input, Kbd } from "@villetacore/velessoft-ui";
import { api } from "../api/client";

export default function TopBar() {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [q, setQ] = useState("");
  const input = useRef<HTMLInputElement>(null);

  const { data: stats } = useQuery({ queryKey: ["stats"], queryFn: api.stats });
  const { data: me } = useQuery({ queryKey: ["me"], queryFn: api.me });

  const reindex = useMutation({
    mutationFn: api.reindex,
    onSuccess: () => qc.invalidateQueries(),
  });

  // Ctrl/Cmd+K focuses search, the way every other tool in this category works.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        input.current?.focus();
        input.current?.select();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  return (
    <header className="topbar">
      <form
        className="search hide-label"
        role="search"
        onSubmit={(e) => {
          e.preventDefault();
          if (q.trim()) navigate(`/search?q=${encodeURIComponent(q.trim())}`);
        }}
      >
        <Input
          ref={input}
          label="Поиск"
          type="search"
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="Проекты, задачи, документы"
          startAddon={<Icon name="search" size={16} />}
          endAddon={<Kbd>Ctrl K</Kbd>}
        />
      </form>

      <div className="topbar-right">
        {me?.mode === "oidc" &&
          (me.authenticated ? (
            <>
              <span title={me.roles?.join(", ")}>{me.name}</span>
              <a className="btn" href={me.logout_url}>Выйти</a>
            </>
          ) : (
            <a className="btn primary" href={`${me.login_url}?return_to=${encodeURIComponent(location.pathname)}`}>
              Войти
            </a>
          ))}
        {stats?.encrypted && (
          <span title="Данные зашифрованы"><Icon name="lock" size={16} /></span>
        )}
        {stats && <span>открыто: {stats.open_tasks}</span>}
        <IconButton label="Обновить данные" variant="ghost" onClick={() => reindex.mutate()} disabled={reindex.isPending}>
          <Icon name="refresh" size={16} className={reindex.isPending ? "spinning" : ""} />
        </IconButton>
      </div>
    </header>
  );
}
