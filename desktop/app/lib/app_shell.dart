import 'package:flutter/material.dart';

import 'data/lumen_repository.dart';
import 'models.dart';
import 'screens/calendar_screen.dart';
import 'screens/integrations_screen.dart';
import 'screens/note_screen.dart';
import 'screens/notes_screen.dart';
import 'screens/passwords_screen.dart';
import 'screens/search_screen.dart';
import 'screens/tasks_screen.dart';

/// Куда смотрит правая часть окна.
sealed class Destination {
  const Destination();
}

class TaskView extends Destination {
  const TaskView(this.view, this.title);
  final String view;
  final String title;
}

class CalendarView extends Destination {
  const CalendarView();
}

class NotesView extends Destination {
  const NotesView({this.tag});
  final String? tag;
}

class NoteView extends Destination {
  const NoteView(this.path);
  final String path;
}

class SearchView extends Destination {
  const SearchView();
}

class IntegrationsView extends Destination {
  const IntegrationsView();
}

class PasswordsView extends Destination {
  const PasswordsView();
}

const _views = [
  (id: 'today', label: 'Сегодня', icon: Icons.today_outlined),
  (id: 'inbox', label: 'Входящие', icon: Icons.inbox_outlined),
  (id: 'week', label: 'Неделя', icon: Icons.date_range_outlined),
  (id: 'overdue', label: 'Просрочено', icon: Icons.priority_high_outlined),
  (id: 'upcoming', label: 'Предстоит', icon: Icons.arrow_forward_outlined),
  (id: 'someday', label: 'Без даты', icon: Icons.all_inclusive_outlined),
  (id: 'all', label: 'Все задачи', icon: Icons.list_alt_outlined),
];

/// Каркас окна: слева навигация, справа активный экран. Набор разделов
/// повторяет веб-клиент, чтобы привычки переносились между ними.
class AppShell extends StatefulWidget {
  const AppShell({super.key, required this.repository});

  final LumenRepository repository;

  @override
  State<AppShell> createState() => _AppShellState();
}

class _AppShellState extends State<AppShell> {
  Destination _current = const TaskView('today', 'Сегодня');
  Destination? _previous;

  late Future<Identity> _me = widget.repository.me();
  late Future<List<TagCount>> _tags = widget.repository.tags();
  late Future<List<NoteSummary>> _projects =
      widget.repository.notes(type: 'project');

  void _go(Destination d) {
    setState(() {
      _previous = _current;
      _current = d;
    });
  }

  void _openNote(String path) => _go(NoteView(path));

  void _refreshSidebar() {
    setState(() {
      _tags = widget.repository.tags();
      _projects = widget.repository.notes(type: 'project');
      _me = widget.repository.me();
    });
  }

  bool _isActive(Destination d) => switch ((d, _current)) {
        (TaskView a, TaskView b) => a.view == b.view,
        (CalendarView _, CalendarView _) => true,
        (NotesView a, NotesView b) => a.tag == b.tag,
        (SearchView _, SearchView _) => true,
        (IntegrationsView _, IntegrationsView _) => true,
        (PasswordsView _, PasswordsView _) => true,
        _ => false,
      };

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: Row(
        children: [
          SizedBox(width: 232, child: _sidebar(context)),
          const VerticalDivider(),
          Expanded(
            child: Padding(
              padding: const EdgeInsets.fromLTRB(22, 16, 22, 10),
              child: _content(),
            ),
          ),
        ],
      ),
    );
  }

  Widget _content() {
    return switch (_current) {
      TaskView(:final view, :final title) =>
        TasksScreen(repository: widget.repository, view: view, title: title),
      CalendarView() =>
        CalendarScreen(repository: widget.repository, onOpenNote: _openNote),
      NotesView(:final tag) => NotesScreen(
          repository: widget.repository, onOpenNote: _openNote, tag: tag),
      NoteView(:final path) => NoteScreen(
          repository: widget.repository,
          path: path,
          onOpenNote: _openNote,
          onBack: () => _go(_previous ?? const NotesView()),
        ),
      SearchView() =>
        SearchScreen(repository: widget.repository, onOpenNote: _openNote),
      IntegrationsView() => IntegrationsScreen(repository: widget.repository),
      PasswordsView() => PasswordsScreen(repository: widget.repository),
    };
  }

  Widget _sidebar(BuildContext context) {
    final theme = Theme.of(context);

    return ListView(
      padding: const EdgeInsets.symmetric(vertical: 10),
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(14, 4, 14, 10),
          child: Row(
            children: [
              Container(
                width: 9,
                height: 9,
                decoration: BoxDecoration(
                  color: theme.colorScheme.primary,
                  shape: BoxShape.circle,
                ),
              ),
              const SizedBox(width: 8),
              const Text('Lumen',
                  style: TextStyle(fontWeight: FontWeight.w600)),
              const Spacer(),
              IconButton(
                iconSize: 16,
                visualDensity: VisualDensity.compact,
                tooltip: 'Обновить',
                onPressed: _refreshSidebar,
                icon: const Icon(Icons.refresh),
              ),
            ],
          ),
        ),
        for (final v in _views)
          _navItem(
            icon: v.icon,
            label: v.label,
            destination: TaskView(v.id, v.label),
          ),
        _group(context, 'Проекты'),
        FutureBuilder<List<NoteSummary>>(
          future: _projects,
          builder: (context, snapshot) {
            final projects = snapshot.data ?? const <NoteSummary>[];
            if (projects.isEmpty) {
              return const Padding(
                padding: EdgeInsets.symmetric(horizontal: 14, vertical: 4),
                child: Text('нет проектов', style: TextStyle(fontSize: 12)),
              );
            }
            return Column(
              children: [
                for (final p in projects)
                  ListTile(
                    dense: true,
                    visualDensity: VisualDensity.compact,
                    leading: const Icon(Icons.circle, size: 8),
                    title: Text(p.title, overflow: TextOverflow.ellipsis),
                    trailing: p.openTasks > 0 ? Text('${p.openTasks}') : null,
                    onTap: () => _openNote(p.path),
                  ),
              ],
            );
          },
        ),
        _group(context, 'Обзор'),
        _navItem(
            icon: Icons.calendar_month_outlined,
            label: 'Календарь',
            destination: const CalendarView()),
        _navItem(
            icon: Icons.description_outlined,
            label: 'Все заметки',
            destination: const NotesView()),
        _navItem(
            icon: Icons.search,
            label: 'Поиск',
            destination: const SearchView()),
        _navItem(
            icon: Icons.sync_alt,
            label: 'Интеграции',
            destination: const IntegrationsView()),
        _navItem(
            icon: Icons.key_outlined,
            label: 'Пароли',
            destination: const PasswordsView()),
        _group(context, 'Теги'),
        FutureBuilder<List<TagCount>>(
          future: _tags,
          builder: (context, snapshot) {
            final tags = snapshot.data ?? const <TagCount>[];
            return Column(
              children: [
                for (final t in tags.take(12))
                  ListTile(
                    dense: true,
                    visualDensity: VisualDensity.compact,
                    leading: const Text('#'),
                    title: Text(t.tag, overflow: TextOverflow.ellipsis),
                    trailing: Text('${t.count}'),
                    selected: _isActive(NotesView(tag: t.tag)),
                    onTap: () => _go(NotesView(tag: t.tag)),
                  ),
              ],
            );
          },
        ),
        const Divider(),
        FutureBuilder<Identity>(
          future: _me,
          builder: (context, snapshot) {
            final me = snapshot.data;
            if (me == null) return const SizedBox.shrink();
            return Padding(
              padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 6),
              child: Row(
                children: [
                  const Icon(Icons.person_outline, size: 16),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Text(
                      me.name ??
                          (me.authenticated
                              ? 'вход выполнен'
                              : 'не выполнен вход'),
                      style: theme.textTheme.labelSmall,
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                ],
              ),
            );
          },
        ),
      ],
    );
  }

  Widget _group(BuildContext context, String title) => Padding(
        padding: const EdgeInsets.fromLTRB(14, 14, 14, 4),
        child: Text(
          title.toUpperCase(),
          style: Theme.of(context)
              .textTheme
              .labelSmall
              ?.copyWith(fontWeight: FontWeight.w600),
        ),
      );

  Widget _navItem({
    required IconData icon,
    required String label,
    required Destination destination,
  }) =>
      ListTile(
        dense: true,
        visualDensity: VisualDensity.compact,
        leading: Icon(icon, size: 18),
        title: Text(label),
        selected: _isActive(destination),
        onTap: () => _go(destination),
      );
}
