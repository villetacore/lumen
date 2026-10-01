import 'package:flutter/material.dart';
import 'package:intl/intl.dart';

import '../data/lumen_repository.dart';
import '../models.dart';
import '../widgets/config_form.dart';
import 'tasks_screen.dart';

/// Подключение календарей и трекеров — аналог /integrations в вебе.
class IntegrationsScreen extends StatefulWidget {
  const IntegrationsScreen({super.key, required this.repository});

  final LumenRepository repository;

  @override
  State<IntegrationsScreen> createState() => _IntegrationsScreenState();
}

class _IntegrationsScreenState extends State<IntegrationsScreen> {
  late Future<List<Integration>> _list = widget.repository.integrations();
  bool _adding = false;
  final _results = <String, String>{};

  void _reload() {
    setState(() {
      _list = widget.repository.integrations();
    });
  }

  Future<void> _sync(Integration item) async {
    setState(() => _results[item.id] = 'синхронизация…');
    try {
      final summary = await widget.repository.syncIntegration(item.id);
      setState(() => _results[item.id] = summary);
      _reload();
    } on ApiException catch (e) {
      setState(() => _results[item.id] = e.message);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            Text('Интеграции', style: Theme.of(context).textTheme.titleLarge),
            const Spacer(),
            FilledButton.icon(
              onPressed: () => setState(() => _adding = !_adding),
              icon: const Icon(Icons.add, size: 16),
              label: const Text('Подключить'),
            ),
          ],
        ),
        const SizedBox(height: 4),
        Text(
          'Токены не хранятся в настройках: в полях-секретах указывается ссылка '
          'bitwarden:запись, keepass:база.kdbx#Запись, env:ПЕРЕМЕННАЯ или command:…',
          style: Theme.of(context).textTheme.labelSmall,
        ),
        if (_adding)
          _AddIntegration(
            repository: widget.repository,
            onDone: () {
              setState(() => _adding = false);
              _reload();
            },
          ),
        Expanded(
          child: FutureBuilder<List<Integration>>(
            future: _list,
            builder: (context, snapshot) {
              if (snapshot.connectionState == ConnectionState.waiting) {
                return const Center(child: CircularProgressIndicator());
              }
              if (snapshot.hasError) {
                return ServerUnreachable(
                    message: '${snapshot.error}', onRetry: _reload);
              }
              final items = snapshot.data ?? const <Integration>[];
              if (items.isEmpty && !_adding) {
                return const Center(child: Text('Ничего не подключено'));
              }
              return ListView(
                children: [
                  for (final item in items)
                    Card(
                      margin: const EdgeInsets.symmetric(vertical: 4),
                      child: Padding(
                        padding: const EdgeInsets.all(10),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Row(
                              children: [
                                Switch(
                                  value: item.enabled,
                                  onChanged: (v) async {
                                    await widget.repository
                                        .setIntegrationEnabled(item.id, v);
                                    _reload();
                                  },
                                ),
                                const SizedBox(width: 8),
                                Text(item.name,
                                    style: const TextStyle(
                                        fontWeight: FontWeight.w600)),
                                const SizedBox(width: 8),
                                Chip(label: Text(item.title)),
                                const SizedBox(width: 6),
                                Chip(label: Text('каждые ${item.interval}')),
                                const Spacer(),
                                TextButton(
                                    onPressed: () => _sync(item),
                                    child: const Text('Обновить')),
                                IconButton(
                                  tooltip: 'Удалить',
                                  icon: const Icon(Icons.delete_outline,
                                      size: 18),
                                  onPressed: () async {
                                    await widget.repository
                                        .deleteIntegration(item.id);
                                    _reload();
                                  },
                                ),
                              ],
                            ),
                            Text(
                              'последняя синхронизация: '
                              '${item.lastSync == null ? "—" : DateFormat.Hm().format(item.lastSync!)}'
                              ' · элементов: ${item.lastItems}',
                              style: Theme.of(context).textTheme.labelSmall,
                            ),
                            if (item.lastError != null &&
                                item.lastError!.isNotEmpty)
                              ErrorBanner(item.lastError!),
                            if (_results[item.id] != null)
                              Padding(
                                padding: const EdgeInsets.only(top: 4),
                                child: Text(_results[item.id]!,
                                    style:
                                        Theme.of(context).textTheme.labelSmall),
                              ),
                          ],
                        ),
                      ),
                    ),
                ],
              );
            },
          ),
        ),
      ],
    );
  }
}

class _AddIntegration extends StatefulWidget {
  const _AddIntegration({required this.repository, required this.onDone});

  final LumenRepository repository;
  final VoidCallback onDone;

  @override
  State<_AddIntegration> createState() => _AddIntegrationState();
}

class _AddIntegrationState extends State<_AddIntegration> {
  late final Future<List<IntegrationKind>> _kinds =
      widget.repository.integrationKinds();
  String? _kind;
  String _name = '';
  String _interval = '15m';
  final _values = <String, String>{};
  String? _error;

  @override
  Widget build(BuildContext context) {
    return FutureBuilder<List<IntegrationKind>>(
      future: _kinds,
      builder: (context, snapshot) {
        final kinds = snapshot.data ?? const <IntegrationKind>[];
        if (kinds.isEmpty) return const SizedBox.shrink();
        final selected =
            kinds.firstWhere((k) => k.kind == _kind, orElse: () => kinds.first);
        _kind ??= selected.kind;

        return Card(
          margin: const EdgeInsets.symmetric(vertical: 8),
          child: Padding(
            padding: const EdgeInsets.all(12),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const SectionHead('Новая интеграция'),
                DropdownButtonFormField<String>(
                  initialValue: selected.kind,
                  decoration: const InputDecoration(
                      isDense: true, border: OutlineInputBorder()),
                  items: [
                    for (final k in kinds)
                      DropdownMenuItem(value: k.kind, child: Text(k.title)),
                  ],
                  onChanged: (v) => setState(() {
                    _kind = v;
                    _values.clear();
                  }),
                ),
                const SizedBox(height: 8),
                TextFormField(
                  decoration: const InputDecoration(
                      isDense: true,
                      border: OutlineInputBorder(),
                      labelText: 'Название'),
                  onChanged: (v) => _name = v,
                ),
                const SizedBox(height: 8),
                TextFormField(
                  initialValue: _interval,
                  decoration: const InputDecoration(
                      isDense: true,
                      border: OutlineInputBorder(),
                      labelText: 'Как часто обновлять'),
                  onChanged: (v) => _interval = v,
                ),
                const SizedBox(height: 4),
                ConfigForm(
                  fields: selected.fields,
                  values: _values,
                  onChanged: (k, v) => _values[k] = v,
                ),
                if (_error != null) ErrorBanner(_error!),
                const SizedBox(height: 8),
                Row(
                  children: [
                    FilledButton(
                      onPressed: () async {
                        try {
                          await widget.repository.addIntegration(
                            selected.kind,
                            _name.isEmpty ? selected.title : _name,
                            _interval,
                            _values,
                          );
                          widget.onDone();
                        } on ApiException catch (e) {
                          setState(() => _error = e.message);
                        }
                      },
                      child: const Text('Подключить'),
                    ),
                    const SizedBox(width: 8),
                    TextButton(
                        onPressed: widget.onDone, child: const Text('Отмена')),
                  ],
                ),
              ],
            ),
          ),
        );
      },
    );
  }
}
