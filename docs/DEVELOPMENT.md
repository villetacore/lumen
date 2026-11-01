# Локальная разработка

Текущий цикл разработки охватывает `server/` (Go) и `web/` (React/TypeScript).
Перед изменениями прочитайте [CONTRIBUTING](../CONTRIBUTING.md) и [AGENTS.md](../AGENTS.md).

## Окружение

Go 1.26, Node.js 24+, npm, Git и Make. Устанавливайте frontend-зависимости через
`npm ci`, сохраняя lockfile. Для проверок backend/web Flutter и Rust не нужны.

```bash
npm ci --prefix web
make dev
```

API слушает `127.0.0.1:8787`, Vite — `http://localhost:5173`. При первом запуске
`vault-example` копируется в `.dev-vault`; повторный запуск сохраняет эту копию.
`Ctrl+C` останавливает оба процесса. Frontend обновляется автоматически;
изменения Go требуют перезапуска команды.

Для отдельного запуска в двух терминалах:

```bash
# Терминал 1, из корня репозитория
cd server
go run ./cmd/lumend --vault ../.dev-vault --ui=false
```

```bash
# Терминал 2, из корня репозитория
cd web
npm run dev
```

Прокси Vite направляет `/api` на backend. Другой адрес задаётся через
`LUMEN_API=http://127.0.0.1:8788 npm run dev`.
`make dev-prod` подключает **боевые данные** и отправляет туда изменения;
для тестов и скриншотов используйте локальный vault.

## Проверки перед pull request

```bash
(cd server && go vet ./... && go test -race ./...)
(cd web && npm run typecheck && npm test && npm run build)
git diff --check
```

Go-файлы должны быть отформатированы `gofmt`. `make check` выполняет базовую
проверку Go и web, но race detector и production-сборку запускайте командами выше.
Общий `make test` включает desktop и не нужен для изменений только backend/web.

Для проверки поставки:

```bash
make app VERSION=local-review
./dist/lumend --vault ./review-vault --addr 127.0.0.1:8788
curl -fsS http://127.0.0.1:8788/healthz
```

Храните `review-vault` вне git или используйте временную папку. Проверьте также
глубокую ссылку на заметку, обе темы и узкий экран, если меняется интерфейс.

## Карта кода

| Каталог | Ответственность |
|---|---|
| `server/internal/httpapi` | HTTP, DTO, SSE, авторизация запросов |
| `server/internal/service` | Сценарии, запись, импорт, индексация |
| `server/internal/vault`, `markdown`, `index` | Файлы, формат и индекс в памяти |
| `server/internal/integrations` | Коннекторы и состояние подключений |
| `web/src/api`, `hooks` | API-клиент, запросы и live-обновления |
| `web/src/pages`, `components` | Экраны и общие компоненты |
| `web/src/design`, `web/public/fonts` | RIOT-токены, шрифты и лицензии |
| `spec/testdata` | Golden-фикстуры формата |

API и типы клиента поддерживаются вручную; при изменении контракта обновите
обе стороны и [API.md](API.md). Сохранность draft, повтор импорта и конфликт
записи проверяются отдельными регрессионными тестами. Полный браузерный E2E
пока не настроен; компонентные тесты его не заменяют.

Поставка: [CI → образ → сервер](DEPLOY.md). Приоритеты:
[IMPLEMENTATION_GUIDE](plans/IMPLEMENTATION_GUIDE.md) и [BACKLOG](plans/BACKLOG.md).
