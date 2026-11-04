# Веб-клиент Lumen

React 19, TypeScript, Next.js (App Router, статический экспорт) и TanStack Query.
Клиент работает с API `lumend`; production-сборка встраивается в Go-бинарник
([ADR-0018](../docs/adr/0018-nextjs-static-export.md)).

## Запуск

Из корня репозитория `make dev` запускает backend и Next на демонстрационной
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

- `src/app/` — маршруты: тонкие `page.tsx`, каждый отдаёт экран из `src/screens/`.
- `src/Shell.tsx` — провайдеры, вход и оболочка рабочего места.
- `src/lib/navigation.tsx`, `navigationGuard.ts` — навигация и защита черновиков.
- `src/proxy.dev.ts` — добавляет `LUMEN_TOKEN` к запросам API, только в `next dev`.
- `src/api/` — HTTP-клиент, DTO, SSE.
- `src/hooks/` — запросы и live-обновления через инвалидацию кэша.
- `src/screens/` — задачи, карточка задачи, заметки, календарь, поиск и настройки.
- `src/components/` — оболочка, Markdown, редакторы, диалоги и материалы.
- `src/lib/` — даты, ошибки, темы и вспомогательная логика.
- `src/styles.css` — общие токены и компоненты.
- `src/design/` — как Lumen использует Velessoft UI; `vendor/` — архивы `@villetacore/velessoft-ui` и `-tokens`.
- `public/fonts/` — локальные Manrope и JetBrains Mono с OFL-лицензиями.

Правила из-за статического экспорта ([ADR-0018](../docs/adr/0018-nextjs-static-export.md)):

- Новый экран — `src/screens/XPage.tsx` с `"use client"` и `src/app/x/page.tsx`.
- Маршрут с параметром экспортирует `generateStaticParams` с заглушкой `_`, параметр
  читается через `useRouteParam`, а не `useParams`.
- Ссылки и переходы — только из `lib/navigation`, иначе они минуют защиту черновика.
- SSR, route handlers и server actions недоступны.
- Каталог не называется `src/pages`: Next считает его Pages Router.

Светлая, тёмная и системная темы применяются до рендера. На узком экране sidebar
превращается в меню с управлением фокусом и Escape. Детали и ограничения переноса
макета — в [дизайн-системе](src/design/README.md).

Редакторы используют Markdown textarea. Draft отделён от query-данных, поэтому
refetch не должен заменять пользовательский ввод. При изменении редактора
проверяйте конфликт revision, удаление файла и попытку ухода.

Далее: [разработка](../docs/DEVELOPMENT.md), [руководство пользователя](../docs/USER_GUIDE.md),
[архитектура](../docs/ARCHITECTURE.md), [планы web](../docs/plans/WEB.md).
