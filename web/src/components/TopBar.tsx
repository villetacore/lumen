import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
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
      <div className="brand">
        <span className="dot" />
        Lumen
      </div>

      <form
        className="search-box"
        onSubmit={(e) => {
          e.preventDefault();
          if (q.trim()) navigate(`/search?q=${encodeURIComponent(q.trim())}`);
        }}
      >
        <span>⌕</span>
        <input
          ref={input}
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="Поиск по заметкам и задачам"
          aria-label="Поиск"
        />
        <kbd>Ctrl K</kbd>
      </form>

      <div className="topbar-right">
        {me?.mode === "oidc" &&
          (me.authenticated ? (
            <>
              <span title={me.roles?.join(", ")}>{me.name}</span>
              <a className="btn" href={me.logout_url}>
                Выйти
              </a>
            </>
          ) : (
            <a
              className="btn primary"
              href={`${me.login_url}?return_to=${encodeURIComponent(location.pathname)}`}
            >
              Войти
            </a>
          ))}
        {stats?.encrypted && (
          <span title="Vault зашифрован на диске">🔒</span>
        )}
        {stats && (
          <span title={stats.vault_path}>
            {stats.notes} заметок · {stats.open_tasks} открытых
          </span>
        )}
        <button
          className="btn"
          onClick={() => reindex.mutate()}
          disabled={reindex.isPending}
          title="Переиндексировать vault"
        >
          {reindex.isPending ? "…" : "⟳"}
        </button>
      </div>
    </header>
  );
}
