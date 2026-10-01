import 'package:flutter/material.dart';

import '../data/lumen_repository.dart';
import '../models.dart';
import 'tasks_screen.dart';

/// Список заметок с фильтром по тегу — аналог /notes в вебе.
class NotesScreen extends StatefulWidget {
  const NotesScreen(
      {super.key,
      required this.repository,
      required this.onOpenNote,
      this.tag});

  final LumenRepository repository;
  final void Function(String path) onOpenNote;
  final String? tag;

  @override
  State<NotesScreen> createState() => _NotesScreenState();
}

class _NotesScreenState extends State<NotesScreen> {
  late Future<List<NoteSummary>> _notes =
      widget.repository.notes(tag: widget.tag);

  @override
  void didUpdateWidget(NotesScreen old) {
    super.didUpdateWidget(old);
    if (old.tag != widget.tag) _reload();
  }

  void _reload() {
    setState(() {
      _notes = widget.repository.notes(tag: widget.tag);
    });
  }

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            Text(
              widget.tag == null ? 'Все заметки' : '#${widget.tag}',
              style: Theme.of(context).textTheme.titleLarge,
            ),
            const Spacer(),
            IconButton(onPressed: _reload, icon: const Icon(Icons.refresh)),
          ],
        ),
        Expanded(
          child: FutureBuilder<List<NoteSummary>>(
            future: _notes,
            builder: (context, snapshot) {
              if (snapshot.connectionState == ConnectionState.waiting) {
                return const Center(child: CircularProgressIndicator());
              }
              if (snapshot.hasError) {
                return ServerUnreachable(
                    message: '${snapshot.error}', onRetry: _reload);
              }
              final notes = snapshot.data ?? const <NoteSummary>[];
              if (notes.isEmpty) {
                return const Center(child: Text('Заметок нет'));
              }

              return ListView.builder(
                itemCount: notes.length,
                itemBuilder: (context, i) {
                  final n = notes[i];
                  return ListTile(
                    dense: true,
                    leading: const Icon(Icons.description_outlined, size: 18),
                    title: Text(n.title),
                    subtitle:
                        Text(n.path, style: const TextStyle(fontSize: 11.5)),
                    trailing: Wrap(
                      spacing: 6,
                      children: [
                        if (n.openTasks > 0)
                          Chip(label: Text('${n.openTasks} задач')),
                        for (final t in n.tags.take(3))
                          Chip(label: Text('#$t')),
                      ],
                    ),
                    onTap: () => widget.onOpenNote(n.path),
                  );
                },
              );
            },
          ),
        ),
      ],
    );
  }
}
