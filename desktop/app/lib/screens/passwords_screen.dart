import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:intl/intl.dart';

import '../data/lumen_repository.dart';
import '../models.dart';
import '../widgets/config_form.dart';
import 'tasks_screen.dart';

/// Чтение записей из Bitwarden/Vaultwarden и KeePass.
///
/// Раскрытые значения живут только в состоянии виджета: при закрытии записи,
/// хранилища или экрана они исчезают. В файлы и в vault не попадают никогда.
class PasswordsScreen extends StatefulWidget {
  const PasswordsScreen({super.key, required this.repository});

  final LumenRepository repository;

  @override
  State<PasswordsScreen> createState() => _PasswordsScreenState();
}

class _PasswordsScreenState extends State<PasswordsScreen> {
  late Future<List<PasswordStore>> _stores = widget.repository.passwordStores();
  bool _adding = false;

  void _reload() {
    setState(() {
      _stores = widget.repository.passwordStores();
    });
  }

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            Text('Пароли', style: Theme.of(context).textTheme.titleLarge),
            const Spacer(),
            TextButton(
              onPressed: () async {
                await widget.repository.lockAllPasswordStores();
                _reload();
              },
              child: const Text('Закрыть все'),
            ),
            const SizedBox(width: 6),
            FilledButton.icon(
              onPressed: () => setState(() => _adding = !_adding),
              icon: const Icon(Icons.add, size: 16),
              label: const Text('Подключить'),
            ),
          ],
        ),
        const SizedBox(height: 4),
        Text(
          'Мастер-пароль нужен один раз, чтобы получить сессию: она живёт только в '
          'памяти сервера и закрывается сама при простое. В заметки и поиск пароли не попадают.',
          style: Theme.of(context).textTheme.labelSmall,
        ),
        if (_adding)
          _AddStore(
            repository: widget.repository,
            onDone: () {
              setState(() => _adding = false);
              _reload();
            },
          ),
        Expanded(
          child: FutureBuilder<List<PasswordStore>>(
            future: _stores,
            builder: (context, snapshot) {
              if (snapshot.connectionState == ConnectionState.waiting) {
                return const Center(child: CircularProgressIndicator());
              }
              if (snapshot.hasError) {
                return ServerUnreachable(
                    message: '${snapshot.error}', onRetry: _reload);
              }
              final stores = snapshot.data ?? const <PasswordStore>[];
              if (stores.isEmpty && !_adding) {
                return const Center(child: Text('Хранилища не подключены'));
              }
              return ListView(
                children: [
                  for (final s in stores)
                    _StorePanel(
                      repository: widget.repository,
                      store: s,
                      onChanged: _reload,
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

class _StorePanel extends StatefulWidget {
  const _StorePanel(
      {required this.repository, required this.store, required this.onChanged});

  final LumenRepository repository;
  final PasswordStore store;
  final VoidCallback onChanged;

  @override
  State<_StorePanel> createState() => _StorePanelState();
}

class _StorePanelState extends State<_StorePanel> {
  final _passphrase = TextEditingController();
  final _query = TextEditingController();
  Future<List<PasswordItem>>? _items;
  String? _error;

  @override
  void initState() {
    super.initState();
    if (widget.store.unlocked) _search();
  }

  @override
  void didUpdateWidget(_StorePanel old) {
    super.didUpdateWidget(old);
    if (old.store.unlocked != widget.store.unlocked) {
      setState(() => _items = widget.store.unlocked
          ? widget.repository.passwordItems(widget.store.id, query: _query.text)
          : null);
    }
  }

  @override
  void dispose() {
    _passphrase.dispose();
    _query.dispose();
    super.dispose();
  }

  void _search() {
    setState(() {
      _items =
          widget.repository.passwordItems(widget.store.id, query: _query.text);
    });
  }

  Future<void> _unlock() async {
    try {
      await widget.repository
          .unlockPasswordStore(widget.store.id, _passphrase.text);
      _passphrase.clear(); // мастер-пароль не задерживается в памяти клиента
      setState(() => _error = null);
      widget.onChanged();
    } on ApiException catch (e) {
      setState(() => _error = e.message);
    }
  }

  @override
  Widget build(BuildContext context) {
    final s = widget.store;
    return Card(
      margin: const EdgeInsets.symmetric(vertical: 6),
      child: Padding(
        padding: const EdgeInsets.all(10),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(s.unlocked ? Icons.lock_open : Icons.lock_outline,
                    size: 18),
                const SizedBox(width: 8),
                Text(s.name,
                    style: const TextStyle(fontWeight: FontWeight.w600)),
                const SizedBox(width: 8),
                Chip(label: Text(s.title)),
                if (s.locksAt != null) ...[
                  const SizedBox(width: 6),
                  Chip(label: Text('до ${DateFormat.Hm().format(s.locksAt!)}')),
                ],
                const Spacer(),
                if (s.unlocked)
                  TextButton(
                    onPressed: () async {
                      await widget.repository.lockPasswordStore(s.id);
                      widget.onChanged();
                    },
                    child: const Text('Закрыть'),
                  ),
                IconButton(
                  tooltip: 'Убрать из Lumen',
                  icon: const Icon(Icons.delete_outline, size: 18),
                  onPressed: () async {
                    await widget.repository.deletePasswordStore(s.id);
                    widget.onChanged();
                  },
                ),
              ],
            ),
            if (s.lastError != null && s.lastError!.isNotEmpty)
              ErrorBanner(s.lastError!),
            if (_error != null) ErrorBanner(_error!),
            if (!s.unlocked)
              Row(
                children: [
                  if (s.needsPassphrase)
                    Expanded(
                      child: TextField(
                        controller: _passphrase,
                        obscureText: true,
                        autocorrect: false,
                        enableSuggestions: false,
                        onSubmitted: (_) => _unlock(),
                        decoration: const InputDecoration(
                          isDense: true,
                          border: OutlineInputBorder(),
                          labelText: 'Мастер-пароль',
                        ),
                      ),
                    )
                  else
                    const Expanded(
                      child: Text(
                          'Пароль не нужен: используется сессия из настроек'),
                    ),
                  const SizedBox(width: 8),
                  FilledButton(
                      onPressed: _unlock, child: const Text('Открыть')),
                ],
              )
            else ...[
              TextField(
                controller: _query,
                onChanged: (v) {
                  if (v.isEmpty || v.length >= 2) _search();
                },
                decoration: const InputDecoration(
                  isDense: true,
                  border: OutlineInputBorder(),
                  prefixIcon: Icon(Icons.search, size: 18),
                  hintText: 'Поиск по записям',
                ),
              ),
              const SizedBox(height: 6),
              FutureBuilder<List<PasswordItem>>(
                future: _items,
                builder: (context, snapshot) {
                  if (snapshot.connectionState == ConnectionState.waiting) {
                    return const Padding(
                      padding: EdgeInsets.all(12),
                      child: Center(child: CircularProgressIndicator()),
                    );
                  }
                  final items = snapshot.data ?? const <PasswordItem>[];
                  if (items.isEmpty) {
                    return const Padding(
                      padding: EdgeInsets.all(12),
                      child: Text('Ничего не найдено'),
                    );
                  }
                  return Column(
                    children: [
                      for (final it in items)
                        _ItemTile(
                            repository: widget.repository,
                            storeId: s.id,
                            item: it),
                    ],
                  );
                },
              ),
            ],
          ],
        ),
      ),
    );
  }
}

const _fieldLabels = {
  'password': 'Пароль',
  'username': 'Логин',
  'totp': 'Код 2FA',
  'notes': 'Заметка',
  'uri': 'Адрес',
};

class _ItemTile extends StatefulWidget {
  const _ItemTile(
      {required this.repository, required this.storeId, required this.item});

  final LumenRepository repository;
  final String storeId;
  final PasswordItem item;

  @override
  State<_ItemTile> createState() => _ItemTileState();
}

class _ItemTileState extends State<_ItemTile> {
  final _revealed = <String, String>{};
  String? _error;

  Future<String> _value(String field) async {
    final cached = _revealed[field];
    if (cached != null) return cached;
    final v = await widget.repository
        .revealPassword(widget.storeId, widget.item.id, field);
    return v;
  }

  Future<void> _reveal(String field) async {
    try {
      final v = await _value(field);
      setState(() {
        _revealed[field] = v;
        _error = null;
      });
    } on ApiException catch (e) {
      setState(() => _error = e.message);
    }
  }

  Future<void> _copy(String field) async {
    try {
      final v = await _value(field);
      await Clipboard.setData(ClipboardData(text: v));
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(
            content: Text('Скопировано; буфер очистится через 30 секунд')),
      );
      // Пароль не должен лежать в буфере до конца дня.
      Future.delayed(const Duration(seconds: 30), () {
        Clipboard.setData(const ClipboardData(text: ''));
      });
    } on ApiException catch (e) {
      setState(() => _error = e.message);
    }
  }

  @override
  Widget build(BuildContext context) {
    final it = widget.item;
    return ExpansionTile(
      dense: true,
      tilePadding: EdgeInsets.zero,
      // Закрыли запись — забыли значения.
      onExpansionChanged: (open) {
        if (!open) setState(_revealed.clear);
      },
      title: Row(
        children: [
          Flexible(child: Text(it.name, overflow: TextOverflow.ellipsis)),
          if (it.username != null && it.username!.isNotEmpty) ...[
            const SizedBox(width: 8),
            Text(it.username!, style: Theme.of(context).textTheme.labelSmall),
          ],
          if (it.folder != null && it.folder!.isNotEmpty) ...[
            const SizedBox(width: 8),
            Chip(label: Text(it.folder!)),
          ],
        ],
      ),
      children: [
        if (_error != null) ErrorBanner(_error!),
        for (final field in it.fields)
          Padding(
            padding: const EdgeInsets.symmetric(vertical: 2),
            child: Row(
              children: [
                SizedBox(
                  width: 80,
                  child: Text(_fieldLabels[field] ?? field,
                      style: Theme.of(context).textTheme.labelSmall),
                ),
                Expanded(
                  child: SelectableText(
                    _revealed[field] ?? '••••••••••••',
                    style: const TextStyle(
                        fontFamily: 'monospace', fontSize: 12.5),
                  ),
                ),
                TextButton(
                  onPressed: () => _revealed.containsKey(field)
                      ? setState(() => _revealed.remove(field))
                      : _reveal(field),
                  child: Text(
                      _revealed.containsKey(field) ? 'Скрыть' : 'Показать'),
                ),
                IconButton(
                  tooltip: 'Скопировать',
                  icon: const Icon(Icons.copy, size: 16),
                  onPressed: () => _copy(field),
                ),
              ],
            ),
          ),
      ],
    );
  }
}

class _AddStore extends StatefulWidget {
  const _AddStore({required this.repository, required this.onDone});

  final LumenRepository repository;
  final VoidCallback onDone;

  @override
  State<_AddStore> createState() => _AddStoreState();
}

class _AddStoreState extends State<_AddStore> {
  late final Future<List<PasswordKind>> _kinds =
      widget.repository.passwordKinds();
  String? _kind;
  String _name = '';
  final _values = <String, String>{};
  String? _error;

  @override
  Widget build(BuildContext context) {
    return FutureBuilder<List<PasswordKind>>(
      future: _kinds,
      builder: (context, snapshot) {
        final kinds = snapshot.data ?? const <PasswordKind>[];
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
                const SectionHead('Подключить хранилище'),
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
                const SizedBox(height: 4),
                ConfigForm(
                    fields: selected.fields,
                    values: _values,
                    onChanged: (k, v) => _values[k] = v),
                if (_error != null) ErrorBanner(_error!),
                const SizedBox(height: 8),
                Row(
                  children: [
                    FilledButton(
                      onPressed: () async {
                        try {
                          await widget.repository.addPasswordStore(
                            selected.kind,
                            _name.isEmpty ? selected.title : _name,
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
