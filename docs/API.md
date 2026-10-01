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
| `GET` | `/api/v1/me` | Кто вошёл и как: `{authenticated, mode, name, roles, login_url}` |
| `GET` | `/auth/login?return_to=/calendar` | Начать вход через OIDC |
| `GET` | `/auth/callback` | Возврат от провайдера |
| `GET`/`POST` | `/auth/logout` | Выйти |

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

Коды: `bad_request`, `unauthorized`, `not_found`, `conflict`, `unprocessable`,
`rate_limited`, `internal`.
