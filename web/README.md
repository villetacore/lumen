# Веб-клиент Lumen

React 19, TypeScript, Vite, React Router и TanStack Query. Клиент работает с API
`lumend`; production-сборка встраивается в Go-бинарник.

## Запуск

Из корня репозитория `make dev` запускает backend и Vite на демонстрационной
копии vault. Для отдельного frontend из этого каталога:

```bash
npm ci
npm run dev
```

Интерфейс: http://localhost:5173. `/api` проксируется на `127.0.0.1:8787`.
Другой backend: `LUMEN_API=http://127.0.0.1:8788 npm run dev`.

## Проверки

```bash
npm run typecheck
npm test
npm run build
```

Vitest + jsdom проверяют сохранность текста заметки и описания задачи при
refetch, конфликте и уходе. Полный браузерный E2E пока не настроен.

## Устройство

- `src/api/` — HTTP-клиент, DTO, SSE.
- `src/hooks/` — запросы и live-обновления через инвалидацию кэша.
- `src/pages/` — задачи, карточка задачи, заметки, календарь, поиск и настройки.
- `src/components/` — оболочка, Markdown, редакторы, диалоги и материалы.
- `src/lib/` — даты, ошибки, темы и вспомогательная логика.
- `src/styles.css` — общие токены и компоненты.
- `src/design/` — исходные RIOT-токены и описание переноса.
- `public/fonts/` — локальные Manrope и JetBrains Mono с OFL-лицензиями.

Светлая, тёмная и системная темы применяются до рендера. На узком экране sidebar
превращается в меню с управлением фокусом и Escape. Детали и ограничения переноса
макета — в [дизайн-системе](src/design/README.md).

Редакторы используют Markdown textarea. Draft отделён от query-данных, поэтому
refetch не должен заменять пользовательский ввод. При изменении редактора
проверяйте конфликт revision, удаление файла и попытку ухода.

Далее: [разработка](../docs/DEVELOPMENT.md), [руководство пользователя](../docs/USER_GUIDE.md),
[архитектура](../docs/ARCHITECTURE.md), [планы web](../docs/plans/WEB.md).
