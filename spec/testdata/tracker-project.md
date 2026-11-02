---
title: Сайт студии
type: project
key: WEB
issue_seq: 12
issue_types: [task, bug, story, epic]
workflow:
  - id: todo
    name: К работе
    category: todo
  - id: in-progress
    name: В работе
    category: in_progress
    wip: 3
  - id: review
    name: Проверка
    category: in_progress
  - id: done
    name: Готово
    category: done
transitions:
  todo: [in-progress]
  in-progress: [review, todo]
  review: [done, in-progress]
  done: [todo]
---

# Сайт студии

Описание проекта. Тикеты лежат в `WEB/issues/`.
