# Velessoft UI в Lumen

Источник дизайна — библиотека [velessoft-ui](https://github.com/villetacore/velessoft-ui) 0.2.0:
`@villetacore/velessoft-tokens` (CSS-переменные `--vs-*`) и `@villetacore/velessoft-ui`
(React-компоненты и их CSS). Оба пакета подключены в `../main.tsx`; архивы лежат
в `../../vendor/`, потому что в npm пакетов пока нет.

Правила:

- Цвет, шрифт, размеры — только токены `--vs-*`. Тема: `data-theme` на `<html>`
  (`light`, `dark`, `auto` для системной), см. `../lib/theme.ts`.
- Кнопки, поля, диалоги, бейджи, алерты, пустые состояния, спиннеры, статистика —
  компоненты библиотеки (`Button`, `Input`, `Select`, `Textarea`, `Dialog`, `Badge`,
  `Alert`, `EmptyState`, `Spinner`, `Stat`, `SegmentedControl`, `Switch`, `Tag`).
- `../styles.css` раскладывает приложение (оболочка, списки задач и документов,
  календарь, Markdown) и рисует оставшиеся классы `.btn`/`.input`/`.chip` так же,
  как их `vs-*`-аналоги, пока экраны интеграций и паролей не переведены на компоненты.
- Лайм — главное действие и выбранный пункт меню, фиолетовый — акцент и цепочки
  навигации. Ошибки не передаются только цветом.

Шрифты Manrope и JetBrains Mono лежат в `../../public/fonts/` с лицензиями OFL.
Прежний набор токенов RIOT v0.3 (velessoft-punk.zip) заменён пакетом токенов.
