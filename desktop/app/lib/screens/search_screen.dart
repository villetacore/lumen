import 'package:flutter/material.dart';

import '../data/lumen_repository.dart';
import '../models.dart';

/// Полнотекстовый поиск по vault — аналог /search в вебе.
class SearchScreen extends StatefulWidget {
  const SearchScreen(
      {super.key, required this.repository, required this.onOpenNote});

  final LumenRepository repository;
  final void Function(String path) onOpenNote;

  @override
  State<SearchScreen> createState() => _SearchScreenState();
}

class _SearchScreenState extends State<SearchScreen> {
  final _controller = TextEditingController();
  final _focus = FocusNode();
  Future<List<SearchHit>>? _hits;
  String _query = '';

  @override
  void initState() {
    super.initState();
    _focus.requestFocus();
  }

  @override
  void dispose() {
    _controller.dispose();
    _focus.dispose();
    super.dispose();
  }

  void _run(String q) {
    setState(() {
      _query = q.trim();
      _hits = _query.isEmpty ? null : widget.repository.search(_query);
    });
  }

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text('Поиск', style: Theme.of(context).textTheme.titleLarge),
        const SizedBox(height: 8),
        TextField(
          controller: _controller,
          focusNode: _focus,
          onSubmitted: _run,
          onChanged: (v) {
            // Ищем по мере набора, но только когда есть за что зацепиться.
            if (v.trim().length >= 3) _run(v);
          },
          decoration: const InputDecoration(
            isDense: true,
            border: OutlineInputBorder(),
            prefixIcon: Icon(Icons.search, size: 18),
            hintText: 'Поиск по заметкам и задачам',
          ),
        ),
        const SizedBox(height: 8),
        Expanded(
          child: FutureBuilder<List<SearchHit>>(
            future: _hits,
            builder: (context, snapshot) {
              if (_hits == null) {
                return const Center(child: Text('Введите запрос'));
              }
              if (snapshot.connectionState == ConnectionState.waiting) {
                return const Center(child: CircularProgressIndicator());
              }
              if (snapshot.hasError) {
                return Center(child: Text('${snapshot.error}'));
              }
              final hits = snapshot.data ?? const <SearchHit>[];
              if (hits.isEmpty) {
                return Center(
                    child: Text('По запросу «$_query» ничего не найдено'));
              }
              return ListView.separated(
                itemCount: hits.length,
                separatorBuilder: (_, __) => const Divider(height: 1),
                itemBuilder: (context, i) {
                  final h = hits[i];
                  return ListTile(
                    dense: true,
                    title: Text(h.title),
                    subtitle: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(h.snippet,
                            style: const TextStyle(
                                fontFamily: 'monospace', fontSize: 11.5)),
                        Text(h.path, style: const TextStyle(fontSize: 11)),
                      ],
                    ),
                    onTap: () => widget.onOpenNote(h.path),
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
