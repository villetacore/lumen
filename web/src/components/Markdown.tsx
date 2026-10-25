import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import type { Task } from "../api/types";
import { isOverdue, isToday, relativeDue } from "../lib/date";

/**
 * Markdown renders a note body for reading. It builds React elements only —
 * never HTML strings — so whatever the file contains cannot inject markup or
 * script. Supported: headings, paragraphs, lists, task checkboxes, quotes,
 * fenced code, tables, rules and the Obsidian inline syntax ([[wikilinks]],
 * #tags, ==highlight==, [key:: value] fields, ^block-ids).
 */
export default function Markdown({
  body,
  tasks = [],
  onToggleTask,
}: {
  body: string;
  /** The note's parsed tasks; checkbox lines are matched to them by line number. */
  tasks?: Task[];
  onToggleTask?: (task: Task) => void;
}) {
  const lines = body.replace(/\r\n/g, "\n").split("\n");
  const taskAt = matchTasks(lines, tasks);
  return <div className="md">{renderBlocks(lines, taskAt, onToggleTask)}</div>;
}

const FENCE = /^\s*(```|~~~)/;
const HEADING = /^(#{1,6})\s+(.*?)\s*#*\s*$/;
const RULE = /^\s*([-*_])(\s*\1){2,}\s*$/;
const LIST = /^(\s*)([-*+]|\d+[.)])\s+(.*)$/;
const CHECKBOX = /^\[(.)\]\s+(.*)$/;
const TABLE_SEP = /^\s*\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)*\|?\s*$/;

/**
 * matchTasks maps body line indexes onto tasks. Task line numbers are counted
 * from the top of the file, the body starts after the frontmatter, so the
 * offset is found from the first checkbox and then checked line by line.
 */
function matchTasks(lines: string[], tasks: Task[]): Map<number, Task> {
  const out = new Map<number, Task>();
  if (tasks.length === 0) return out;
  const checkboxLines: number[] = [];
  let inFence = false;
  lines.forEach((l, i) => {
    if (FENCE.test(l)) inFence = !inFence;
    else if (!inFence && isCheckboxLine(l)) checkboxLines.push(i);
  });
  if (checkboxLines.length === 0) return out;

  const byLine = new Map(tasks.map((t) => [t.line, t]));
  const first = Math.min(...tasks.map((t) => t.line));
  const offset = first - checkboxLines[0];
  for (const i of checkboxLines) {
    const t = byLine.get(i + offset);
    // Guard against a stale offset: the task text must appear on the line.
    if (t && lines[i].includes(t.text.slice(0, 24))) out.set(i, t);
  }
  return out;
}

function isCheckboxLine(l: string): boolean {
  const m = LIST.exec(l);
  return !!m && CHECKBOX.test(m[3]);
}

function renderBlocks(
  lines: string[],
  taskAt: Map<number, Task>,
  onToggleTask?: (task: Task) => void,
): ReactNode[] {
  const out: ReactNode[] = [];
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];

    if (line.trim() === "") {
      i++;
      continue;
    }

    const fence = FENCE.exec(line);
    if (fence) {
      const code: string[] = [];
      i++;
      while (i < lines.length && !lines[i].trimStart().startsWith(fence[1])) code.push(lines[i++]);
      i++; // closing fence
      out.push(
        <pre key={out.length} className="md-code">
          <code>{code.join("\n")}</code>
        </pre>,
      );
      continue;
    }

    const h = HEADING.exec(line);
    if (h) {
      const Tag = `h${h[1].length}` as "h1";
      out.push(<Tag key={out.length}>{inline(h[2])}</Tag>);
      i++;
      continue;
    }

    if (RULE.test(line)) {
      out.push(<hr key={out.length} />);
      i++;
      continue;
    }

    if (line.trimStart().startsWith(">")) {
      const quote: string[] = [];
      while (i < lines.length && lines[i].trimStart().startsWith(">")) {
        quote.push(lines[i].trimStart().replace(/^>\s?/, ""));
        i++;
      }
      out.push(<blockquote key={out.length}>{renderBlocks(quote, new Map())}</blockquote>);
      continue;
    }

    if (line.trimStart().startsWith("|") && i + 1 < lines.length && TABLE_SEP.test(lines[i + 1])) {
      const head = cells(line);
      i += 2;
      const rows: string[][] = [];
      while (i < lines.length && lines[i].trimStart().startsWith("|")) rows.push(cells(lines[i++]));
      out.push(
        <div key={out.length} className="md-table">
          <table>
            <thead>
              <tr>{head.map((c, k) => <th key={k}>{inline(c)}</th>)}</tr>
            </thead>
            <tbody>
              {rows.map((r, j) => (
                <tr key={j}>{r.map((c, k) => <td key={k}>{inline(c)}</td>)}</tr>
              ))}
            </tbody>
          </table>
        </div>,
      );
      continue;
    }

    if (LIST.test(line)) {
      const items: ReactNode[] = [];
      while (i < lines.length && LIST.test(lines[i])) {
        items.push(listItem(lines[i], i, taskAt.get(i), onToggleTask));
        i++;
      }
      out.push(
        <ul key={out.length} className="md-list">
          {items}
        </ul>,
      );
      continue;
    }

    const para: string[] = [];
    while (
      i < lines.length &&
      lines[i].trim() !== "" &&
      !FENCE.test(lines[i]) &&
      !HEADING.test(lines[i]) &&
      !LIST.test(lines[i]) &&
      !lines[i].trimStart().startsWith(">")
    ) {
      para.push(lines[i++]);
    }
    out.push(
      <p key={out.length}>
        {para.map((p, k) => (
          <span key={k}>
            {k > 0 && <br />}
            {inline(p)}
          </span>
        ))}
      </p>,
    );
  }
  return out;
}

function cells(row: string): string[] {
  return row.trim().replace(/^\|/, "").replace(/\|$/, "").split("|").map((c) => c.trim());
}

function listItem(
  line: string,
  key: number,
  task: Task | undefined,
  onToggleTask?: (task: Task) => void,
): ReactNode {
  const m = LIST.exec(line)!;
  const depth = Math.floor(m[1].replace(/\t/g, "  ").length / 2);
  const ordered = /\d/.test(m[2]);
  const style = { marginLeft: depth * 20 };
  const box = CHECKBOX.exec(m[3]);

  if (!box) {
    return (
      <li key={key} style={style} className={ordered ? "ordered" : ""} data-marker={ordered ? m[2] : undefined}>
        {inline(m[3])}
      </li>
    );
  }

  const mark = box[1];
  const closed = mark === "x" || mark === "X" || mark === "-";
  const status = task?.status ?? (closed ? "done" : mark === "/" ? "doing" : "todo");
  const due = task ? task.due ?? task.scheduled : null;
  return (
    <li key={key} style={style} className={`md-task is-${status}`}>
      <button
        className={`checkbox ${status}`}
        disabled={!task || !onToggleTask}
        onClick={() => task && onToggleTask?.(task)}
        aria-label={closed ? "Вернуть в работу" : "Отметить выполненной"}
        title={task ? undefined : "Задача ещё не проиндексирована"}
      >
        {status === "done" ? "✓" : status === "doing" ? "/" : status === "cancelled" ? "×" : ""}
      </button>
      <span className="md-task-text">{inline(stripTaskMeta(box[2]))}</span>
      {task?.priority && <span className={`chip prio-${task.priority}`}>{task.priority}</span>}
      {due && status !== "done" && (
        <span className={`chip ${isOverdue(due) ? "due-overdue" : isToday(due) ? "due-today" : ""}`}>
          {relativeDue(due)}
        </span>
      )}
    </li>
  );
}

/** stripTaskMeta drops inline fields and the block id: they are shown as chips. */
function stripTaskMeta(s: string): string {
  return s
    .replace(/\s*\[[\w-]+::[^\]]*\]/g, "")
    .replace(/\s*\^[\w-]+\s*$/, "")
    .trim();
}

// One alternation for every inline construct; the first match wins.
const INLINE =
  /(`+)([^`]+?)\1|!?\[\[([^\]|#]*)(#[^\]|]*)?(?:\|([^\]]*))?\]\]|\[([^\]]+)\]\(([^)\s]+)\)|\*\*([^*]+)\*\*|__([^_]+)__|~~([^~]+)~~|==([^=]+)==|(?<![\w*])\*([^*\s][^*]*?)\*(?!\w)|(?<![\w_])_([^_\s][^_]*?)_(?!\w)|(^|\s)#([\p{L}\p{N}_/-]*\p{L}[\p{L}\p{N}_/-]*)|(https?:\/\/[^\s<>()]+[^\s<>().,;:!?"'])|\s*\[[\w-]+::[^\]]*\]|\s+\^[\w-]+$/gu;

function inline(text: string): ReactNode[] {
  const out: ReactNode[] = [];
  let last = 0;
  // matchAll works on a copy of the regex, so the recursive calls below
  // (bold inside a link, …) cannot disturb this loop's position.
  for (const m of text.matchAll(INLINE)) {
    if (m.index > last) out.push(text.slice(last, m.index));
    last = m.index + m[0].length;
    const k = out.length;
    if (m[2] !== undefined) {
      out.push(<code key={k}>{m[2]}</code>);
    } else if (m[3] !== undefined) {
      const target = m[3].trim();
      const label = m[5] || target + (m[4] && m[4] !== "#" ? ` › ${m[4].slice(1)}` : "") || m[4];
      out.push(
        <Link key={k} className="wikilink" to={target ? `/note/${notePath(target)}` : "#"}>
          {label}
        </Link>,
      );
    } else if (m[6] !== undefined) {
      out.push(link(k, m[6], m[7]));
    } else if (m[8] !== undefined || m[9] !== undefined) {
      out.push(<strong key={k}>{inline(m[8] ?? m[9])}</strong>);
    } else if (m[10] !== undefined) {
      out.push(<s key={k}>{inline(m[10])}</s>);
    } else if (m[11] !== undefined) {
      out.push(<mark key={k}>{inline(m[11])}</mark>);
    } else if (m[12] !== undefined || m[13] !== undefined) {
      out.push(<em key={k}>{inline(m[12] ?? m[13])}</em>);
    } else if (m[15] !== undefined) {
      out.push(m[14]);
      out.push(
        <Link key={k} className="md-tag" to={`/notes?tag=${encodeURIComponent(m[15])}`}>
          #{m[15]}
        </Link>,
      );
    } else if (m[16] !== undefined) {
      out.push(
        <a key={k} href={m[16]} target="_blank" rel="noreferrer noopener">
          {m[16]}
        </a>,
      );
    }
    // Inline fields and block ids render as nothing.
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}

/** link renders [text](url), allowing only safe schemes and vault-relative notes. */
function link(key: number, label: string, href: string): ReactNode {
  if (/^(https?:|mailto:)/i.test(href)) {
    return (
      <a key={key} href={href} target="_blank" rel="noreferrer noopener">
        {inline(label)}
      </a>
    );
  }
  if (!/^[a-z][a-z0-9+.-]*:/i.test(href) && !href.startsWith("//")) {
    const path = decodeURIComponentSafe(href.split("#")[0]);
    return (
      <Link key={key} className="wikilink" to={`/note/${notePath(path)}`}>
        {inline(label)}
      </Link>
    );
  }
  // javascript:, data: and anything else stays plain text.
  return <span key={key}>{label}</span>;
}

function notePath(target: string): string {
  return target.replace(/^\.?\//, "").split("/").map(encodeURIComponent).join("/");
}

function decodeURIComponentSafe(s: string): string {
  try {
    return decodeURIComponent(s);
  } catch {
    return s;
  }
}
