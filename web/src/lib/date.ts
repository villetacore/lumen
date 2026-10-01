const MONTHS = [
  "января", "февраля", "марта", "апреля", "мая", "июня",
  "июля", "августа", "сентября", "октября", "ноября", "декабря",
];
const WEEKDAYS = ["воскресенье", "понедельник", "вторник", "среда", "четверг", "пятница", "суббота"];
export const DOW_SHORT = ["пн", "вт", "ср", "чт", "пт", "сб", "вс"];

/** iso returns a YYYY-MM-DD key in local time. */
export function iso(d: Date): string {
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`;
}

export function today(): Date {
  const n = new Date();
  return new Date(n.getFullYear(), n.getMonth(), n.getDate());
}

export function parseDate(s: string): Date {
  return new Date(s.length > 10 ? s : s + "T00:00:00");
}

export function addDays(d: Date, n: number): Date {
  const c = new Date(d);
  c.setDate(c.getDate() + n);
  return c;
}

/** startOfWeek returns the Monday of the given date's week. */
export function startOfWeek(d: Date): Date {
  return addDays(d, -((d.getDay() + 6) % 7));
}

export function longDate(d: Date): string {
  return `${WEEKDAYS[d.getDay()]}, ${d.getDate()} ${MONTHS[d.getMonth()]}`;
}

export function monthTitle(d: Date): string {
  const nominative = [
    "январь", "февраль", "март", "апрель", "май", "июнь",
    "июль", "август", "сентябрь", "октябрь", "ноябрь", "декабрь",
  ];
  return `${nominative[d.getMonth()]} ${d.getFullYear()}`;
}

/** relativeDue renders a due date the way a task list should read. */
export function relativeDue(value: string): string {
  const d = parseDate(value);
  const t = today();
  const days = Math.round((new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime() - t.getTime()) / 86_400_000);
  const time = value.length > 10 ? " " + value.slice(11, 16) : "";
  if (days === 0) return "сегодня" + time;
  if (days === 1) return "завтра" + time;
  if (days === -1) return "вчера" + time;
  if (days > 1 && days < 7) return DOW_SHORT[(d.getDay() + 6) % 7] + time;
  return `${d.getDate()} ${MONTHS[d.getMonth()].slice(0, 3)}${time}`;
}

export function isOverdue(value: string): boolean {
  return parseDate(value) < today();
}

export function isToday(value: string): boolean {
  return iso(parseDate(value)) === iso(today());
}
