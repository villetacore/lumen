# Секреты: пароли, токены, менеджеры паролей

Lumen не хранит пароли и токены. Везде, где нужен секрет — парольная фраза
зашифрованного vault, токен GitHub, пароль CalDAV — указывается **ссылка на
секрет**: где его взять, а не сам секрет.

```
env:LUMEN_PASSPHRASE
file:/run/secrets/lumen
command:pass show lumen/vault
bitwarden:lumen-vault
keepass:D:/keys/personal.kdbx#Lumen/vault?password=env:KDBX_PASSWORD
```

Такие строки безопасно лежат в `.lumen/integrations.yaml`, в git и в скриншотах.

## Провайдеры

| Схема | Что делает | Пример |
|---|---|---|
| `env:` | Переменная окружения | `env:GITHUB_TOKEN` |
| `file:` | Первая строка файла (остальное — комментарий) | `file:/run/secrets/lumen` |
| `command:` | Любая команда, секрет — её stdout | `command:pass show lumen/vault` |
| `bitwarden:` | Bitwarden CLI (`bw`), он же Vaultwarden | `bitwarden:lumen-vault` |
| `keepass:` | KeePassXC CLI (`keepassxc-cli`) | `keepass:db.kdbx#Lumen/vault` |
| `literal:` | Значение прямо в строке | только для тестов |

Голый путь без схемы читается как `file:` — `--passphrase-from D:/keys/pass.txt`
работает как ожидается, включая букву диска.

Значения параметров сами могут быть ссылками (глубина вложенности ограничена),
поэтому пароль от базы KeePass берётся из окружения, а не из конфига.

## Bitwarden и Vaultwarden

Оба работают через официальный `bw` CLI: Vaultwarden — совместимая реализация
сервера, клиенту достаточно указать адрес.

```bash
npm install -g @bitwarden/cli

# только для Vaultwarden / self-hosted:
bw config server https://vault.example.com

bw login
export BW_SESSION=$(bw unlock --raw)     # сессия живёт до перезагрузки/lock
```

Дальше:

```bash
lumend --vault ./notes --passphrase-from bitwarden:lumen-vault
```

Параметры:

| Параметр | По умолчанию | Смысл |
|---|---|---|
| `field` | `password` | Что взять: `password`, `username`, `notes`, `totp` |
| `session` | `env:BW_SESSION` | Откуда взять сессию (сама может быть ссылкой) |
| `bw` | `bw` | Путь к бинарнику CLI |

```
bitwarden:lumen-vault?field=notes
bitwarden:c1f0…?session=file:/run/secrets/bw-session&bw=/opt/bin/bw
```

Если сессии нет, Lumen скажет об этом прямо и напомнит про `bw unlock --raw` —
он никогда не пытается запросить мастер-пароль сам.

## KeePass / KeePassXC

Чтение идёт через `keepassxc-cli`, который есть в любой установке KeePassXC.
Свой разбор `.kdbx` Lumen не делает намеренно — см.
[ADR-0007](adr/0007-secret-references.md).

```bash
# Windows: C:\Program Files\KeePassXC\keepassxc-cli.exe
keepassxc-cli --version
```

```
keepass:D:/keys/personal.kdbx#Lumen/vault?password=env:KDBX_PASSWORD
keepass:/home/a/db.kdbx#Работа/GitHub?key-file=/home/a/db.keyx&no-password=true
```

| Параметр | По умолчанию | Смысл |
|---|---|---|
| `password` | — | Ссылка на пароль базы; передаётся в stdin |
| `key-file` | — | Файл-ключ |
| `no-password` | `false` | База открывается только ключом |
| `attribute` | `Password` | Какое поле записи взять |
| `cli` | `keepassxc-cli` | Путь к бинарнику |

После `#` идёт путь записи внутри базы: `Группа/Подгруппа/Запись`.

## Всё остальное — через `command:`

`pass`, `gopass`, `1Password` (`op`), `age`, `gpg`, свой скрипт:

```
command:pass show lumen/vault
command:op read op://Personal/Lumen/password
command:gpg --quiet --decrypt /home/a/lumen.gpg
command:sh -c "curl -s https://vault.local/api/secret"      # аргументы в кавычках
```

Разбор аргументов — простой: пробелы разделяют, кавычки объединяют. Это не
оболочка: нет конвейеров, подстановок и globbing.

## Порядок поиска парольной фразы vault

1. `--passphrase-from <ссылка>` (или `LUMEN_PASSPHRASE_FROM`)
2. `--passphrase-file <файл>` (или `LUMEN_PASSPHRASE_FILE`)
3. Переменная `LUMEN_PASSPHRASE`
4. Стандартный ввод, если туда что-то передали по конвейеру
5. Запрос в терминале без эха

Неинтерактивные источники идут первыми: служба или контейнер никогда не зависнет
в ожидании ввода.

## Что Lumen делает и чего не делает

- Секрет живёт в памяти процесса ровно столько, сколько нужно для операции.
- Секрет никогда не пишется в лог, в конфиг и в ответы API — наружу уходит
  только ссылка (`bitwarden:lumen-vault`) и название провайдера в тексте ошибки.
- Lumen не спрашивает мастер-пароль от менеджера паролей и не хранит его.
  Разблокировка хранилища — задача самого менеджера.
- Вызов внешней команды ограничен 30 секундами, stdin закрыт, если туда нечего
  передавать: интерактивный запрос упадёт с понятной ошибкой, а не подвесит сервер.
