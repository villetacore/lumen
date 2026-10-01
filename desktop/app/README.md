# Lumen Desktop (Flutter)

UI десктопного клиента. Вся логика — в [`../core`](../core) (Rust).

## Состояние

Набор экранов совпадает с веб-клиентом:

| Раздел | Что умеет |
|---|---|
| Задачи | Семь представлений, группировка, переключение статуса, быстрый ввод |
| Календарь | Месячная сетка, события интеграций, перетаскивание задач по дням |
| Заметки | Список, фильтр по тегу, просмотр и правка, обратные ссылки |
| Поиск | Полнотекстовый по vault |
| Интеграции | Подключение, включение, синхронизация, удаление |
| Пароли | Bitwarden/Vaultwarden и KeePass: открыть, найти, показать поле, скопировать |

Экраны построены поверх `LumenRepository`; сейчас есть одна реализация —
`HttpRepository`, которая ходит в запущенный `lumend`. На M2 добавится
`NativeRepository` поверх `lumen-core` через `flutter_rust_bridge` — офлайн, без
сервера, с тем же интерфейсом и теми же экранами.

## Сборка и запуск

```powershell
./scripts/desktop.ps1              # собрать релиз
./scripts/desktop.ps1 -Run         # собрать и запустить
./scripts/desktop.ps1 -Test        # flutter analyze + flutter test
./scripts/desktop.ps1 -Debug       # отладочная сборка
```

Приложению нужен работающий сервер:

```powershell
dist\lumend.exe --vault .\vault-example
```

Результат сборки — `desktop/app/build/windows/x64/runner/Release/lumen_app.exe`.

Требования: Flutter (stable) и Visual Studio с рабочей нагрузкой
«Разработка классических приложений на C++». `flutter doctor` должен показывать
зелёными строки Windows и Visual Studio; Android и Chrome для десктопа не нужны.

## Если сборка падает с MSB8066 и кодом 255

Симптом:

```
error MSB8066: пользовательская сборка для "...flutter_assemble.rule"
завершила работу с кодом 255
```

а под `flutter build windows -v` видно:

```
Unhandled exception:
FormatException: Unexpected extension byte (at offset 0)
```

**Причина не в проекте.** Цепочка такая:

1. В `PATH` есть некорректная запись — например `C:\Progra~1\Java\<graalvm>\bin`
   с угловыми скобками или литеральный `%PATH%`.
2. Из-за неё `flutter.bat` при каждом запуске пишет в stderr
   «The system cannot find the file specified». Сам по себе этот шум безобиден.
3. `tool_backend.dart` из Flutter SDK, который CMake вызывает на шаге
   `flutter_assemble`, пропускает stdout и stderr дочернего процесса через
   строгий `utf8.decoder`:

   ```dart
   assembleProcess.stderr.transform(utf8.decoder)
   ```

4. На локализованной Windows это сообщение приходит в cp1251. Первый же байт —
   кириллический, для UTF-8 он невалиден, декодер бросает `FormatException`,
   процесс падает с кодом 255, а MSBuild показывает только обёртку MSB8066.

Проверить, что дело именно в этом:

```powershell
cmd /c "flutter.bat --version 2>&1 1>NUL"     # если что-то печатает — причина в PATH
```

`scripts/desktop.ps1` обходит проблему: он выкидывает битые записи из `PATH`
внутри своего процесса, систему не трогая. Этого достаточно, чтобы собирать.

### Как починить насовсем

Нужны права администратора — PATH машины лежит в HKLM. В консоли **от имени
администратора**:

```powershell
$p = [Environment]::GetEnvironmentVariable("Path", "Machine")
$fixed = ($p -split ';' | Where-Object { $_ -and $_ -notmatch '[<>%]' } | Select-Object -Unique) -join ';'
[Environment]::SetEnvironmentVariable("Path", $fixed, "Machine")
```

Команда убирает записи с `<`, `>`, `%` и дубликаты. Перед этим стоит сохранить
текущее значение: `$p | Out-File $HOME\path-backup.txt`. После — перелогиниться,
чтобы новые процессы увидели изменения.

## Мост к Rust (M2)

```bash
cargo install flutter_rust_bridge_codegen
flutter_rust_bridge_codegen generate       # конфиг в flutter_rust_bridge.yaml
```

Мост будет описан в `../core/src/api.rs`: открытие vault, запросы задач,
патч задачи, поток событий файлового watcher и прогресс синхронизации.

## Структура

```
lib/
├── main.dart                     точка входа, тема, выбор репозитория
├── app_shell.dart                каркас окна: навигация и переключение экранов
├── theme.dart                    тема, общая с веб-клиентом по духу
├── models.dart                   DTO: Task, Note, CalendarEvent, Integration, PasswordItem…
├── data/lumen_repository.dart    интерфейс источника данных + HTTP-реализация
├── screens/                      tasks, calendar, notes, note, search, integrations, passwords
└── widgets/                      строка задачи, форма настроек по схеме сервера
test/
└── widget_test.dart              11 смоук-тестов с подменённым репозиторием
```

Платформенные папки (`windows/`, `build/`) генерируются локально и в git не
хранятся:

```powershell
flutter create --platforms=windows --project-name lumen_app .
```
