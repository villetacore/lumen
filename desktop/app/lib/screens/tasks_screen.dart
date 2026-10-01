import 'package:flutter/material.dart';

import '../data/lumen_repository.dart';
import '../models.dart';
import '../widgets/config_form.dart';
import '../widgets/task_tile.dart';

/// Список задач одного представления, с группировкой и быстрым вводом —
/// то же, что показывает веб-клиент на /view/:view.
class TasksScreen extends StatefulWidget {
  const TasksScreen(
      {super.key,
      required this.repository,
      required this.view,
      required this.title});

  final LumenRepository repository;
  final String view;
  final String title;

  @override
  State<TasksScreen> createState() => _TasksScreenState();
}

class _TasksScreenState extends State<TasksScreen> {
  late Future<List<Task>> _tasks = widget.repository.tasks(view: widget.view);
  final _quickAdd = TextEditingController();

  @override
  void didUpdateWidget(TasksScreen old) {
    super.didUpdateWidget(old);
    if (old.view != widget.view) _reload();
  }

  @override
  void dispose() {
    _quickAdd.dispose();
    super.dispose();
  }

  void _reload() {
    setState(() {
      _tasks = widget.repository.tasks(view: widget.view);
    });
  }

  Future<void> _toggle(Task task, TaskStatus status) async {
    try {
      await widget.repository.patchTask(task.id, status: status);
      _reload();
    } on ApiException catch (e) {
      _snack(e.message);
    }
  }

  Future<void> _add() async {
    final text = _quickAdd.text.trim();
    if (text.isEmpty) return;
    try {
      await widget.repository.createTask(text);
      _quickAdd.clear();
      _reload();
    } on ApiException catch (e) {
      _snack(e.message);
    }
  }

  void _snack(String message) {
    if (!mounted) return;
    ScaffoldMessenger.of(context)
        .showSnackBar(SnackBar(content: Text(message)));
  }

  /// Разбивка на секции повторяет веб: просрочено, сегодня, дальше, без даты.
  List<(String, List<Task>)> _group(List<Task> tasks) {
    if (widget.view == 'all' ||
        widget.view == 'inbox' ||
        widget.view == 'done') {
      final byNote = <String, List<Task>>{};
      for (final t in tasks) {
        byNote.putIfAbsent(t.notePath, () => []).add(t);
      }
      return byNote.entries.map((e) => (e.key, e.value)).toList();
    }

    final today = DateTime.now();
    final startOfToday = DateTime(today.year, today.month, today.day);
    final overdue = <Task>[],
        now = <Task>[],
        later = <Task>[],
        undated = <Task>[];
    for (final t in tasks) {
      final d = t.date;
      if (d == null) {
        undated.add(t);
      } else {
        final day = DateTime(d.year, d.month, d.day);
        if (day.isBefore(startOfToday)) {
          overdue.add(t);
        } else if (day == startOfToday) {
          now.add(t);
        } else {
          later.add(t);
        }
      }
    }
    return [
      ('Просрочено', overdue),
      ('Сегодня', now),
      ('Дальше', later),
      ('Без даты', undated),
    ].where((g) => g.$2.isNotEmpty).toList();
  }

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            Text(widget.title, style: Theme.of(context).textTheme.titleLarge),
            const Spacer(),
            IconButton(
                onPressed: _reload,
                icon: const Icon(Icons.refresh),
                tooltip: 'Обновить'),
          ],
        ),
        Expanded(
          child: FutureBuilder<List<Task>>(
            future: _tasks,
            builder: (context, snapshot) {
              if (snapshot.connectionState == ConnectionState.waiting) {
                return const Center(child: CircularProgressIndicator());
              }
              if (snapshot.hasError) {
                return ServerUnreachable(
                    message: '${snapshot.error}', onRetry: _reload);
              }
              final tasks = snapshot.data ?? const <Task>[];
              if (tasks.isEmpty) {
                return const Center(child: Text('Здесь пусто'));
              }
              final groups = _group(tasks);
              return ListView(
                children: [
                  for (final (label, items) in groups) ...[
                    SectionHead('$label  ${items.length}'),
                    for (final t in items)
                      TaskTile(
                          task: t, onToggle: (status) => _toggle(t, status)),
                  ],
                ],
              );
            },
          ),
        ),
        const Divider(),
        Row(
          children: [
            const Icon(Icons.add, size: 18),
            const SizedBox(width: 8),
            Expanded(
              child: TextField(
                controller: _quickAdd,
                onSubmitted: (_) => _add(),
                decoration: const InputDecoration(
                  border: InputBorder.none,
                  hintText: 'Быстрое добавление в дневную заметку…',
                ),
              ),
            ),
          ],
        ),
      ],
    );
  }
}

/// Одинаковое объяснение на всех экранах, когда сервер не отвечает.
class ServerUnreachable extends StatelessWidget {
  const ServerUnreachable(
      {super.key, required this.message, required this.onRetry});

  final String message;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(message, textAlign: TextAlign.center),
          const SizedBox(height: 8),
          const Text(
            'Проверьте, что запущен lumend:\n'
            'dist\\lumend.exe --vault .\\vault-example',
            textAlign: TextAlign.center,
          ),
          const SizedBox(height: 12),
          FilledButton(onPressed: onRetry, child: const Text('Повторить')),
        ],
      ),
    );
  }
}
