import { useEffect, useRef, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Alert, Button, Dialog, IconButton, Input, Select, Textarea } from "@villetacore/velessoft-ui";
import { api } from "../api/client";
import type { AutomationAction, AutomationCondition, AutomationDefinition, AutomationRule, TrackerProject } from "../api/types";
import { ACTIONS, CONDITION_FIELDS, CONDITION_OPS, NOTIFY_TARGETS, TRIGGERS, WATCHED_FIELDS, WEEKDAYS, browserZone, opNeedsValue, opsFor, problems } from "../lib/automation";
import { errorText, isConflict } from "../lib/errors";
import { PeopleOptions } from "../lib/people";
import { CATEGORY_LABEL, PRIORITIES, typeLabel } from "../lib/tracker";
import Icon from "./Icon";

const blank: AutomationDefinition = { name: "", trigger: { type: "status_changed" }, conditions: [], actions: [{ type: "add_comment", value: "" }] };

/**
 * AutomationDialog edits one rule. The draft is local state: a refetch of the list
 * (any change in the vault triggers one) does not touch it, and a rule changed by
 * someone else is reported at save time with the text kept.
 */
export default function AutomationDialog({ project, rule, latestRevision, initial, onClose }: {
  project: TrackerProject;
  rule?: AutomationRule;
  /** The rule's revision in the freshest list, for "save over". */
  latestRevision?: number;
  initial?: AutomationDefinition;
  onClose: () => void;
}) {
  const qc = useQueryClient();
  const start = useRef<AutomationDefinition>(rule
    ? { name: rule.name, trigger: rule.trigger, conditions: rule.conditions ?? [], actions: rule.actions }
    : initial ?? blank);
  const [d, setD] = useState<AutomationDefinition>(start.current);
  // The version this draft was made from: the prop follows refetches, and saving
  // against the newest one would silently overwrite someone else's change.
  const baseRevision = useRef(rule?.revision);
  const dirty = JSON.stringify(d) !== JSON.stringify(start.current) || (!rule && !!initial);
  const missing = problems(d);

  const save = useMutation({
    mutationFn: (revision?: number) => rule
      ? api.patchAutomation(rule.id, { definition: d, revision: revision ?? baseRevision.current ?? rule.revision })
      : api.createAutomation(project.key, d),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["automations", project.key] }); onClose(); },
  });
  const close = () => {
    if (!save.isPending && (!dirty || window.confirm("Закрыть без сохранения?"))) onClose();
  };
  useEffect(() => {
    const onUnload = (e: BeforeUnloadEvent) => { if (dirty) e.preventDefault(); };
    window.addEventListener("beforeunload", onUnload);
    return () => window.removeEventListener("beforeunload", onUnload);
  }, [dirty]);

  const trigger = d.trigger;
  const setTrigger = (t: Partial<AutomationDefinition["trigger"]>) => setD({ ...d, trigger: { ...trigger, ...t } });
  const chooseTrigger = (type: AutomationDefinition["trigger"]["type"]) => setD({ ...d, trigger: {
    type,
    ...(type === "field_changed" ? { field: "assignee" } : {}),
    ...(type === "schedule" ? { schedule: { at: "09:00", weekdays: [1, 2, 3, 4, 5], tz: browserZone() } } : {}),
  } });
  const schedule = trigger.schedule;
  const toggleDay = (n: number) => {
    const days = new Set(schedule?.weekdays ?? []);
    if (days.has(n)) days.delete(n); else days.add(n);
    setTrigger({ schedule: { at: schedule?.at ?? "09:00", tz: schedule?.tz ?? browserZone(), weekdays: [...days].sort() } });
  };
  const statusOptions = project.workflow.map((s) => <option key={s.id} value={s.id}>{s.name}</option>);

  const setCondition = (i: number, c: Partial<AutomationCondition>) =>
    setD({ ...d, conditions: d.conditions.map((x, j) => (j === i ? { ...x, ...c } : x)) });
  const setAction = (i: number, a: Partial<AutomationAction>) =>
    setD({ ...d, actions: d.actions.map((x, j) => (j === i ? { ...x, ...a } : x)) });

  const conditionValue = (c: AutomationCondition, i: number) => {
    const set = (e: { target: { value: string } }) => setCondition(i, { value: e.target.value });
    const v = c.value ?? "";
    switch (c.field) {
      case "status": return <Select label="Значение" value={v} onChange={set}><option value="">Выберите</option>{statusOptions}</Select>;
      case "category": return <Select label="Значение" value={v} onChange={set}><option value="">Выберите</option>
        {Object.entries(CATEGORY_LABEL).map(([id, label]) => <option key={id} value={id}>{label}</option>)}</Select>;
      case "priority": return <Select label="Значение" value={v} onChange={set}><option value="">Выберите</option>
        {PRIORITIES.map((p) => <option key={p.id} value={p.id}>{p.label}</option>)}</Select>;
      case "issue_type": return <Select label="Значение" value={v} onChange={set}><option value="">Выберите</option>
        {project.issue_types.map((t) => <option key={t} value={t}>{typeLabel(t)}</option>)}</Select>;
      case "due": return <Input label="Дней" type="number" min={0} value={v} onChange={set} />;
      case "assignee": return <Input label="Значение" value={v} onChange={set} list="automation-people" />;
    }
    return <Input label="Значение" value={v} onChange={set} />;
  };

  const actionValue = (a: AutomationAction, i: number) => {
    const set = (e: { target: { value: string } }) => setAction(i, { value: e.target.value });
    const v = a.value ?? "";
    const hint = ACTIONS[a.type]?.hint;
    switch (a.type) {
      case "set_status": return <Select label="Статус" value={v} onChange={set}><option value="">Выберите</option>{statusOptions}</Select>;
      case "set_priority": return <Select label="Приоритет" value={v} onChange={set}><option value="">Не задан</option>
        {PRIORITIES.map((p) => <option key={p.id} value={p.id}>{p.label}</option>)}</Select>;
      case "notify": {
        const person = !!v && !NOTIFY_TARGETS[v];
        return <div className="form-row">
          <Select label="Кому" value={person ? "@" : v} onChange={(e) => setAction(i, { value: e.target.value })}>
            <option value="">Выберите</option>
            {Object.entries(NOTIFY_TARGETS).map(([id, label]) => <option key={id} value={id}>{label}</option>)}
            <option value="@">конкретному человеку</option>
          </Select>
          {person && <Input label="Логин" value={v.slice(1)} list="automation-people" onChange={(e) => setAction(i, { value: "@" + e.target.value.trim() })} />}
        </div>;
      }
      case "add_comment": return <Textarea label="Текст" value={v} onChange={set} hint={hint} />;
      case "set_due": return <Input label="Дней" type="number" min={0} value={v} onChange={set} hint={hint} />;
      case "set_assignee": return <Input label="Исполнитель" value={v} onChange={set} hint={hint} list="automation-people" />;
    }
    return <Input label={a.type === "create_subissue" ? "Название" : "Метка"} value={v} onChange={set} hint={hint} />;
  };

  const conflict = save.isError && isConflict(save.error);
  return <Dialog open onClose={close} closeOnBackdrop={!dirty} title={rule ? "Правило автоматизации" : "Новое правило"} footer={<>
    <Button onClick={close} disabled={save.isPending}>Отмена</Button>
    <Button type="submit" form="automation-form" variant="primary" loading={save.isPending} disabled={missing.length > 0 || conflict}>Сохранить</Button>
  </>}>
    <form id="automation-form" className="issue-form automation-form" onSubmit={(e) => { e.preventDefault(); if (!missing.length && !save.isPending) save.mutate(undefined); }}>
      <PeopleOptions id="automation-people" />
      <Input label="Название" required value={d.name} onChange={(e) => setD({ ...d, name: e.target.value })} />

      <fieldset>
        <legend>Когда</legend>
        <Select label="Событие" value={trigger.type} onChange={(e) => chooseTrigger(e.target.value as AutomationDefinition["trigger"]["type"])}>
          {TRIGGERS.map((t) => <option key={t.id} value={t.id}>{t.label}</option>)}
        </Select>
        {trigger.type === "status_changed" && <div className="form-row">
          <Select label="Из статуса" value={trigger.from ?? ""} onChange={(e) => setTrigger({ from: e.target.value || undefined })}><option value="">Любого</option>{statusOptions}</Select>
          <Select label="В статус" value={trigger.to ?? ""} onChange={(e) => setTrigger({ to: e.target.value || undefined })}><option value="">Любой</option>{statusOptions}</Select>
        </div>}
        {trigger.type === "field_changed" && <div className="form-row">
          <Select label="Поле" value={trigger.field ?? ""} onChange={(e) => setTrigger({ field: e.target.value })}>
            {Object.entries(WATCHED_FIELDS).map(([id, label]) => <option key={id} value={id}>{label}</option>)}
          </Select>
          <Input label="Новое значение" hint="Пусто — любое" value={trigger.to ?? ""} onChange={(e) => setTrigger({ to: e.target.value || undefined })} />
        </div>}
        {trigger.type === "schedule" && <>
          <div className="form-row">
            <Input label="Время" type="time" value={schedule?.at ?? "09:00"} onChange={(e) => setTrigger({ schedule: { at: e.target.value, tz: schedule?.tz ?? browserZone(), weekdays: schedule?.weekdays } })} />
            <Input label="Часовой пояс" value={schedule?.tz ?? ""} onChange={(e) => setTrigger({ schedule: { at: schedule?.at ?? "09:00", tz: e.target.value, weekdays: schedule?.weekdays } })} />
          </div>
          <div className="weekday-picker" role="group" aria-label="Дни недели">
            {WEEKDAYS.map((w, i) => <button type="button" key={w} aria-pressed={!!schedule?.weekdays?.includes(i + 1)} onClick={() => toggleDay(i + 1)}>{w}</button>)}
          </div>
          <p className="hint">Без выбранных дней — каждый день. Правило проверит все тикеты проекта, подходящие под условия.</p>
        </>}
      </fieldset>

      <fieldset>
        <legend>Если</legend>
        {d.conditions.length === 0 && <p className="hint">Без условий правило сработает на любом тикете проекта.</p>}
        {d.conditions.map((c, i) => <div className="automation-line" key={i}>
          <Select label="Поле" value={c.field} onChange={(e) => {
            const field = e.target.value;
            setCondition(i, { field, op: opsFor(field)[0], value: "" });
          }}>
            {Object.entries(CONDITION_FIELDS).map(([id, label]) => <option key={id} value={id}>{label}</option>)}
          </Select>
          <Select label="Условие" value={c.op} onChange={(e) => setCondition(i, { op: e.target.value, value: opNeedsValue(e.target.value) ? c.value : "" })}>
            {opsFor(c.field).map((op) => <option key={op} value={op}>{CONDITION_OPS[op]}</option>)}
          </Select>
          {opNeedsValue(c.op) && conditionValue(c, i)}
          <IconButton variant="ghost" label="Убрать условие" onClick={() => setD({ ...d, conditions: d.conditions.filter((_, j) => j !== i) })}><Icon name="close" size={14} /></IconButton>
        </div>)}
        <Button size="sm" onClick={() => setD({ ...d, conditions: [...d.conditions, { field: "status", op: "is", value: "" }] })}><Icon name="plus" size={14} />Условие</Button>
      </fieldset>

      <fieldset>
        <legend>То</legend>
        {d.actions.map((a, i) => <div className="automation-line" key={i}>
          <Select label="Действие" value={a.type} onChange={(e) => setAction(i, { type: e.target.value, value: "" })}>
            {Object.entries(ACTIONS).map(([id, x]) => <option key={id} value={id}>{x.label}</option>)}
          </Select>
          <div className="automation-value">{actionValue(a, i)}</div>
          <IconButton variant="ghost" label="Убрать действие" onClick={() => setD({ ...d, actions: d.actions.filter((_, j) => j !== i) })}><Icon name="close" size={14} /></IconButton>
        </div>)}
        <Button size="sm" onClick={() => setD({ ...d, actions: [...d.actions, { type: "add_label", value: "" }] })}><Icon name="plus" size={14} />Действие</Button>
        <p className="hint">Действия выполняются с правами того, кто сохранил правило. Изменения от автоматизаций не запускают другие правила.</p>
      </fieldset>

      {conflict && <Alert tone="warning" title="Правило изменили, пока вы его редактировали">
        Ваш вариант остаётся в форме.
        <div className="alert-actions"><Button size="sm" onClick={() => save.mutate(latestRevision)}>Сохранить поверх</Button></div>
      </Alert>}
      {save.isError && !conflict && <Alert tone="error" title="Правило не сохранено">{errorText(save.error)}</Alert>}
    </form>
  </Dialog>;
}
