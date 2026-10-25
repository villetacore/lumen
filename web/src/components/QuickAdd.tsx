import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api/client";
import { errorText } from "../lib/errors";

/**
 * QuickAdd parses a few shorthands before sending the task:
 *   "текст !A"          → priority A
 *   "текст @завтра"     → due date (@пт, @+3d, @2026-11-01 …)
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

export default function QuickAdd({
  note,
  section,
  scheduled,
  placeholder,
}: {
  note?: string;
  section?: string;
  /** Scheduled date for tasks typed without a due date, so they stay in the current view. */
  scheduled?: string;
  placeholder?: string;
}) {
  const [value, setValue] = useState("");
  const qc = useQueryClient();

  const create = useMutation({
    mutationFn: (input: string) => {
      const { text, due, priority } = parse(input);
      return api.createTask({
        note,
        section,
        text,
        due,
        priority,
        scheduled: due ? undefined : scheduled,
      });
    },
    onSuccess: () => qc.invalidateQueries(),
    // Give the text back unless the person already started the next task.
    onError: (_, input) => setValue((v) => v || input),
  });

  return (
    <form
      className="quick-add"
      onSubmit={(e) => {
        e.preventDefault();
        // The field clears at once so the next task can be typed while this one saves.
        if (parse(value).text) {
          create.mutate(value);
          setValue("");
        }
      }}
    >
      <span style={{ color: "var(--text-faint)" }}>＋</span>
      <input
        value={value}
        onChange={(e) => {
          setValue(e.target.value);
          if (create.isError) create.reset();
        }}
        placeholder={
          placeholder ??
          (note
            ? "Новая задача… (!A приоритет, @завтра срок)"
            : "Быстрое добавление в дневную заметку… (!A, @завтра)")
        }
        aria-label="Новая задача"
      />
      {create.isError && <span style={{ color: "var(--overdue)" }}>{errorText(create.error)}</span>}
    </form>
  );
}
