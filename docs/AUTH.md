# Вход через Keycloak (и любой OIDC)

Lumen умеет жить за single sign-on: если на сервере уже стоит Keycloak и все
сервисы ходят через него, Lumen встраивается туда же и не заводит собственных
паролей.

## Три режима

| Режим | Когда | Как включается |
|---|---|---|
| Без авторизации | Локальное приложение на своей машине | По умолчанию |
| Статический токен | Скрипты, автоматизация, простой доступ извне | `--token` |
| OIDC / Keycloak | Сервер с несколькими пользователями и SSO | `--oidc-issuer` |

При включённом OIDC статический токен, если он задан, продолжает работать —
это удобный запасной путь для `curl` и cron.

## Настройка на стороне Keycloak

1. В нужном realm создайте клиента:
   - **Client ID**: `lumen`
   - **Client authentication**: `Off` для публичного клиента с PKCE, `On` —
     если хотите client secret. Lumen поддерживает оба.
   - **Valid redirect URIs**: `https://lumen.example.com/auth/callback`
   - **Valid post logout redirect URIs**: `https://lumen.example.com/*`
   - **Web origins**: `https://lumen.example.com`
2. При желании заведите роль (например `lumen-user`) и назначьте её тем, кому
   можно. Вместо роли годится и группа.

## Запуск

```bash
lumend \
  --vault /srv/vault \
  --addr 0.0.0.0:8787 \
  --public-url https://lumen.example.com \
  --oidc-issuer https://keycloak.example.com/realms/main \
  --oidc-client-id lumen \
  --oidc-roles lumen-user
```

С конфиденциальным клиентом добавьте ссылку на секрет — сам секрет в командной
строке не пишется:

```bash
  --oidc-client-secret bitwarden:lumen-oidc
  # или env:LUMEN_OIDC_SECRET, file:/run/secrets/oidc, command:…
```

Все флаги имеют парные переменные окружения: `LUMEN_OIDC_ISSUER`,
`LUMEN_OIDC_CLIENT_ID`, `LUMEN_OIDC_CLIENT_SECRET`, `LUMEN_OIDC_ROLES`,
`LUMEN_PUBLIC_URL`.

Discovery выполняется при старте: опечатка в адресе или спящий Keycloak
обнаружатся сразу, а не при первом входе.

## Как это работает

```mermaid
sequenceDiagram
    participant B as Браузер
    participant L as Lumen
    participant K as Keycloak
    B->>L: GET /auth/login?return_to=/calendar
    L->>L: генерирует state и PKCE-верификатор
    L-->>B: 302 на Keycloak (code_challenge=S256)
    B->>K: вход
    K-->>B: 302 /auth/callback?code=…&state=…
    B->>L: GET /auth/callback
    L->>K: обмен кода на токены (+ code_verifier)
    K-->>L: id_token, access_token
    L->>K: JWKS (кэш 15 минут)
    L->>L: проверка подписи, iss, aud/azp, срока, ролей
    L-->>B: cookie lumen_session (HttpOnly, SameSite=Lax) и 302 на /calendar
```

Токен наружу в JavaScript не отдаётся: браузер носит только идентификатор
сессии в HttpOnly-cookie, а сами токены остаются в памяти сервера.

## Токен Keycloak напрямую

Всё под `/api` принимает и `Authorization: Bearer <access_token>`, выданный тем
же realm. Так ходят десктопное приложение, скрипты и соседние сервисы:

```bash
TOKEN=$(curl -s -d grant_type=client_credentials -d client_id=lumen \
  -d client_secret=… https://keycloak.example.com/realms/main/protocol/openid-connect/token \
  | jq -r .access_token)

curl -H "Authorization: Bearer $TOKEN" https://lumen.example.com/api/v1/tasks?view=today
```

Проверяются подпись (RS256/384/512 по JWKS), издатель, срок действия и
принадлежность приложению: `aud` содержит client id **или** `azp` ему равен —
Keycloak заполняет их по-разному для id- и access-токенов.

## Что именно проверяется

- Подпись по ключу из JWKS. Неизвестный `kid` — повод перечитать JWKS
  (Keycloak ротирует ключи), но не повод поверить токену.
- `alg`: только RSA. `none` и HMAC отвергаются — это классическая подмена
  алгоритма.
- `iss` совпадает с настроенным realm: токен от соседнего сервиса не подойдёт.
- `exp` и `nbf` с допуском в минуту на расхождение часов.
- Роли из `realm_access.roles`, `resource_access.<client>.roles` и групп —
  если задан `--oidc-roles`.

## Права

`--oidc-roles lumen-user,admins` пускает тех, у кого есть хотя бы одна из
перечисленных ролей или групп. Пусто — пускает любого, кто прошёл вход в realm.

Разделения прав внутри Lumen пока нет: вошедший пользователь видит весь vault.
Разные vault для разных пользователей — это M3.

## Что дальше

- Обновление access-токена по refresh-токену (сейчас сессия живёт `--session-ttl`).
- Разные vault и права на уровне пользователя.
- Вход в десктопном приложении через системный браузер.
