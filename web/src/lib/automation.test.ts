import { expect, test } from "vitest";
import type { TrackerProject } from "../api/types";
import { describeRule, presets, problems } from "./automation";

const project = {
  key: "WEB", title: "Сайт", note_id: "n", path: "p", issue_types: ["task"], issue_seq: 0, revision: 1,
  workflow: [{ id: "todo", name: "К работе", category: "todo" }, { id: "done", name: "Готово", category: "done" }],
} as TrackerProject;

test("a rule reads as a sentence, with the project's names", () => {
  expect(describeRule({
    name: "x", trigger: { type: "status_changed", to: "done" },
    conditions: [{ field: "priority", op: "is", value: "high" }],
    actions: [{ type: "notify", value: "reporter" }, { type: "add_label", value: "итог" }],
  }, project)).toBe("Когда статус меняется на «Готово», если приоритет — Высокий: сообщить автору, метка +итог.");
  expect(describeRule({
    name: "x", trigger: { type: "schedule", schedule: { at: "09:00", weekdays: [5, 1], tz: "UTC" } },
    conditions: [{ field: "due", op: "overdue" }], actions: [{ type: "set_status", value: "todo" }],
  }, project)).toBe("Пн, Пт в 09:00 (UTC), если срок прошёл: статус → К работе.");
});

test("presets are complete, and use a status the project has", () => {
  for (const p of presets(project)) expect(problems(p.definition)).toEqual([]);
  expect(presets(project)[2].definition.trigger.to).toBe("done");
});

test("the form says what is missing", () => {
  expect(problems({ name: " ", trigger: { type: "issue_created" }, conditions: [{ field: "label", op: "is" }], actions: [] }))
    .toEqual(["Назовите правило", "Добавьте действие", "Заполните условие: Метка"]);
  expect(problems({ name: "x", trigger: { type: "issue_created" }, conditions: [], actions: [{ type: "set_assignee" }] })).toEqual([]);
  expect(problems({ name: "x", trigger: { type: "issue_created" }, conditions: [], actions: [{ type: "add_comment", value: " " }] })).toHaveLength(1);
});
