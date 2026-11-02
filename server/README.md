# lumend — сервер и приложение Lumen

Go 1.26. Зависимости: `gopkg.in/yaml.v3`, `github.com/fsnotify/fsnotify`,
`golang.org/x/term`. Всё остальное — стандартная библиотека.

Один бинарник делает всё: индексирует vault, отдаёт API, отдаёт веб-интерфейс
(если он вшит) и умеет шифровать хранилище.

## Запуск

```bash
go run ./cmd/lumend --vault ../vault-example            # разработка
../dist/lumend --vault ../vault-example --open          # собранное приложение
```

| Флаг | Env | По умолчанию | Смысл |
|---|---|---|---|
| `--addr` | `LUMEN_ADDR` | `127.0.0.1:8787` | Адрес прослушивания |
| `--vault` | `LUMEN_VAULT` | `./vault` | Папка vault |
| `--state` | `LUMEN_STATE` | `lumen-state` рядом с vault | Операционная БД, вне vault ([ADR-0011](../docs/adr/0011-operational-state.md)); `lumend state status\|backup\|restore` |
| `--token` | `LUMEN_TOKEN` | пусто | Bearer-токен для `/api`; пусто = без авторизации |
| `--cors` | `LUMEN_CORS` | `http://localhost:5173` | Разрешённые origin, `*` для всех |
| `--ui` | `LUMEN_UI` | `true` | Отдавать вшитый веб-клиент на `/` |
| `--open` | — | `false` | Открыть браузер после старта |
| `--watch` | `LUMEN_WATCH` | `true` | Следить за изменениями файлов |
| `--debounce` | — | `250ms` | Пауза перед переиндексацией изменённого файла |
| `--poll` | — | `2s` | Интервал опроса, если fsnotify недоступен |
| `--passphrase-from` | `LUMEN_PASSPHRASE_FROM` | пусто | Откуда взять фразу vault: `env:`, `file:`, `command:`, `bitwarden:`, `keepass:` |
| `--passphrase-file` | `LUMEN_PASSPHRASE_FILE` | пусто | То же, но только файл |
| `-v` | — | `false` | Подробный лог запросов |

Single sign-on (см. [../docs/AUTH.md](../docs/AUTH.md)):

| Флаг | Env | Смысл |
|---|---|---|
| `--public-url` | `LUMEN_PUBLIC_URL` | Внешний адрес сервера |
| `--oidc-issuer` | `LUMEN_OIDC_ISSUER` | `https://keycloak…/realms/main` |
| `--oidc-client-id` | `LUMEN_OIDC_CLIENT_ID` | Идентификатор клиента |
| `--oidc-client-secret` | `LUMEN_OIDC_CLIENT_SECRET` | Ссылка на секрет |
| `--oidc-roles` | `LUMEN_OIDC_ROLES` | Кого пускать: роли или группы |
| `--admin-roles` | `LUMEN_ADMIN_ROLES` | Роли/группы администраторов пространства; пусто — все допущенные администраторы ([ADR-0012](../docs/adr/0012-access-policy.md)) |
| `--viewer-roles` | `LUMEN_VIEWER_ROLES` | Роли/группы только для чтения |
| `--session-ttl` | — | Срок жизни сессии в браузере |

## Команды

```bash
lumend                      # то же, что lumend serve
lumend crypt status         # зашифрован ли vault
lumend crypt init           # включить шифрование и зашифровать заметки
lumend crypt encrypt        # дошифровать файлы, добавленные в обход Lumen
lumend crypt decrypt        # вернуть обычный markdown
lumend crypt passwd         # сменить парольную фразу
lumend version
```

Подробности про шифрование и модель угроз — [../docs/ENCRYPTION.md](../docs/ENCRYPTION.md),
про ссылки на секреты — [../docs/SECRETS.md](../docs/SECRETS.md).

## Сборка одного приложения

```bash
npm ci --prefix web   # из корня репозитория
make app
./scripts/build.ps1   # то же на Windows
```

Веб-клиент собирается Vite, копируется в `internal/webui/dist` и вшивается
через `go:embed`. Без этой сборки сервер работает, но на `/` показывает заглушку.

## Проверка

```bash
curl localhost:8787/healthz
curl "localhost:8787/api/v1/tasks?view=today"
curl -X PATCH -H 'Content-Type: application/json' \
     -d '{"status":"done"}' localhost:8787/api/v1/tasks/<id>
curl -N localhost:8787/api/v1/events        # поток изменений
```

## Тесты

```bash
go test ./...
```

- `internal/markdown` — golden-фикстуры из `spec/testdata`, общие с Rust-ядром ([ADR-0004](../docs/adr/0004-two-parser-implementations.md)).
- `internal/crypt` — контейнеры, привязка к пути, смена фразы.
- `internal/watch` — fsnotify, новые папки, дебаунс, фильтрация.
- `internal/auth` — проверка токенов Keycloak против поддельного провайдера: подпись, `iss`, `aud`/`azp`, срок, роли, `alg: none`.
- `internal/secrets` — провайдеры секретов с подменённым исполнителем команд, без реальных менеджеров паролей.
- `internal/passwords` — открытие хранилища, список без значений, раскрытие по одному полю; проверяется, что секреты не попадают в конфиг.
- `internal/integrations` — разбор iCalendar и повторяющихся событий, защита от SSRF, хранение конфигурации.
- `internal/service` — точечная запись задач, шифрованный vault, реакция на внешние правки, идемпотентность интеграций.

## Устройство

Зависимости идут строго внутрь:
`httpapi → service → {domain, index, vault, markdown, watch, crypt, secrets, integrations}`,
плюс `httpapi → {auth, passwords}` для входа и хранилищ паролей.
Пакет `domain` не знает ни про HTTP, ни про файлы; `vault.Codec` — единственный
шов, за которым живёт шифрование. Подробнее — [../docs/ARCHITECTURE.md](../docs/ARCHITECTURE.md).

## Развитие

Порядок реализации задают [IMPLEMENTATION_GUIDE](../docs/plans/IMPLEMENTATION_GUIDE.md)
и [BACKLOG](../docs/plans/BACKLOG.md). SQL persistence, sync API и агентный runtime
пока не реализованы. Текущие ограничения и приёмка — в
[PROGRESS](../docs/plans/PROGRESS.md). Эксплуатация и backup — в
[OPERATIONS](../docs/OPERATIONS.md).

## Восстановление интеграций

Календарные события хранятся в памяти. После запуска первая проверка расписания
(до 30 секунд) заново загружает включённые подключения, игнорируя сохранённый
ETag/cursor; далее используется обычный интервал и условные запросы. Для
восстановления календаря нужен доступ к источнику. Несколько процессов `lumend`
не должны одновременно писать в один vault.

Cursor записывается после полного применения результата; при ошибке применения
или записи state следующий запрос повторяет предыдущую страницу. Ошибка видна
в настройках подключения и возвращается при ручной синхронизации.

Импортированные материалы обновляют выделенный блок источника. Личные
дополнения снаружи сохраняются; ручная правка исходного текста останавливает
обновление с сообщением об ошибке. Для старых материалов перед первым обновлением
сохраняется точная копия в `.lumen/backups/imports/`, а весь прежний текст остаётся
в документе. Копия записывается через codec vault, включая его шифрование.
Новые ID импортированных задач учитывают подключение; прежние ID сохраняются.
