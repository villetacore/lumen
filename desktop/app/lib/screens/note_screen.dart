import 'package:flutter/material.dart';

import '../data/lumen_repository.dart';
import '../models.dart';
import '../widgets/config_form.dart';
import '../widgets/task_tile.dart';
import 'tasks_screen.dart';

/// Заметка: задачи, содержимое с правкой и обратные ссылки — как /note/* в вебе.
class NoteScreen extends StatefulWidget {
  const NoteScreen({
    super.key,
    required this.repository,
    required this.path,
    required this.onOpenNote,
    required this.onBack,
  });

  final LumenRepository repository;
  final String path;
  final void Function(String path) onOpenNote;
  final VoidCallback onBack;

  @override
  State<NoteScreen> createState() => _NoteScreenState();
}

class _NoteScreenState extends State<NoteScreen> {
  late Future<Note> _note = widget.repository.note(widget.path);
  final _body = TextEditingController();
  bool _editing = false;
  int _revision = 0;
  String? _error;

  @override
  void didUpdateWidget(NoteScreen old) {
    super.didUpdateWidget(old);
    if (old.path != widget.path) {
      _editing = false;
      _error = null;
      _reload();
    }
  }

  @override
  void dispose() {
    _body.dispose();
    super.dispose();
  }

  void _reload() {
    setState(() {
      _note = widget.repository.note(widget.path);
    });
  }

  Future<void> _save() async {
    try {
      await widget.repository.saveNote(widget.path, _body.text, _revision);
      setState(() {
        _editing = false;
        _error = null;
      });
      _reload();
    } on ApiException catch (e) {
      setState(() => _error = e.status == 409
          ? 'Заметка изменилась на диске — обновите страницу и повторите'
          : e.message);
    }
  }

  Future<void> _toggle(Task task, TaskStatus status) async {
    try {
      await widget.repository.patchTask(task.id, status: status);
      _reload();
    } on ApiException catch (e) {
      setState(() => _error = e.message);
    }
  }

  @override
  Widget build(BuildContext context) {
    return FutureBuilder<Note>(
      future: _note,
      builder: (context, snapshot) {
        if (snapshot.connectionState == ConnectionState.waiting) {
          return const Center(child: CircularProgressIndicator());
        }
        if (snapshot.hasError) {
          return ServerUnreachable(
              message: '${snapshot.error}', onRetry: _reload);
        }
        final note = snapshot.data!;
        _revision = note.revision;
        if (!_editing) _body.text = note.body;

        return Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                IconButton(
                  onPressed: widget.onBack,
                  icon: const Icon(Icons.arrow_back),
                  tooltip: 'Назад',
                ),
                Expanded(
                  child: Text(note.title,
                      style: Theme.of(context).textTheme.titleLarge,
                      overflow: TextOverflow.ellipsis),
                ),
                if (_editing) ...[
                  FilledButton(
                      onPressed: _save, child: const Text('Сохранить')),
                  const SizedBox(width: 6),
                  TextButton(
                    onPressed: () => setState(() {
                      _editing = false;
                      _body.text = note.body;
                    }),
                    child: const Text('Отмена'),
                  ),
                ] else
                  TextButton.icon(
                    onPressed: () => setState(() => _editing = true),
                    icon: const Icon(Icons.edit, size: 16),
                    label: const Text('Редактировать'),
                  ),
              ],
            ),
            Text(note.path, style: Theme.of(context).textTheme.labelSmall),
            if (_error != null) ErrorBanner(_error!),
            Expanded(
              child: ListView(
                children: [
                  if (note.tasks.isNotEmpty && !_editing) ...[
                    SectionHead('Задачи ${note.tasks.length}'),
                    for (final t in note.tasks)
                      TaskTile(task: t, onToggle: (s) => _toggle(t, s)),
                  ],
                  const SectionHead('Содержимое'),
                  if (_editing)
                    TextField(
                      controller: _body,
                      maxLines: null,
                      minLines: 18,
                      style: const TextStyle(
                          fontFamily: 'monospace', fontSize: 12.5),
                      decoration:
                          const InputDecoration(border: OutlineInputBorder()),
                    )
                  else
                    SelectableText(
                      note.body,
                      style: const TextStyle(
                          fontFamily: 'monospace', fontSize: 12.5, height: 1.6),
                    ),
                  const SectionHead('Свойства'),
                  _prop(context, 'тип', note.type),
                  _prop(
                      context,
                      'теги',
                      note.tags.isEmpty
                          ? '—'
                          : note.tags.map((t) => '#$t').join(' ')),
                  _prop(context, 'ревизия', '${note.revision}'),
                  _Backlinks(
                    repository: widget.repository,
                    path: widget.path,
                    onOpenNote: widget.onOpenNote,
                  ),
                ],
              ),
            ),
          ],
        );
      },
    );
  }

  Widget _prop(BuildContext context, String key, String value) => Padding(
        padding: const EdgeInsets.symmetric(vertical: 2),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            SizedBox(
              width: 90,
              child: Text(key, style: Theme.of(context).textTheme.labelSmall),
            ),
            Expanded(child: Text(value, style: const TextStyle(fontSize: 12))),
          ],
        ),
      );
}

class _Backlinks extends StatelessWidget {
  const _Backlinks(
      {required this.repository, required this.path, required this.onOpenNote});

  final LumenRepository repository;
  final String path;
  final void Function(String path) onOpenNote;

  @override
  Widget build(BuildContext context) {
    return FutureBuilder<List<NoteSummary>>(
      future: repository.backlinks(path),
      builder: (context, snapshot) {
        final links = snapshot.data ?? const <NoteSummary>[];
        if (links.isEmpty) return const SizedBox.shrink();
        return Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            SectionHead('Обратные ссылки ${links.length}'),
            for (final b in links)
              ListTile(
                dense: true,
                contentPadding: EdgeInsets.zero,
                leading: const Icon(Icons.subdirectory_arrow_left, size: 16),
                title: Text(b.title),
                subtitle: Text(b.path, style: const TextStyle(fontSize: 11)),
                onTap: () => onOpenNote(b.path),
              ),
          ],
        );
      },
    );
  }
}
