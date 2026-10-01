import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api/client";

/**
 * QuickAdd parses a few shorthands before sending the task:
 *   "текст !A"          → priority A
 *   "текст @завтра"     → due date
 * Everything else is left as-is; the server normalises dates before writing.
 */
function parse(input: string): { text: string; due?: string; priority?: string } {
  let text = input;
  let due: string | undefined;
  let priority: string | undefined;

  text = text.replace(/(^|\s)!([ABCD])(?=\s|$)/i, (_, sp: string, p: string) => {
    priority = p.toUpperCase();
    return sp;
  });
  text = text.replace(/(^|\s)@(\S+)/, (_, sp: string, d: string) => {
    due = d;
    return sp;
  });
  return { text: text.trim(), due, priority };
}

export default function QuickAdd({ note, section }: { note?: string; section?: string }) {
  const [value, setValue] = useState("");
  const qc = useQueryClient();

  const create = useMutation({
    mutationFn: (input: string) => {
      const { text, due } = parse(input);
      return api.createTask({ note, section, text, due });
    },
    onSuccess: () => {
      setValue("");
      qc.invalidateQueries();
    },
  });

  return (
    <form
      className="quick-add"
      onSubmit={(e) => {
        e.preventDefault();
        if (value.trim()) create.mutate(value);
      }}
    >
      <span style={{ color: "var(--text-faint)" }}>＋</span>
      <input
        value={value}
        onChange={(e) => setValue(e.target.value)}
        placeholder={
          note
            ? "Новая задача… (!A приоритет, @завтра срок)"
            : "Быстрое добавление в дневную заметку… (!A, @завтра)"
        }
        disabled={create.isPending}
      />
      {create.isError && <span style={{ color: "var(--overdue)" }}>ошибка</span>}
    </form>
  );
}
