/// Модели, зеркалящие DTO сервера (`server/internal/domain`) и типы
/// `lumen-core`. Пока данные приходят по HTTP; на M2 те же типы будут
/// заполняться напрямую из Rust через flutter_rust_bridge.
library;

enum TaskStatus { todo, doing, done, cancelled, deferred }

TaskStatus taskStatusFrom(String? value) => switch (value) {
      'doing' => TaskStatus.doing,
      'done' => TaskStatus.done,
      'cancelled' => TaskStatus.cancelled,
      'deferred' => TaskStatus.deferred,
      _ => TaskStatus.todo,
    };

String taskStatusName(TaskStatus s) => s.name;

extension TaskStatusX on TaskStatus {
  bool get isOpen => this == TaskStatus.todo || this == TaskStatus.doing;

  /// Клик по чекбоксу перебирает статусы: todo → doing → done → todo.
  TaskStatus get next => switch (this) {
        TaskStatus.todo => TaskStatus.doing,
        TaskStatus.doing => TaskStatus.done,
        _ => TaskStatus.todo,
      };
}

class Task {
  const Task({
    required this.id,
    required this.notePath,
    required this.line,
    required this.status,
    required this.text,
    this.tags = const [],
    this.due,
    this.scheduled,
    this.priority,
    this.estimate,
    this.project,
  });

  final String id;
  final String notePath;
  final int line;
  final TaskStatus status;
  final String text;
  final List<String> tags;
  final DateTime? due;
  final DateTime? scheduled;
  final String? priority;
  final String? estimate;
  final String? project;

  DateTime? get date => due ?? scheduled;

  bool get isOverdue {
    final d = date;
    if (d == null || !status.isOpen) return false;
    final today = DateTime.now();
    return DateTime(d.year, d.month, d.day)
        .isBefore(DateTime(today.year, today.month, today.day));
  }

  static DateTime? _date(Object? v) =>
      v is String && v.isNotEmpty ? DateTime.tryParse(v) : null;

  factory Task.fromJson(Map<String, dynamic> j) => Task(
        id: j['id'] as String,
        notePath: j['note_path'] as String? ?? '',
        line: j['line'] as int? ?? 0,
        status: taskStatusFrom(j['status'] as String?),
        text: j['text'] as String? ?? '',
        tags: (j['tags'] as List?)?.cast<String>() ?? const [],
        due: _date(j['due']),
        scheduled: _date(j['scheduled']),
        priority: j['priority'] as String?,
        estimate: j['estimate'] as String?,
        project: j['project'] as String?,
      );
}

class NoteSummary {
  const NoteSummary({
    required this.id,
    required this.path,
    required this.title,
    required this.type,
    this.tags = const [],
    this.openTasks = 0,
  });

  final String id;
  final String path;
  final String title;
  final String type;
  final List<String> tags;
  final int openTasks;

  factory NoteSummary.fromJson(Map<String, dynamic> j) => NoteSummary(
        id: j['id'] as String? ?? '',
        path: j['path'] as String? ?? '',
        title: j['title'] as String? ?? '',
        type: j['type'] as String? ?? 'note',
        tags: (j['tags'] as List?)?.cast<String>() ?? const [],
        openTasks: j['open_tasks'] as int? ?? 0,
      );
}

class Note extends NoteSummary {
  const Note({
    required super.id,
    required super.path,
    required super.title,
    required super.type,
    super.tags,
    required this.body,
    required this.revision,
    this.tasks = const [],
  });

  final String body;
  final int revision;
  final List<Task> tasks;

  factory Note.fromJson(Map<String, dynamic> j) => Note(
        id: j['id'] as String? ?? '',
        path: j['path'] as String? ?? '',
        title: j['title'] as String? ?? '',
        type: j['type'] as String? ?? 'note',
        tags: (j['tags'] as List?)?.cast<String>() ?? const [],
        body: j['body'] as String? ?? '',
        revision: j['revision'] as int? ?? 0,
        tasks: ((j['tasks'] as List?) ?? const [])
            .map((e) => Task.fromJson(e as Map<String, dynamic>))
            .toList(),
      );
}

class SearchHit {
  const SearchHit(
      {required this.path, required this.title, required this.snippet});

  final String path;
  final String title;
  final String snippet;

  factory SearchHit.fromJson(Map<String, dynamic> j) => SearchHit(
        path: j['path'] as String? ?? '',
        title: j['title'] as String? ?? '',
        snippet: j['snippet'] as String? ?? '',
      );
}

class TagCount {
  const TagCount({required this.tag, required this.count});

  final String tag;
  final int count;

  factory TagCount.fromJson(Map<String, dynamic> j) =>
      TagCount(tag: j['tag'] as String? ?? '', count: j['count'] as int? ?? 0);
}

/// Описание одного поля настроек, приходящее с сервера. Одна и та же форма
/// используется и для интеграций, и для хранилищ паролей.
class ConfigField {
  const ConfigField({
    required this.key,
    required this.label,
    required this.type,
    this.required = false,
    this.placeholder,
    this.help,
  });

  final String key;
  final String label;
  final String type; // text | url | secret | bool | duration
  final bool required;
  final String? placeholder;
  final String? help;

  factory ConfigField.fromJson(Map<String, dynamic> j) => ConfigField(
        key: j['key'] as String? ?? '',
        label: j['label'] as String? ?? '',
        type: j['type'] as String? ?? 'text',
        required: j['required'] as bool? ?? false,
        placeholder: j['placeholder'] as String?,
        help: j['help'] as String?,
      );
}

class Integration {
  const Integration({
    required this.id,
    required this.kind,
    required this.title,
    required this.name,
    required this.enabled,
    required this.interval,
    this.values = const {},
    this.lastSync,
    this.lastError,
    this.lastItems = 0,
    this.running = false,
  });

  final String id;
  final String kind;
  final String title;
  final String name;
  final bool enabled;
  final String interval;
  final Map<String, String> values;
  final DateTime? lastSync;
  final String? lastError;
  final int lastItems;
  final bool running;

  factory Integration.fromJson(Map<String, dynamic> j) => Integration(
        id: j['id'] as String? ?? '',
        kind: j['kind'] as String? ?? '',
        title: j['title'] as String? ?? '',
        name: j['name'] as String? ?? '',
        enabled: j['enabled'] as bool? ?? false,
        interval: j['interval'] as String? ?? '',
        values:
            ((j['values'] as Map?) ?? {}).map((k, v) => MapEntry('$k', '$v')),
        lastSync: j['last_sync'] is String
            ? DateTime.tryParse(j['last_sync'] as String)
            : null,
        lastError: j['last_error'] as String?,
        lastItems: j['last_items'] as int? ?? 0,
        running: j['running'] as bool? ?? false,
      );
}

class IntegrationKind {
  const IntegrationKind(
      {required this.kind, required this.title, required this.fields});

  final String kind;
  final String title;
  final List<ConfigField> fields;

  factory IntegrationKind.fromJson(Map<String, dynamic> j) => IntegrationKind(
        kind: j['kind'] as String? ?? '',
        title: j['title'] as String? ?? '',
        fields: ((j['fields'] as List?) ?? const [])
            .map((e) => ConfigField.fromJson(e as Map<String, dynamic>))
            .toList(),
      );
}

class PasswordStore {
  const PasswordStore({
    required this.id,
    required this.kind,
    required this.title,
    required this.name,
    required this.unlocked,
    required this.needsPassphrase,
    this.locksAt,
    this.lastError,
  });

  final String id;
  final String kind;
  final String title;
  final String name;
  final bool unlocked;
  final bool needsPassphrase;
  final DateTime? locksAt;
  final String? lastError;

  factory PasswordStore.fromJson(Map<String, dynamic> j) => PasswordStore(
        id: j['id'] as String? ?? '',
        kind: j['kind'] as String? ?? '',
        title: j['title'] as String? ?? '',
        name: j['name'] as String? ?? '',
        unlocked: j['unlocked'] as bool? ?? false,
        needsPassphrase: j['needs_passphrase'] as bool? ?? true,
        locksAt: j['locks_at'] is String
            ? DateTime.tryParse(j['locks_at'] as String)
            : null,
        lastError: j['last_error'] as String?,
      );
}

class PasswordItem {
  const PasswordItem({
    required this.id,
    required this.name,
    this.username,
    this.uri,
    this.folder,
    this.fields = const [],
  });

  final String id;
  final String name;
  final String? username;
  final String? uri;
  final String? folder;
  final List<String> fields;

  factory PasswordItem.fromJson(Map<String, dynamic> j) => PasswordItem(
        id: j['id'] as String? ?? '',
        name: j['name'] as String? ?? '',
        username: j['username'] as String?,
        uri: j['uri'] as String?,
        folder: j['folder'] as String?,
        fields: ((j['fields'] as List?) ?? const []).cast<String>(),
      );
}

class PasswordKind {
  const PasswordKind(
      {required this.kind, required this.title, required this.fields});

  final String kind;
  final String title;
  final List<ConfigField> fields;

  factory PasswordKind.fromJson(Map<String, dynamic> j) => PasswordKind(
        kind: j['kind'] as String? ?? '',
        title: j['title'] as String? ?? '',
        fields: ((j['fields'] as List?) ?? const [])
            .map((e) => ConfigField.fromJson(e as Map<String, dynamic>))
            .toList(),
      );
}

class Identity {
  const Identity(
      {required this.authenticated,
      required this.mode,
      this.name,
      this.loginUrl});

  final bool authenticated;
  final String mode; // none | token | oidc
  final String? name;
  final String? loginUrl;

  factory Identity.fromJson(Map<String, dynamic> j) => Identity(
        authenticated: j['authenticated'] as bool? ?? false,
        mode: j['mode'] as String? ?? 'none',
        name: j['name'] as String?,
        loginUrl: j['login_url'] as String?,
      );
}

class CalendarEvent {
  const CalendarEvent({
    required this.id,
    required this.source,
    required this.title,
    required this.start,
    required this.allDay,
    this.path,
    this.status,
  });

  final String id;
  final String source;
  final String title;
  final DateTime start;
  final bool allDay;
  final String? path;
  final TaskStatus? status;

  factory CalendarEvent.fromJson(Map<String, dynamic> j) => CalendarEvent(
        id: j['id'] as String,
        source: j['source'] as String? ?? 'note',
        title: j['title'] as String? ?? '',
        start: DateTime.parse(j['start'] as String),
        allDay: j['all_day'] as bool? ?? true,
        path: j['path'] as String?,
        status:
            j['status'] == null ? null : taskStatusFrom(j['status'] as String?),
      );
}
