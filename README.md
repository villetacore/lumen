# Lumen

Локальный, файловый таск-трекер и база знаний: Markdown как источник правды,
календарь, заметки, задачи и интеграции.

> Obsidian-подобный vault (папка `.md` файлов) + сервер синхронизации + веб-клиент + десктоп.

## Принципы

1. **Файлы — источник правды.** Всё содержимое живёт в обычных `.md` файлах в папке
   (vault). Сервер и клиенты — это индекс + UI поверх файлов, а не «база с экспортом».
   При желании содержимое файлов шифруется — папка остаётся папкой.
2. **Local-first.** Десктоп работает полностью офлайн; сервер нужен только для
   синхронизации, веба и интеграций.
3. **Совместимость с Obsidian.** YAML frontmatter, `[[wikilinks]]`, `#tags`,
   `- [ ]` чекбоксы, `^block-id`. Vault Lumen открывается в Obsidian и наоборот.
4. **Одна спецификация формата, две реализации парсера** (Go и Rust), сверяемые
   общими golden-фикстурами в [`spec/testdata`](spec/testdata).

## Стек

| Компонент | Технологии | Каталог |
|---|---|---|
| Сервер | Go 1.26, stdlib `net/http`, SQLite (позже), zero-dep MVP | [`server/`](server) |
| Веб-клиент | React 19 + TypeScript + Vite + TanStack Query | [`web/`](web) |
| Десктоп: ядро | Rust (парсер, индекс, watcher, sync-клиент) | [`desktop/core/`](desktop/core) |
| Десктоп: UI | Flutter, через `flutter_rust_bridge` | [`desktop/app/`](desktop/app) |
| Формат данных | Markdown + YAML frontmatter | [`spec/`](spec) |

## Документация

- [MVP: границы и пользовательские сценарии](docs/MVP.md)
- [Архитектура](docs/ARCHITECTURE.md)
- [Модель данных](docs/DATA_MODEL.md)
- [HTTP API](docs/API.md)
- [Синхронизация](docs/SYNC.md)
- [Интеграции](docs/INTEGRATIONS.md)
- [Шифрование vault](docs/ENCRYPTION.md)
- [Секреты и менеджеры паролей](docs/SECRETS.md)
- [Вход через Keycloak](docs/AUTH.md)
- [Пароли в интерфейсе](docs/PASSWORDS.md)
- [UI-макет MVP](docs/UI_MOCKUP.md)
- [Дорожная карта](docs/ROADMAP.md)
- [Спецификация формата файлов](spec/FORMAT.md)
- [ADR (принятые решения)](docs/adr)

## Быстрый старт

### Одно приложение

Собирает веб-клиент, вшивает его в бинарник и запускает — ни Node, ни отдельного
фронтенда для работы не нужно.

```bash
make app                                    # или: ./scripts/build.ps1
./dist/lumend --vault ./vault-example --open
```

Откроется `http://127.0.0.1:8787`: задачи, календарь, заметки, поиск.

### Зашифрованное локальное хранилище

```bash
./dist/lumend crypt init --vault ./notes          # спросит парольную фразу
LUMEN_PASSPHRASE='…' ./dist/lumend --vault ./notes --open
```

Фразу можно не вводить руками, а взять из менеджера паролей:

```bash
./dist/lumend --vault ./notes --passphrase-from bitwarden:lumen-vault
./dist/lumend --vault ./notes --passphrase-from "keepass:D:/keys/db.kdbx#Lumen/vault?password=env:KDBX_PASSWORD"
```

Содержимое каждой заметки на диске — AES-256-GCM контейнер; ключ данных лежит
обёрнутым в `.lumen/crypt.json`. Что именно скрыто, а что нет —
[docs/ENCRYPTION.md](docs/ENCRYPTION.md). Вернуться к обычному markdown можно
всегда: `lumend crypt decrypt`.

### Десктопное приложение

```powershell
./scripts/desktop.ps1 -Run       # соберёт и запустит; нужен работающий lumend
```

Требуются Flutter (stable) и Visual Studio с рабочей нагрузкой C++.
Если сборка падает с `MSB8066` и кодом 255 — это известная проблема окружения,
а не проекта: разбор и лечение в [desktop/app/README.md](desktop/app/README.md).

### Режим разработки

```bash
cd server && go run ./cmd/lumend --vault ../vault-example   # API на :8787
cd web && npm install && npm run dev                        # UI на :5173
```

### Сервер для нескольких устройств

```bash
lumend --addr 0.0.0.0:8787 --token "$(openssl rand -hex 16)" --vault /srv/vault
```

### Сервер за Keycloak

Если на сервере уже стоит single sign-on, Lumen встраивается туда же и своих
паролей не заводит:

```bash
lumend --vault /srv/vault --addr 0.0.0.0:8787   --public-url https://lumen.example.com   --oidc-issuer https://keycloak.example.com/realms/main   --oidc-client-id lumen --oidc-roles lumen-user
```

Подробности — [docs/AUTH.md](docs/AUTH.md).

### Пароли

Раздел **Пароли** показывает записи из Bitwarden, Vaultwarden или KeePass —
только чтение, значения запрашиваются по одному и никуда не сохраняются.
См. [docs/PASSWORDS.md](docs/PASSWORDS.md).

### Интеграции

Календари и трекеры подключаются в разделе **Интеграции**: ICS-подписка
(Google, Outlook, Яндекс), CalDAV (Nextcloud, Radicale, Baikal),
Nextcloud Notes и GitHub/Gitea — issues превращаются в задачи. Токены в настройках не хранятся:
указывается ссылка вида `bitwarden:github-token`.
Подробности — [docs/INTEGRATIONS.md](docs/INTEGRATIONS.md).

## Статус

Работают: индексация vault, задачи и календарь, поиск, точечная запись в `.md`,
слежение за файлами через fsnotify, шифрование, секреты из менеджеров паролей,
вход через Keycloak, чтение паролей из Bitwarden/Vaultwarden/KeePass,
интеграции ICS / CalDAV / Nextcloud / GitHub, единый бинарник со встроенным UI
и десктопное приложение с тем же набором экранов.
См. [ROADMAP](docs/ROADMAP.md) — идёт этап M1–M3.
