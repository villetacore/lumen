import Icon from "./Icon";
import { useEffect, useRef, useState } from "react";
import { Link, useNavigate } from "../lib/navigation";
import { useQuery } from "@tanstack/react-query";
import { Input, Kbd } from "@villetacore/velessoft-ui";
import { api } from "../api/client";

export default function TopBar() {
  const navigate = useNavigate();
  const [q, setQ] = useState("");
  const input = useRef<HTMLInputElement>(null);

  const { data: stats } = useQuery({ queryKey: ["stats"], queryFn: api.stats });
  const { data: me } = useQuery({ queryKey: ["me"], queryFn: api.me });
  const { data: inbox } = useQuery({ queryKey: ["notifications"], queryFn: () => api.notifications(), enabled: !!me?.username });

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
        {stats?.encrypted && (
          <span title="Данные зашифрованы"><Icon name="lock" size={16} /></span>
        )}
        {me?.username && <Link href="/inbox" className="bell" aria-label={`Уведомления${inbox?.unread ? `: ${inbox.unread} непрочитанных` : ""}`}>
          <Icon name="bell" size={18} />
          {!!inbox?.unread && <span className="bell-count">{inbox.unread > 99 ? "99+" : inbox.unread}</span>}
        </Link>}
      </div>
    </header>
  );
}
