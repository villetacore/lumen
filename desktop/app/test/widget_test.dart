// Смоук-тесты экранов. Репозиторий подменён, поэтому тесты не требуют
// запущенного lumend и не ходят в сеть.

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:intl/date_symbol_data_local.dart';
import 'package:lumen_app/app_shell.dart';
import 'package:lumen_app/data/lumen_repository.dart';
import 'package:lumen_app/models.dart';
import 'package:lumen_app/theme.dart';

/// FakeRepository отдаёт заранее заданные данные и запоминает вызовы.
class FakeRepository implements LumenRepository {
  FakeRepository({
    List<Task>? tasks,
    this.notesList = const [],
    this.hits = const [],
    this.tagList = const [],
    this.stores = const [],
    this.items = const [],
    this.failWith,
  }) : _tasks = tasks ?? [];

  final List<Task> _tasks;
  final List<NoteSummary> notesList;
  final List<SearchHit> hits;
  final List<TagCount> tagList;
  final List<PasswordStore> stores;
  final List<PasswordItem> items;
  final ApiException? failWith;

  final List<String> patched = [];
  final List<String> created = [];
  final List<String> revealed = [];
  final List<String> unlocked = [];

  @override
  Future<Identity> me() async =>
      const Identity(authenticated: true, mode: 'none', name: 'локально');

  @override
  Future<List<Task>> tasks({String view = 'today'}) async {
    if (failWith != null) throw failWith!;
    return _tasks;
  }

  @override
  Future<Task> patchTask(String id, {TaskStatus? status, String? due}) async {
    patched.add('$id:${status?.name}');
    final i = _tasks.indexWhere((t) => t.id == id);
    final updated = Task(
      id: id,
      notePath: _tasks[i].notePath,
      line: _tasks[i].line,
      status: status ?? _tasks[i].status,
      text: _tasks[i].text,
    );
    _tasks[i] = updated;
    return updated;
  }

  @override
  Future<Task> createTask(String text, {String? note, String? due}) async {
    created.add(text);
    final task = Task(
        id: 'new',
        notePath: 'inbox.md',
        line: 0,
        status: TaskStatus.todo,
        text: text);
    _tasks.add(task);
    return task;
  }

  @override
  Future<List<NoteSummary>> notes({String? type, String? tag}) async {
    if (type == 'project') {
      return notesList.where((n) => n.type == 'project').toList();
    }
    return notesList;
  }

  @override
  Future<Note> note(String pathOrId) async => Note(
        id: pathOrId,
        path: pathOrId,
        title: 'Заметка',
        type: 'note',
        body: '# Заметка\n\nтекст',
        revision: 3,
      );

  @override
  Future<Note> saveNote(String pathOrId, String body, int revision) async =>
      Note(
          id: pathOrId,
          path: pathOrId,
          title: 'Заметка',
          type: 'note',
          body: body,
          revision: revision + 1);

  @override
  Future<List<NoteSummary>> backlinks(String pathOrId) async => const [];

  @override
  Future<List<CalendarEvent>> calendar(DateTime from, DateTime to) async => [
        CalendarEvent(
          id: 'task:t1a',
          source: 'task',
          title: 'Событие в календаре',
          start: DateTime.now(),
          allDay: true,
        ),
      ];

  @override
  Future<Task> moveEvent(String ref, DateTime date) async => const Task(
      id: 't1a', notePath: 'a.md', line: 0, status: TaskStatus.todo, text: 'x');

  @override
  Future<List<SearchHit>> search(String query) async => hits;

  @override
  Future<List<TagCount>> tags() async => tagList;

  @override
  Future<List<Integration>> integrations() async => const [];

  @override
  Future<List<IntegrationKind>> integrationKinds() async => const [];

  @override
  Future<void> addIntegration(
      String kind, String name, String interval, Map<String, String> v) async {}

  @override
  Future<void> setIntegrationEnabled(String id, bool enabled) async {}

  @override
  Future<void> deleteIntegration(String id) async {}

  @override
  Future<String> syncIntegration(String id) async => 'ок';

  @override
  Future<List<PasswordStore>> passwordStores() async => stores;

  @override
  Future<List<PasswordKind>> passwordKinds() async => const [];

  @override
  Future<void> addPasswordStore(
      String kind, String name, Map<String, String> values) async {}

  @override
  Future<void> deletePasswordStore(String id) async {}

  @override
  Future<void> unlockPasswordStore(String id, String passphrase) async {
    unlocked.add('$id:$passphrase');
  }

  @override
  Future<void> lockPasswordStore(String id) async {}

  @override
  Future<void> lockAllPasswordStores() async {}

  @override
  Future<List<PasswordItem>> passwordItems(String id,
          {String query = ''}) async =>
      items;

  @override
  Future<String> revealPassword(
      String storeId, String itemId, String field) async {
    revealed.add('$storeId/$itemId/$field');
    return 's3cret';
  }
}

/// Приложение десктопное: тесты должны идти в окне рабочего размера, иначе
/// нижние пункты боковой навигации уходят за край и до них не дотянуться.
void desktopWindow(WidgetTester tester) {
  tester.view.physicalSize = const Size(1440, 1024);
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.reset);
}

Widget wrap(LumenRepository repository) => MaterialApp(
      theme: LumenTheme.light(),
      home: AppShell(repository: repository),
    );

void main() {
  // Экраны с датами используют русские форматы, как и приложение в main().
  setUpAll(() async => initializeDateFormatting('ru'));

  testWidgets('показывает задачи представления «Сегодня»', (tester) async {
    desktopWindow(tester);
    final repo = FakeRepository(tasks: [
      Task(
        id: 't1a',
        notePath: '40-daily/2026/2026-09-08.md',
        line: 10,
        status: TaskStatus.todo,
        text: 'Позвонить в банк',
        due: DateTime.now(),
        priority: 'A',
      ),
      const Task(
        id: 't2b',
        notePath: '00-inbox/quick.md',
        line: 4,
        status: TaskStatus.doing,
        text: 'Собрать скелет MVP',
      ),
    ]);

    await tester.pumpWidget(wrap(repo));
    await tester.pumpAndSettle();

    expect(find.text('Позвонить в банк'), findsOneWidget);
    expect(find.text('Собрать скелет MVP'), findsOneWidget);
  });

  testWidgets('клик по задаче меняет статус через репозиторий', (tester) async {
    desktopWindow(tester);
    final repo = FakeRepository(tasks: [
      const Task(
        id: 't1a',
        notePath: 'a.md',
        line: 0,
        status: TaskStatus.todo,
        text: 'Купить молоко',
      ),
    ]);

    await tester.pumpWidget(wrap(repo));
    await tester.pumpAndSettle();

    await tester.tap(find.text('Купить молоко'));
    await tester.pumpAndSettle();

    // todo → doing, как и в веб-клиенте.
    expect(repo.patched, ['t1a:doing']);
  });

  testWidgets('быстрый ввод создаёт задачу', (tester) async {
    desktopWindow(tester);
    final repo = FakeRepository();

    await tester.pumpWidget(wrap(repo));
    await tester.pumpAndSettle();

    await tester.enterText(find.byType(TextField).last, 'Новая задача');
    await tester.testTextInput.receiveAction(TextInputAction.done);
    await tester.pumpAndSettle();

    expect(repo.created, ['Новая задача']);
  });

  testWidgets('без сервера объясняет, что делать', (tester) async {
    desktopWindow(tester);
    final repo =
        FakeRepository(failWith: ApiException(0, 'соединение отклонено'));

    await tester.pumpWidget(wrap(repo));
    await tester.pumpAndSettle();

    expect(find.textContaining('lumend'), findsOneWidget);
    expect(find.text('Повторить'), findsOneWidget);
  });

  testWidgets('переключение представления перезапрашивает задачи',
      (tester) async {
    desktopWindow(tester);
    final repo = FakeRepository(tasks: [
      const Task(
          id: 'i1',
          notePath: 'a.md',
          line: 0,
          status: TaskStatus.todo,
          text: 'Из инбокса'),
    ]);

    await tester.pumpWidget(wrap(repo));
    await tester.pumpAndSettle();

    await tester.tap(find.text('Входящие'));
    await tester.pumpAndSettle();

    expect(find.text('Из инбокса'), findsOneWidget);
  });

  testWidgets('в боковой навигации есть все разделы веб-клиента',
      (tester) async {
    desktopWindow(tester);
    await tester.pumpWidget(wrap(FakeRepository()));
    await tester.pumpAndSettle();

    for (final label in [
      'Сегодня',
      'Входящие',
      'Неделя',
      'Просрочено',
      'Предстоит',
      'Без даты',
      'Все задачи',
      'Календарь',
      'Все заметки',
      'Поиск',
      'Интеграции',
      'Пароли',
    ]) {
      expect(find.text(label), findsWidgets, reason: 'нет раздела $label');
    }
  });

  testWidgets('календарь открывается и показывает события', (tester) async {
    desktopWindow(tester);
    await tester.pumpWidget(wrap(FakeRepository()));
    await tester.pumpAndSettle();

    await tester.tap(find.text('Календарь'));
    await tester.pumpAndSettle();

    expect(find.text('Событие в календаре'), findsWidgets);
  });

  testWidgets('список заметок открывает заметку', (tester) async {
    desktopWindow(tester);
    final repo = FakeRepository(notesList: const [
      NoteSummary(
          id: 'n1', path: '10-notes/a.md', title: 'Архитектура', type: 'note'),
    ]);

    await tester.pumpWidget(wrap(repo));
    await tester.pumpAndSettle();

    await tester.tap(find.text('Все заметки'));
    await tester.pumpAndSettle();
    expect(find.text('Архитектура'), findsOneWidget);

    await tester.tap(find.text('Архитектура'));
    await tester.pumpAndSettle();
    expect(find.textContaining('# Заметка'), findsWidgets);
  });

  testWidgets('поиск показывает результаты', (tester) async {
    desktopWindow(tester);
    final repo = FakeRepository(hits: const [
      SearchHit(
          path: '10-notes/a.md', title: 'Найденная', snippet: '…фрагмент…'),
    ]);

    await tester.pumpWidget(wrap(repo));
    await tester.pumpAndSettle();

    await tester.tap(find.text('Поиск'));
    await tester.pumpAndSettle();

    await tester.enterText(find.byType(TextField).first, 'фраг');
    await tester.pumpAndSettle();

    expect(find.text('Найденная'), findsOneWidget);
  });

  testWidgets('закрытое хранилище просит мастер-пароль и открывается',
      (tester) async {
    desktopWindow(tester);
    final repo = FakeRepository(stores: const [
      PasswordStore(
        id: 'bw',
        kind: 'bitwarden',
        title: 'Bitwarden / Vaultwarden',
        name: 'Мой Vaultwarden',
        unlocked: false,
        needsPassphrase: true,
      ),
    ]);

    await tester.pumpWidget(wrap(repo));
    await tester.pumpAndSettle();

    await tester.tap(find.text('Пароли'));
    await tester.pumpAndSettle();

    expect(find.text('Мой Vaultwarden'), findsOneWidget);
    await tester.enterText(
        find.widgetWithText(TextField, 'Мастер-пароль'), 'мастер');
    await tester.tap(find.text('Открыть'));
    await tester.pumpAndSettle();

    expect(repo.unlocked, ['bw:мастер']);
  });

  testWidgets('пароль показывается только по явному запросу', (tester) async {
    desktopWindow(tester);
    final repo = FakeRepository(
      stores: const [
        PasswordStore(
          id: 'bw',
          kind: 'bitwarden',
          title: 'Bitwarden',
          name: 'Хранилище',
          unlocked: true,
          needsPassphrase: false,
        ),
      ],
      items: const [
        PasswordItem(
            id: 'i1', name: 'GitHub', username: 'alex', fields: ['password']),
      ],
    );

    await tester.pumpWidget(wrap(repo));
    await tester.pumpAndSettle();

    await tester.tap(find.text('Пароли'));
    await tester.pumpAndSettle();

    await tester.tap(find.text('GitHub'));
    await tester.pumpAndSettle();

    // До нажатия «Показать» значение не запрашивалось.
    expect(repo.revealed, isEmpty);
    expect(find.text('••••••••••••'), findsOneWidget);

    await tester.tap(find.text('Показать'));
    await tester.pumpAndSettle();

    expect(repo.revealed, ['bw/i1/password']);
    expect(find.text('s3cret'), findsOneWidget);
  });
}
