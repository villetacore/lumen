# HTTP API v1

Базовый префикс: `/api/v1`. Формат — JSON, UTF-8. Времена — RFC3339.
Даты — `YYYY-MM-DD`.

Аутентификация MVP: `Authorization: Bearer <token>`, токен задаётся в конфиге
(`--token` / `LUMEN_TOKEN`). Пустой токен отключает проверку — это режим по
умолчанию для локального запуска. Статика вшитого веб-клиента отдаётся без токена,
всё под `/api` — только с ним. Многопользовательский режим и JWT — M3.

`GET /` отдаёт вшитый веб-клиент, если он собран (`make app`), иначе —
страницу с инструкцией. Отключается флагом `--ui=false`.

Ответ `/stats` описывает и сам сервер:

```json
{
  "notes": 512, "tasks": 1840, "open_tasks": 260, "tags": 37,
  "last_indexed": "2026-09-08T11:46:03+03:00", "index_took_ms": 41,
  "vault_path": "/srv/vault",
  "encrypted": true, "ui_embedded": true, "auth_required": false
}
```

## Служебные

| Метод | Путь | Описание |
|---|---|---|
| `GET` | `/healthz` | Живость |
| `GET` | `/api/v1/stats` | Кол-во заметок, задач, время индексации, режим сервера |
| `POST` | `/api/v1/reindex` | Полная переиндексация vault |

## Подключение MCP-клиентов

Решение — [ADR-0019](adr/0019-oauth-for-mcp.md). Работает при входе через OIDC, иначе
эндпоинты OAuth отвечают 404.

| Метод и путь | Назначение |
|---|---|
| `GET /.well-known/oauth-protected-resource[/mcp]` | Метаданные ресурса (RFC 9728), без входа |
| `GET /.well-known/oauth-authorization-server` | Метаданные сервера авторизации (RFC 8414), без входа |
| `POST /oauth/register` | Регистрация публичного клиента (RFC 7591), без входа |
| `GET /oauth/authorize` | Проверка запроса и переход на экран разрешения |
| `POST /oauth/token` | Обмен кода с PKCE и обновление токена |
| `GET /api/v1/oauth/consent` | Что показать на экране разрешения |
| `POST /api/v1/oauth/consent` | Ответ человека; только с этого же сайта, возвращает `redirect_to` |
| `GET /api/v1/mcp/connection` | Адрес `/mcp` и способ входа: `oauth`, `token`, `none` |
| `GET /api/v1/mcp/connections` | Мои подключённые клиенты |
| `DELETE /api/v1/mcp/connections/{id}` | Отключить своего клиента; чужой — 404 |

## Заметки

| Метод | Путь | Описание |
|---|---|---|
| `GET` | `/api/v1/notes?type=&tag=&prefix=&limit=&offset=` | Список (без body) |
| `GET` | `/api/v1/notes/{id}` | Заметка целиком (`id` = ULID или путь в URL-encoding) |
| `POST` | `/api/v1/notes` | Создать: `{path, title?, type?, tags?, body?, template?}` |
| `PUT` | `/api/v1/notes/{id}` | Заменить содержимое: `{body, revision}` |
| `PATCH` | `/api/v1/notes/{id}` | Частично: `{title?, tags?, frontmatter?, revision}` |
| `DELETE` | `/api/v1/notes/{id}` | Удалить файл (в `.lumen/trash/`) |
| `GET` | `/api/v1/notes/{id}/backlinks` | Входящие ссылки |

`revision` обязателен на запись: несовпадение → `409 Conflict` с текущей версией.

### Пример
```json
{
  "id": "01K5Q9V3JX8ZQ2N6C1B7M4TDA0",
  "path": "20-projects/lumen.md",
  "title": "Запуск Lumen",
  "type": "project",
  "tags": ["work", "lumen"],
  "frontmatter": {"status": "active"},
  "body": "# Запуск Lumen\n\n- [ ] Написать спецификацию [due:: 2026-09-10] ^k5q9v3\n",
  "revision": 7,
  "hash": "sha256:9f2c…",
  "updated": "2026-09-08T12:31:11+03:00"
}
```

## Задачи

| Метод | Путь | Описание |
|---|---|---|
| `GET` | `/api/v1/tasks` | Запрос задач, параметры ниже |
| `GET` | `/api/v1/tasks/{id}` | Одна задача |
| `POST` | `/api/v1/tasks` | Добавить задачу: `{note, text, due?, prio?, project?, section?}` |
| `PATCH` | `/api/v1/tasks/{id}` | `{status?, text?, due?, scheduled?, prio?, est?, project?}` |
| `DELETE` | `/api/v1/tasks/{id}` | Удалить строку задачи |

Параметры `GET /tasks`:
`status` (csv), `due_before`, `due_after`, `scheduled_from`, `scheduled_to`,
`overdue=true`, `tag` (csv), `project`, `prefix`, `q`, `sort` (`due,-prio,path`),
`limit`, `offset`, `view` (`inbox|today|week|overdue|all`).

`PATCH` меняет только целевую строку в файле и возвращает обновлённую задачу
и новую `revision` заметки.

## Трекер

Проекты и тикеты — Markdown-файлы ([формат](../spec/FORMAT.md#7-трекер-проект-тикет-спринт),
[ADR-0010](adr/0010-issue-files.md)).

| Метод | Путь | Описание |
|---|---|---|
| `GET` | `/api/v1/tracker/projects` | Проекты с ключом: `{key, title, path, workflow, issue_types, transitions, issue_seq}` |
| `POST` | `/api/v1/tracker/projects` | `{key, title}` — новый проект; `{key, note}` — включить трекер у существующей заметки-проекта |
| `GET` | `/api/v1/tracker/projects/{key}` | Один проект |
| `GET` | `/api/v1/issues` | Список в порядке доски, без описания и комментариев. Параметры: `project`, `status` (csv), `category` (`todo\|in_progress\|done`), `assignee` (`none` — без исполнителя, `me` — вызывающий), `watcher` (автор, исполнитель или наблюдатель; `me`), `type`, `label` (csv), `sprint`, `epic`, `q`, `limit`, `offset` |
| `POST` | `/api/v1/issues` | `{project, title, issue_type?, status?, priority?, assignee?, labels?, epic?, parent?, sprint?, start?, due?, estimate?, points?, description?}`; ключ выдаёт сервер, `reporter` — вошедший пользователь |
| `GET` | `/api/v1/issues/{key}` | Тикет с `description`, `checklist`, `comments` |
| `POST` | `/api/v1/tasks/{id}/promote` | `{project}` — сделать из задачи тикет: строка в заметке становится ссылкой на него. Вернёт тикет |
| `PATCH` | `/api/v1/issues/{key}` | Любые поля создания, а также `watchers`, `revision`; пустая строка/список очищает поле |
| `POST` | `/api/v1/issues/{key}/move` | `{status?, before?, after?, sprint?, revision?}` — перенос на доске или в бэклоге: `before`/`after` — ключи соседей сверху/снизу; `sprint` — в спринт (`""` — в бэклог) |
| `POST` | `/api/v1/issues/{key}/comments` | `{body}` → комментарий `{id, author, at, body}` |
| `GET` | `/api/v1/issues/{key}/history` | Журнал изменений, старые сверху: `{id, kind: created\|changed\|deleted, field, old, new, actor, source: lumen\|external, at}`. Правки файла вне Lumen — `source: external` без автора; ранг доски не журналируется |
| `DELETE` | `/api/v1/issues/{key}` | Переместить файл тикета в корзину vault; номер не переиспользуется |
| `GET` | `/api/v1/tracker/projects/{key}/members` | Доступ к проекту: `{open, my_role, items: [{kind, name, role, added_at, added_by}]}` |
| `PUT` | `/api/v1/tracker/projects/{key}/members` | `{kind: user\|group, name, role: viewer\|member\|admin}` — добавить/изменить; нужен admin проекта; закрыть открытый проект может только admin пространства |
| `DELETE` | `/api/v1/tracker/projects/{key}/members/{kind}/{name}` | Убрать участника; без участников проект снова открыт |
| `GET` | `/api/v1/tracker/projects/{key}/sprints` | Спринты: активный, запланированные, закрытые (новые сверху) |
| `POST` | `/api/v1/tracker/projects/{key}/sprints` | `{name?, goal?, start?, end?}` — новый запланированный спринт `KEY-S<N>` |
| `GET` | `/api/v1/sprints/{id}` | Один спринт |
| `PATCH` | `/api/v1/sprints/{id}` | `{name?, goal?, start?, end?}` |
| `POST` | `/api/v1/sprints/{id}/start` | `{start?, end?, goal?}`; по умолчанию сегодня и +14 дней. Второй активный спринт — `409 sprint_state` |
| `POST` | `/api/v1/sprints/{id}/complete` | `{move_to?}` — куда незавершённые тикеты (пусто — бэклог) → `{sprint, moved}`. Повтор после сбоя дозавершает |

Уведомления: исполнитель — о назначении; упомянутые через `@логин` в описании
(однократно на тикет) или комментарии — об упоминании; наблюдатели (автор,
исполнитель, `watchers`) — о комментариях и смене статуса. Автор действия
уведомлений о себе не получает; повторный запрос без изменений их не создаёт.
Событие SSE `notification` приходит только адресату и не называет тикет.

Ошибки: недопустимый переход workflow — `409 bad_transition`; недопустимое
состояние спринта (второй активный, закрытый спринт для тикета) — `409 sprint_state`; устаревшая
`revision` — `409 conflict`; неизвестный статус/тип или пустой заголовок —
`422 unprocessable`.

## Календарь

| Метод | Путь | Описание |
|---|---|---|
| `GET` | `/api/v1/calendar?from=2026-09-01&to=2026-09-30&sources=note,task,daily,ics` | События диапазона, включая календари интеграций |
| `POST` | `/api/v1/calendar/move` | `{ref:"task:abc", date:"2026-09-11", field:"scheduled"}` — drag-n-drop |

## Поиск

| Метод | Путь | Описание |
|---|---|---|
| `GET` | `/api/v1/search?q=…&limit=20` | Полнотекстовый поиск, возвращает сниппеты |
| `GET` | `/api/v1/tags` | Все теги с количеством |

## Файлы (сырой доступ, для sync и десктопа)

| Метод | Путь | Описание |
|---|---|---|
| `GET` | `/api/v1/files?since=<cursor>` | Список файлов и версий |
| `GET` | `/api/v1/files/{path}` | Сырое содержимое |
| `PUT` | `/api/v1/files/{path}` | Записать, заголовок `If-Match: <revision>` |
| `DELETE` | `/api/v1/files/{path}` | Удалить |

## Синхронизация

| Метод | Путь | Описание |
|---|---|---|
| `GET` | `/api/v1/sync/changes?since=<cursor>` | Изменения с курсора |
| `POST` | `/api/v1/sync/push` | Пакет изменений клиента |
| `GET` | `/api/v1/sync/state` | Курсор сервера, кол-во файлов, общий хэш |

См. [SYNC.md](SYNC.md).

## События реального времени

`GET /api/v1/events` — Server-Sent Events (SSE). MVP использует SSE: он не тянет
зависимостей и работает из браузера через `EventSource`. WebSocket появится на M2
вместе с двусторонней синхронизацией десктопа. Токен можно передать как `?token=`,
потому что `EventSource` не умеет заголовки.

Сообщения:

```json
{"type":"note.changed","path":"40-daily/2026/2026-09-08.md","revision":12}
{"type":"note.deleted","path":"00-inbox/tmp.md"}
{"type":"task.changed","id":"k5q9v3","status":"done"}
{"type":"issue.changed","id":"WEB-12","path":"20-projects/WEB/issues/WEB-12.md"}
{"type":"issue.deleted","id":"WEB-12","path":"20-projects/WEB/issues/WEB-12.md"}
{"type":"sprint.changed","id":"WEB-S3","path":"20-projects/WEB/sprints/WEB-S3.md"}
{"type":"notification"}
{"type":"sync.progress","done":120,"total":340}
{"type":"reindex.finished","notes":512,"tasks":1840,"took_ms":410}
```

## Интеграции

| Метод | Путь | Описание |
|---|---|---|
| `GET` | `/api/v1/integrations` | Настроенные интеграции и их состояние |
| `GET` | `/api/v1/integrations/kinds` | Доступные коннекторы и схема их настроек |
| `POST` | `/api/v1/integrations` | Подключить: `{kind, name, interval?, folder?, values}` |
| `PATCH` | `/api/v1/integrations/{id}` | Включить/выключить, поменять интервал и значения |
| `DELETE` | `/api/v1/integrations/{id}` | Отключить (заметки остаются, события с календаря уходят) |
| `POST` | `/api/v1/integrations/{id}/sync` | Синхронизировать немедленно |

Значения полей типа `secret` — это **ссылки** на секрет, а не сами токены:
`bitwarden:github-token`, `keepass:db.kdbx#Работа/GitHub`, `env:GITHUB_TOKEN`,
`command:pass show work/gh`. См. [SECRETS.md](SECRETS.md).

```json
{
  "id": "work-calendar",
  "kind": "ics",
  "title": "Календарь ICS (Google, Outlook, Яндекс)",
  "name": "Рабочий календарь",
  "enabled": true,
  "interval": "15m0s",
  "values": {"url": "https://…/basic.ics", "calendar": "Работа"},
  "last_sync": "2026-09-08T12:31:11+03:00",
  "next_sync": "2026-09-08T12:46:11+03:00",
  "last_items": 17,
  "running": false
}
```

Ответ `POST /integrations/{id}/sync` описывает прогон и возвращает `200` даже
при неудаче — текст ошибки нужен интерфейсу:

```json
{"id":"work-calendar","items":17,"created":0,"updated":0,"events":17,
 "skipped":0,"took_ms":24,"error":""}
```

## Вход и пользователь

| Метод | Путь | Описание |
|---|---|---|
| `GET` | `/api/v1/me` | Кто вошёл и как: `{authenticated, mode, name, roles, role, login_url}`; `role` — роль пространства `viewer\|member\|admin` |
| `GET` | `/auth/login?return_to=/calendar` | Начать вход через OIDC |
| `GET` | `/auth/callback` | Возврат от провайдера |
| `GET`/`POST` | `/auth/logout` | Выйти |

| `GET` | `/api/v1/notifications?unread=true&limit=` | Уведомления вызывающего, новые сверху: `{items: [{id, kind, issue, ref, actor, created_at, read, title, project, status}], unread}`; `kind` — `assigned\|mentioned\|commented\|status`. Тикеты, которые читателю не видны, в выдачу не попадают |
| `POST` | `/api/v1/notifications/read` | `{ids: [..]}` — отметить прочитанными; пустое тело — все |
| `GET` | `/api/v1/users?q=` | Справочник: кто входил через OIDC — `{username, name, groups, role, first_seen, last_seen, email?}`; `email` только для admin |
| `GET` | `/api/v1/groups` | Группы Keycloak известных пользователей: `{name, users}` |

`mode` — `none`, `token` или `oidc`. `/api/v1/me` доступен без входа: клиенту
нужно узнать, как войти. См. [AUTH.md](AUTH.md).

## Пароли

| Метод | Путь | Описание |
|---|---|---|
| `GET` | `/api/v1/passwords/kinds` | Поддерживаемые менеджеры и схема настроек |
| `GET` | `/api/v1/passwords/stores` | Подключённые хранилища и их состояние |
| `POST` | `/api/v1/passwords/stores` | Подключить: `{kind, name, values}` |
| `PATCH` | `/api/v1/passwords/stores/{id}` | Изменить настройки |
| `DELETE` | `/api/v1/passwords/stores/{id}` | Убрать из Lumen (база не меняется) |
| `POST` | `/api/v1/passwords/stores/{id}/unlock` | `{passphrase}` — мастер-пароль используется и забывается |
| `POST` | `/api/v1/passwords/stores/{id}/lock` | Закрыть |
| `POST` | `/api/v1/passwords/lock` | Закрыть все |
| `GET` | `/api/v1/passwords/stores/{id}/items?q=` | Список записей без значений |
| `POST` | `/api/v1/passwords/stores/{id}/reveal` | `{item, field}` → одно значение |

`reveal` — единственный путь, которым значение покидает сервер. Ответ идёт с
`Cache-Control: no-store`, метод `POST` выбран, чтобы идентификатор записи не
попал в URL и логи. Раздел отключается, если сервер слушает сеть без входа.
Подробности — [PASSWORDS.md](PASSWORDS.md).

## Ошибки

```json
{"error": {"code": "conflict", "message": "revision mismatch", "details": {"current": 9}}}
```

Коды: `bad_request`, `unauthorized`, `forbidden`, `not_found`, `conflict`,
`unprocessable`, `rate_limited`, `internal`.

Права ([ADR-0012](adr/0012-access-policy.md)): объект, который вызывающему не
виден (закрытый проект), отвечает `404` и не попадает в списки, поиск, теги,
календарь, статистику и SSE. Запись без нужной роли в видимом объекте —
`403 forbidden`. Интеграции, хранилища паролей и `POST /reindex` — только для
роли `admin`.

## Вложенные материалы и документация

`POST /api/v1/notes` принимает дополнительные необязательные поля:

- `parent`: `note:<ID>` для проекта/документа или `task:<ID>` для задачи.
  Родитель должен существовать; для задач нужен устойчивый `block_id`
  (существующий `PATCH /tasks/{id}` с `{}` назначает его старой задаче).
- `content_kind`: `document`, `entry` или `description`. Последнее используется
  для одного подробного описания задачи.
- `path` можно опустить: сервер назначает путь. Для `description` путь всегда
  назначается сервером; повторное создание возвращает 409, не затирая текст.

`GET /api/v1/notes?parent=...&content_kind=...` фильтрует материалы на сервере.
В списке возвращаются `parent` и `content_kind`; в полном документе эти значения
находятся в `frontmatter`. Обновление текста использует существующий PUT с
`revision`; устаревшая версия возвращает 409.

Связи хранятся в Markdown frontmatter и восстанавливаются переиндексацией.
Вложенные документы могут иметь собственные записи и документы. Удаление
родителя не удаляет материалы каскадно: они доступны через общий список.
Существующие запросы с явным `path` без новых полей совместимы.
Это локальные связи текущего single-user режима; workspace ACL и общие
ResourceRef остаются отдельной задачей LUM-023 с её зависимостями.

## MCP и подтверждения

`/mcp` — MCP-сервер ([MCP.md](MCP.md), [ADR-0013](adr/0013-mcp-server.md)).
Изменения, предложенные через него, ждут решения пользователя:

| Метод | Путь | Описание |
|---|---|---|
| `GET` | `/api/v1/proposals?pending=true` | Свои предложения: `{id, kind, target, args, preview, state, result, error, created_at, expires_at, decided_at}` |
| `POST` | `/api/v1/proposals/{id}/approve` | Применить своё предложение; `state` станет `applied`, `stale` или `failed`. Повтор — `409 proposal_state` |
| `POST` | `/api/v1/proposals/{id}/reject` | Отклонить |

Чужое предложение отвечает `404`. SSE-событие `proposal` приходит только автору.

## Деревья и автоматизации

[ADR-0020](adr/0020-trees-and-automations.md). Правила доступа те же, что у тикетов: скрытое отвечает 404.

### Проекты и подзадачи

| Запрос | Что делает |
|---|---|
| `POST /tracker/projects` с `parent` | Создаёт подпроект |
| `PATCH /tracker/projects/{key}` `{title?, parent?, revision?}` | Переименовывает или переносит; `parent: ""` — верхний уровень. Если меняется действующий список участников, нужна роль admin в проекте |
| `GET /issues?project=KEY&subprojects=true` | Тикеты проекта и всего, что вложено |
| `GET /issues?parent=KEY` | Подзадачи тикета |
| `GET /tracker/projects/{key}/members` | В ответе `inherited_from`, если действует список родителя |

### Страницы

`POST /pages/move` `{id, parent?, before?, after?, revision?}` переносит документ. `parent`: пусто, `note:<id>`
или `task:<id>`. `before` — сосед сверху, `after` — сосед снизу; без них страница встаёт последней. Сводки заметок
содержат `rank`. Удаление заметки поднимает её детей на уровень выше; проект с подпроектами не удаляется (422).

### Автоматизации

| Запрос | Что делает | Роль в проекте |
|---|---|---|
| `GET /tracker/projects/{key}/automations` | Правила и `can_edit` | viewer |
| `POST /tracker/projects/{key}/automations` | Создаёт правило, включённым | admin |
| `PATCH /automations/{id}` `{definition?, enabled?, revision}` | Меняет; сохранивший становится владельцем; устаревшая ревизия — 409 | admin |
| `DELETE /automations/{id}` | Удаляет правило и журнал | admin |
| `POST /automations/{id}/run` | Запускает правило по расписанию сейчас, 202 | admin |
| `GET /tracker/projects/{key}/automation-runs?limit=` | Журнал, новые сверху | viewer |

```json
{
  "name": "Просроченные",
  "trigger": {"type": "schedule", "schedule": {"at": "09:00", "weekdays": [1, 2, 3, 4, 5], "tz": "Europe/Moscow"}},
  "conditions": [{"field": "due", "op": "overdue"}],
  "actions": [{"type": "notify", "value": "assignee"}]
}
```

- `trigger.type`: `issue_created`, `status_changed` (`from`, `to`), `field_changed` (`field`, `to`), `comment_added`, `schedule`.
- `conditions[].field`: `status`, `category`, `priority`, `assignee`, `issue_type`, `label`, `sprint`, `epic`, `parent`, `due`.
  `op`: `is`, `is_not`, `empty`, `not_empty`; для `due` — `overdue`, `within` (дней), `empty`, `not_empty`.
- `actions[].type`: `set_status`, `set_priority`, `set_assignee` (логин или `@reporter`), `add_label`, `remove_label`,
  `set_due` (дней от сегодня), `add_comment`, `notify` (`assignee`, `reporter`, `watchers`, `@логин`), `create_subissue`.
  В текстах подставляются `{{key}}`, `{{title}}`, `{{status}}`, `{{assignee}}`, `{{reporter}}`, `{{project}}`.
- Статус запуска: `done`, `failed`, `blocked` (нет прав), `throttled` (сработал предохранитель), `unknown` (прервано рестартом, не повторяется).
- SSE: `automation.run`. В журнале тикета такие правки имеют `source: automation`, в уведомлениях `kind: automation`.
