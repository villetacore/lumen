---
id: WEB-12
title: Канбан-доска проекта
type: issue
project: WEB
issue_type: story
status: in-progress
priority: high
assignee: alice
reporter: bob
watchers: [carol]
labels: [web, board]
epic: WEB-1
sprint: WEB-S3
start: 2026-11-03
due: 2026-11-10
estimate: 1d
points: 3
rank: i
links:
  - blocks: WEB-14
  - relates: [WEB-2, WEB-3]
created: 2026-11-02T10:00:00Z
custom_field: сохраняется при записи
---

Колонки строятся по workflow проекта, см. [[Сайт студии]].

```mermaid
flowchart LR
  todo --> in-progress --> done
```

## Чек-лист
- [x] Колонки по workflow ^w1a
- [ ] Перетаскивание карточек [due:: 2026-11-08] ^w1b

## Комментарии

### 2026-11-02 12:30 · alice ^c-7f3a
Начала с макета, @bob посмотри.

```text
### это не новый комментарий
```

### 2026-11-02 13:05 · bob ^c-8b21
- [ ] задача из комментария не входит в чек-лист ^w1c
