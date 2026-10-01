# Веб-клиент Lumen

React 19 + TypeScript + Vite. Тонкий клиент к `lumend`.

## Запуск

```bash
npm install
npm run dev          # http://localhost:5173, /api проксируется на 127.0.0.1:8787
```

Другой адрес сервера: `LUMEN_API=http://host:port npm run dev`.

## Команды

```bash
npm run build        # tsc -b && vite build
npm run typecheck
```

## Устройство

```
src/
├── api/client.ts        типизированный клиент + подписка на SSE
├── api/types.ts         DTO, зеркалящие server/internal/domain
├── hooks/               live-обновления
├── lib/date.ts          форматирование дат и недель
├── components/          TopBar, Sidebar, TaskRow, QuickAdd, AsidePanel
└── pages/               Tasks, Calendar, Notes, Note, Search
```

Данные — TanStack Query; изменения файлов на диске приходят по SSE
(`/api/v1/events`) и инвалидируют кэш, поэтому правка в Obsidian видна без
перезагрузки.

## Что дальше (M1)

- Редактор на CodeMirror 6 вместо textarea: автодополнение `[[`, клик по чекбоксу в тексте.
- Командная палитра на `Ctrl+K` (сейчас — только фокус в поиск).
- Kanban-доска и недельное представление календаря.
