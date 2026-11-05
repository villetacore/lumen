import type { AutomationAction, AutomationCondition, AutomationDefinition, AutomationRunStatus, TrackerProject } from "../api/types";
import { CATEGORY_LABEL, priorityOf, statusName, typeLabel } from "./tracker";

export const TRIGGERS = [
  { id: "issue_created", label: "Создан тикет" },
  { id: "status_changed", label: "Сменился статус" },
  { id: "field_changed", label: "Изменилось поле" },
  { id: "comment_added", label: "Добавлен комментарий" },
  { id: "schedule", label: "По расписанию" },
] as const;

export const WATCHED_FIELDS: Record<string, string> = {
  assignee: "Исполнитель", priority: "Приоритет", labels: "Метки", sprint: "Спринт",
  due: "Срок", issue_type: "Тип", epic: "Эпик", parent: "Родитель",
};

export const CONDITION_FIELDS: Record<string, string> = {
  status: "Статус", category: "Этап", priority: "Приоритет", assignee: "Исполнитель", issue_type: "Тип",
  label: "Метка", sprint: "Спринт", epic: "Эпик", parent: "Родитель", due: "Срок",
};

export const CONDITION_OPS: Record<string, string> = {
  is: "равно", is_not: "не равно", empty: "не задано", not_empty: "задано", overdue: "просрочен", within: "наступит в течение, дней",
};

export const opsFor = (field: string) => (field === "due" ? ["overdue", "within", "empty", "not_empty"] : ["is", "is_not", "empty", "not_empty"]);
export const opNeedsValue = (op: string) => op === "is" || op === "is_not" || op === "within";

export const ACTIONS: Record<string, { label: string; hint?: string }> = {
  set_status: { label: "Сменить статус" },
  set_priority: { label: "Задать приоритет" },
  set_assignee: { label: "Назначить исполнителя", hint: "Логин или @reporter — автор тикета. Пусто — снять" },
  add_label: { label: "Добавить метку" },
  remove_label: { label: "Убрать метку" },
  set_due: { label: "Задать срок", hint: "Через сколько дней от сегодня. Пусто — убрать срок" },
  add_comment: { label: "Написать комментарий", hint: "Подстановки: {{key}}, {{title}}, {{status}}, {{assignee}}, {{reporter}}" },
  notify: { label: "Сообщить" },
  create_subissue: { label: "Создать подзадачу", hint: "Название. Подстановки: {{key}}, {{title}}" },
};

export const NOTIFY_TARGETS: Record<string, string> = { assignee: "исполнителю", reporter: "автору", watchers: "всем наблюдателям" };

export const WEEKDAYS = ["Пн", "Вт", "Ср", "Чт", "Пт", "Сб", "Вс"];

export const RUN_STATUS: Record<AutomationRunStatus, { label: string; tone: "neutral" | "info" | "success" | "warning" | "error" }> = {
  running: { label: "Выполняется", tone: "info" },
  done: { label: "Выполнено", tone: "success" },
  failed: { label: "Ошибка", tone: "error" },
  blocked: { label: "Нет прав", tone: "error" },
  throttled: { label: "Остановлено", tone: "warning" },
  unknown: { label: "Прервано", tone: "warning" },
};

export const browserZone = () => {
  try { return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC"; } catch { return "UTC"; }
};

/** Ready-made rules: they fill the form, and nothing is saved until the person does it. */
export function presets(p: TrackerProject): { title: string; definition: AutomationDefinition }[] {
  const doneStatus = p.workflow.find((s) => s.category === "done")?.id ?? p.workflow[p.workflow.length - 1].id;
  return [
    { title: "Напоминать о просроченных", definition: {
      name: "Напоминание о просроченных тикетах",
      trigger: { type: "schedule", schedule: { at: "09:00", weekdays: [1, 2, 3, 4, 5], tz: browserZone() } },
      conditions: [{ field: "due", op: "overdue" }],
      actions: [{ type: "notify", value: "assignee" }, { type: "add_label", value: "просрочен" }],
    } },
    { title: "Срочно разбирать новые ошибки", definition: {
      name: "Новые ошибки — в приоритет",
      trigger: { type: "issue_created" },
      conditions: [{ field: "issue_type", op: "is", value: "bug" }],
      actions: [{ type: "set_priority", value: "high" }, { type: "set_assignee", value: "@reporter" }],
    } },
    { title: "Сообщать автору о завершении", definition: {
      name: "Сообщить автору, что готово",
      trigger: { type: "status_changed", to: doneStatus },
      conditions: [],
      actions: [{ type: "notify", value: "reporter" }, { type: "add_comment", value: "Тикет {{key}} завершён." }],
    } },
  ];
}

function conditionText(c: AutomationCondition, p?: TrackerProject): string {
  const field = (CONDITION_FIELDS[c.field] ?? c.field).toLowerCase();
  if (c.field === "due") {
    if (c.op === "overdue") return "срок прошёл";
    if (c.op === "within") return `срок в ближайшие ${c.value} дн.`;
  }
  if (c.op === "empty") return `${field} не задан(а)`;
  if (c.op === "not_empty") return `${field} задан(а)`;
  let v = c.value ?? "";
  if (c.field === "status") v = statusName(p, v);
  if (c.field === "category") v = CATEGORY_LABEL[v as keyof typeof CATEGORY_LABEL] ?? v;
  if (c.field === "priority") v = priorityOf(v)?.label ?? v;
  if (c.field === "issue_type") v = typeLabel(v);
  return `${field} ${c.op === "is" ? "—" : "не"} ${v}`;
}

function actionText(a: AutomationAction, p?: TrackerProject): string {
  const v = a.value ?? "";
  switch (a.type) {
    case "set_status": return `статус → ${statusName(p, v)}`;
    case "set_priority": return `приоритет → ${priorityOf(v)?.label ?? (v || "не задан")}`;
    case "set_assignee": return v ? `исполнитель → ${v === "@reporter" ? "автор тикета" : v}` : "снять исполнителя";
    case "add_label": return `метка +${v}`;
    case "remove_label": return `метка −${v}`;
    case "set_due": return v ? `срок через ${v} дн.` : "убрать срок";
    case "add_comment": return "комментарий";
    case "notify": return `сообщить ${NOTIFY_TARGETS[v] ?? v}`;
    case "create_subissue": return `подзадача «${v}»`;
  }
  return a.type;
}

/** describeRule reads a rule back as one sentence, so a list of rules can be scanned. */
export function describeRule(d: AutomationDefinition, p?: TrackerProject): string {
  const t = d.trigger;
  let when = "";
  switch (t.type) {
    case "issue_created": when = "Когда создан тикет"; break;
    case "comment_added": when = "Когда добавлен комментарий"; break;
    case "status_changed":
      when = "Когда статус меняется" + (t.from ? ` с «${statusName(p, t.from)}»` : "") + (t.to ? ` на «${statusName(p, t.to)}»` : "");
      break;
    case "field_changed":
      when = `Когда меняется поле «${WATCHED_FIELDS[t.field ?? ""] ?? t.field}»` + (t.to ? ` на «${t.to}»` : "");
      break;
    case "schedule": {
      const days = t.schedule?.weekdays?.length ? [...t.schedule.weekdays].sort().map((w) => WEEKDAYS[w - 1]).join(", ") : "Каждый день";
      when = `${days} в ${t.schedule?.at} (${t.schedule?.tz})`;
      break;
    }
  }
  const cond = d.conditions.length ? `, если ${d.conditions.map((c) => conditionText(c, p)).join(" и ")}` : "";
  return `${when}${cond}: ${d.actions.map((a) => actionText(a, p)).join(", ")}.`;
}

/** problems lists what the form still needs; the server checks the rest. */
export function problems(d: AutomationDefinition): string[] {
  const out: string[] = [];
  if (!d.name.trim()) out.push("Назовите правило");
  if (!d.actions.length) out.push("Добавьте действие");
  for (const a of d.actions) {
    const emptyOk = a.type === "set_assignee" || a.type === "set_due" || a.type === "set_priority";
    if (!emptyOk && !(a.value ?? "").trim()) { out.push(`Заполните: ${ACTIONS[a.type]?.label ?? a.type}`); }
  }
  for (const c of d.conditions) {
    if (opNeedsValue(c.op) && !(c.value ?? "").trim()) out.push(`Заполните условие: ${CONDITION_FIELDS[c.field] ?? c.field}`);
  }
  return out;
}
