import 'dart:convert';

import 'package:http/http.dart' as http;

import '../models.dart';

/// Источник данных для UI.
///
/// Сейчас есть одна реализация — [HttpRepository], которая ходит в `lumend`.
/// На M2 появится `NativeRepository` поверх `lumen-core` через
/// flutter_rust_bridge: она даст офлайн-режим и мгновенный отклик, а экраны
/// останутся прежними, потому что зависят только от этого интерфейса.
abstract class LumenRepository {
  Future<Identity> me();

  Future<List<Task>> tasks({String view = 'today'});

  Future<Task> patchTask(String id, {TaskStatus? status, String? due});

  Future<Task> createTask(String text, {String? note, String? due});

  Future<List<NoteSummary>> notes({String? type, String? tag});

  Future<Note> note(String pathOrId);

  Future<Note> saveNote(String pathOrId, String body, int revision);

  Future<List<NoteSummary>> backlinks(String pathOrId);

  Future<List<CalendarEvent>> calendar(DateTime from, DateTime to);

  Future<Task> moveEvent(String ref, DateTime date);

  Future<List<SearchHit>> search(String query);

  Future<List<TagCount>> tags();

  Future<List<Integration>> integrations();

  Future<List<IntegrationKind>> integrationKinds();

  Future<void> addIntegration(
      String kind, String name, String interval, Map<String, String> values);

  Future<void> setIntegrationEnabled(String id, bool enabled);

  Future<void> deleteIntegration(String id);

  Future<String> syncIntegration(String id);

  Future<List<PasswordStore>> passwordStores();

  Future<List<PasswordKind>> passwordKinds();

  Future<void> addPasswordStore(
      String kind, String name, Map<String, String> values);

  Future<void> deletePasswordStore(String id);

  Future<void> unlockPasswordStore(String id, String passphrase);

  Future<void> lockPasswordStore(String id);

  Future<void> lockAllPasswordStores();

  Future<List<PasswordItem>> passwordItems(String id, {String query = ''});

  /// Единственный путь, которым значение секрета попадает в приложение.
  Future<String> revealPassword(String storeId, String itemId, String field);
}

class ApiException implements Exception {
  ApiException(this.status, this.message);

  final int status;
  final String message;

  @override
  String toString() => message;
}

class HttpRepository implements LumenRepository {
  HttpRepository({Uri? baseUrl, this.token, http.Client? client})
      : baseUrl = baseUrl ?? Uri.parse('http://127.0.0.1:8787'),
        _client = client ?? http.Client();

  final Uri baseUrl;
  final String? token;
  final http.Client _client;

  Map<String, String> get _headers => {
        'Content-Type': 'application/json',
        if (token != null) 'Authorization': 'Bearer $token',
      };

  Uri _uri(String path, [Map<String, String>? query]) =>
      baseUrl.replace(path: '/api/v1$path', queryParameters: query);

  Future<dynamic> _send(http.Request request) async {
    final streamed = await _client.send(request);
    final body = await streamed.stream.bytesToString();
    if (streamed.statusCode >= 400) {
      final decoded = body.isEmpty ? null : jsonDecode(body);
      final message = decoded is Map && decoded['error'] is Map
          ? decoded['error']['message'] as String
          : streamed.reasonPhrase ?? 'неизвестная ошибка';
      throw ApiException(streamed.statusCode, message);
    }
    return body.isEmpty ? null : jsonDecode(body);
  }

  Future<dynamic> _get(Uri uri) =>
      _send(http.Request('GET', uri)..headers.addAll(_headers));

  Future<dynamic> _json(String method, Uri uri,
          [Map<String, dynamic> payload = const {}]) =>
      _send(http.Request(method, uri)
        ..headers.addAll(_headers)
        ..body = jsonEncode(payload));

  List<T> _items<T>(dynamic data, T Function(Map<String, dynamic>) parse) =>
      ((data['items'] as List?) ?? const [])
          .map((e) => parse(e as Map<String, dynamic>))
          .toList();

  static String _date(DateTime d) => '${d.year.toString().padLeft(4, '0')}-'
      '${d.month.toString().padLeft(2, '0')}-'
      '${d.day.toString().padLeft(2, '0')}';

  static String _encodePath(String p) =>
      p.split('/').map(Uri.encodeComponent).join('/');

  @override
  Future<Identity> me() async =>
      Identity.fromJson(await _get(_uri('/me')) as Map<String, dynamic>);

  @override
  Future<List<Task>> tasks({String view = 'today'}) async => _items(
      await _get(_uri('/tasks', {'view': view, 'limit': '500'})),
      Task.fromJson);

  @override
  Future<Task> patchTask(String id, {TaskStatus? status, String? due}) async {
    final data = await _json('PATCH', _uri('/tasks/$id'), {
      if (status != null) 'status': taskStatusName(status),
      if (due != null) 'due': due,
    });
    return Task.fromJson(data as Map<String, dynamic>);
  }

  @override
  Future<Task> createTask(String text, {String? note, String? due}) async {
    final data = await _json('POST', _uri('/tasks'), {
      'text': text,
      if (note != null) 'note': note,
      if (due != null) 'due': due,
    });
    return Task.fromJson(data as Map<String, dynamic>);
  }

  @override
  Future<List<NoteSummary>> notes({String? type, String? tag}) async => _items(
        await _get(_uri('/notes', {
          if (type != null) 'type': type,
          if (tag != null) 'tag': tag,
        })),
        NoteSummary.fromJson,
      );

  @override
  Future<Note> note(String pathOrId) async => Note.fromJson(
        await _get(
                baseUrl.replace(path: '/api/v1/notes/${_encodePath(pathOrId)}'))
            as Map<String, dynamic>,
      );

  @override
  Future<Note> saveNote(String pathOrId, String body, int revision) async =>
      Note.fromJson(
        await _json(
            'PUT',
            baseUrl.replace(path: '/api/v1/notes/${_encodePath(pathOrId)}'),
            {'body': body, 'revision': revision}) as Map<String, dynamic>,
      );

  @override
  Future<List<NoteSummary>> backlinks(String pathOrId) async => _items(
        await _get(baseUrl.replace(
            path: '/api/v1/backlinks/${_encodePath(pathOrId)}')),
        NoteSummary.fromJson,
      );

  @override
  Future<List<CalendarEvent>> calendar(DateTime from, DateTime to) async {
    final data =
        await _get(_uri('/calendar', {'from': _date(from), 'to': _date(to)}));
    return ((data['events'] as List?) ?? const [])
        .map((e) => CalendarEvent.fromJson(e as Map<String, dynamic>))
        .toList();
  }

  @override
  Future<Task> moveEvent(String ref, DateTime date) async => Task.fromJson(
        await _json('POST', _uri('/calendar/move'), {
          'ref': ref,
          'date': _date(date),
          'field': 'scheduled'
        }) as Map<String, dynamic>,
      );

  @override
  Future<List<SearchHit>> search(String query) async =>
      _items(await _get(_uri('/search', {'q': query})), SearchHit.fromJson);

  @override
  Future<List<TagCount>> tags() async =>
      _items(await _get(_uri('/tags')), TagCount.fromJson);

  @override
  Future<List<Integration>> integrations() async =>
      _items(await _get(_uri('/integrations')), Integration.fromJson);

  @override
  Future<List<IntegrationKind>> integrationKinds() async =>
      _items(await _get(_uri('/integrations/kinds')), IntegrationKind.fromJson);

  @override
  Future<void> addIntegration(String kind, String name, String interval,
      Map<String, String> values) async {
    await _json('POST', _uri('/integrations'),
        {'kind': kind, 'name': name, 'interval': interval, 'values': values});
  }

  @override
  Future<void> setIntegrationEnabled(String id, bool enabled) async {
    await _json('PATCH', _uri('/integrations/$id'), {'enabled': enabled});
  }

  @override
  Future<void> deleteIntegration(String id) async {
    await _json('DELETE', _uri('/integrations/$id'));
  }

  @override
  Future<String> syncIntegration(String id) async {
    final data = await _json('POST', _uri('/integrations/$id/sync'))
        as Map<String, dynamic>;
    final error = data['error'] as String?;
    if (error != null && error.isNotEmpty) return error;
    return 'создано ${data['created']} · обновлено ${data['updated']} · событий ${data['events']}';
  }

  @override
  Future<List<PasswordStore>> passwordStores() async =>
      _items(await _get(_uri('/passwords/stores')), PasswordStore.fromJson);

  @override
  Future<List<PasswordKind>> passwordKinds() async =>
      _items(await _get(_uri('/passwords/kinds')), PasswordKind.fromJson);

  @override
  Future<void> addPasswordStore(
      String kind, String name, Map<String, String> values) async {
    await _json('POST', _uri('/passwords/stores'),
        {'kind': kind, 'name': name, 'values': values});
  }

  @override
  Future<void> deletePasswordStore(String id) async {
    await _json('DELETE', _uri('/passwords/stores/$id'));
  }

  @override
  Future<void> unlockPasswordStore(String id, String passphrase) async {
    await _json('POST', _uri('/passwords/stores/$id/unlock'),
        {'passphrase': passphrase});
  }

  @override
  Future<void> lockPasswordStore(String id) async {
    await _json('POST', _uri('/passwords/stores/$id/lock'));
  }

  @override
  Future<void> lockAllPasswordStores() async {
    await _json('POST', _uri('/passwords/lock'));
  }

  @override
  Future<List<PasswordItem>> passwordItems(String id,
          {String query = ''}) async =>
      _items(
        await _get(_uri(
            '/passwords/stores/$id/items', {if (query.isNotEmpty) 'q': query})),
        PasswordItem.fromJson,
      );

  @override
  Future<String> revealPassword(
      String storeId, String itemId, String field) async {
    final data = await _json('POST', _uri('/passwords/stores/$storeId/reveal'),
        {'item': itemId, 'field': field}) as Map<String, dynamic>;
    return data['value'] as String? ?? '';
  }
}
