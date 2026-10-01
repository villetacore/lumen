import 'package:flutter/material.dart';
import 'package:intl/intl.dart';

import '../data/lumen_repository.dart';
import '../models.dart';
import 'tasks_screen.dart';

const _dowShort = ['пн', 'вт', 'ср', 'чт', 'пт', 'сб', 'вс'];

/// Месячная сетка с задачами и событиями интеграций. Задачу можно перетащить
/// на другой день — сервер перепишет [scheduled] в самом .md файле.
class CalendarScreen extends StatefulWidget {
  const CalendarScreen({super.key, required this.repository, this.onOpenNote});

  final LumenRepository repository;
  final void Function(String path)? onOpenNote;

  @override
  State<CalendarScreen> createState() => _CalendarScreenState();
}

class _CalendarScreenState extends State<CalendarScreen> {
  late DateTime _cursor = DateTime(DateTime.now().year, DateTime.now().month);
  late Future<List<CalendarEvent>> _events = _load();

  List<DateTime> get _days {
    final first = DateTime(_cursor.year, _cursor.month);
    final start = first.subtract(Duration(days: (first.weekday + 6) % 7));
    return List.generate(42, (i) => start.add(Duration(days: i)));
  }

  Future<List<CalendarEvent>> _load() {
    final days = _days;
    return widget.repository.calendar(days.first, days.last);
  }

  void _reload() => setState(() => _events = _load());

  void _shift(int months) {
    setState(() {
      _cursor = DateTime(_cursor.year, _cursor.month + months);
      _events = _load();
    });
  }

  Future<void> _move(String ref, DateTime day) async {
    try {
      await widget.repository.moveEvent(ref, day);
      _reload();
    } on ApiException catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context)
            .showSnackBar(SnackBar(content: Text(e.message)));
      }
    }
  }

  static bool _sameDay(DateTime a, DateTime b) =>
      a.year == b.year && a.month == b.month && a.day == b.day;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final today = DateTime.now();

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        // Заголовок и панель — разными строками: в узком окне они не влезали
        // в один Row и вызывали overflow.
        Row(
          children: [
            Text('Календарь', style: theme.textTheme.titleLarge),
            const Spacer(),
            IconButton(onPressed: _reload, icon: const Icon(Icons.refresh)),
          ],
        ),
        Row(
          children: [
            IconButton(
              onPressed: () => _shift(-1),
              icon: const Icon(Icons.chevron_left),
              visualDensity: VisualDensity.compact,
            ),
            Flexible(
              child: Text(
                toBeginningOfSentenceCase(
                        DateFormat.yMMMM('ru').format(_cursor)) ??
                    '',
                textAlign: TextAlign.center,
                overflow: TextOverflow.ellipsis,
              ),
            ),
            IconButton(
              onPressed: () => _shift(1),
              icon: const Icon(Icons.chevron_right),
              visualDensity: VisualDensity.compact,
            ),
            TextButton(
              onPressed: () {
                setState(() {
                  _cursor = DateTime(today.year, today.month);
                  _events = _load();
                });
              },
              child: const Text('Сегодня'),
            ),
          ],
        ),
        const SizedBox(height: 8),
        Row(
          children: [
            for (final d in _dowShort)
              Expanded(
                child: Text(d,
                    style: theme.textTheme.labelSmall,
                    textAlign: TextAlign.center),
              ),
          ],
        ),
        const SizedBox(height: 4),
        Expanded(
          child: FutureBuilder<List<CalendarEvent>>(
            future: _events,
            builder: (context, snapshot) {
              if (snapshot.connectionState == ConnectionState.waiting) {
                return const Center(child: CircularProgressIndicator());
              }
              if (snapshot.hasError) {
                return ServerUnreachable(
                    message: '${snapshot.error}', onRetry: _reload);
              }

              final byDay = <String, List<CalendarEvent>>{};
              for (final e in snapshot.data ?? const <CalendarEvent>[]) {
                final key = DateFormat('yyyy-MM-dd').format(e.start);
                byDay.putIfAbsent(key, () => []).add(e);
              }

              return GridView.count(
                crossAxisCount: 7,
                childAspectRatio: 0.85,
                mainAxisSpacing: 1,
                crossAxisSpacing: 1,
                children: [
                  for (final day in _days)
                    _DayCell(
                      day: day,
                      inMonth: day.month == _cursor.month,
                      isToday: _sameDay(day, today),
                      events: byDay[DateFormat('yyyy-MM-dd').format(day)] ??
                          const [],
                      onDropTask: (ref) => _move(ref, day),
                      onOpenNote: widget.onOpenNote,
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

class _DayCell extends StatelessWidget {
  const _DayCell({
    required this.day,
    required this.inMonth,
    required this.isToday,
    required this.events,
    required this.onDropTask,
    this.onOpenNote,
  });

  final DateTime day;
  final bool inMonth;
  final bool isToday;
  final List<CalendarEvent> events;
  final void Function(String ref) onDropTask;
  final void Function(String path)? onOpenNote;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;

    return DragTarget<String>(
      onAcceptWithDetails: (d) => onDropTask(d.data),
      builder: (context, candidate, _) => Container(
        decoration: BoxDecoration(
          color: inMonth
              ? scheme.surface
              : scheme.surfaceContainerHighest.withValues(alpha: 0.4),
          border: Border.all(
            color:
                candidate.isNotEmpty ? scheme.primary : scheme.outlineVariant,
            width: candidate.isNotEmpty ? 2 : 0.5,
          ),
        ),
        padding: const EdgeInsets.all(3),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 5, vertical: 1),
              decoration: BoxDecoration(
                color: isToday ? scheme.primary : Colors.transparent,
                borderRadius: BorderRadius.circular(9),
              ),
              child: Text(
                '${day.day}',
                style: TextStyle(
                  fontSize: 11.5,
                  color: isToday
                      ? scheme.onPrimary
                      : (inMonth ? null : scheme.onSurfaceVariant),
                ),
              ),
            ),
            const SizedBox(height: 2),
            Expanded(
              child: ListView(
                padding: EdgeInsets.zero,
                children: [
                  for (final e in events)
                    _EventChip(event: e, onOpenNote: onOpenNote)
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _EventChip extends StatelessWidget {
  const _EventChip({required this.event, this.onOpenNote});

  final CalendarEvent event;
  final void Function(String path)? onOpenNote;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final color = switch (event.source) {
      'task' => scheme.primary,
      'note' => scheme.tertiary,
      'ics' => scheme.secondary,
      _ => scheme.outline,
    };

    final chip = Container(
      margin: const EdgeInsets.only(bottom: 2),
      padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 1),
      decoration: BoxDecoration(
        border: Border(left: BorderSide(color: color, width: 2)),
        color: scheme.surfaceContainerHighest.withValues(alpha: 0.6),
      ),
      child: Text(
        event.allDay
            ? event.title
            : '${DateFormat.Hm().format(event.start)} ${event.title}',
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
        style: TextStyle(
          fontSize: 11,
          decoration: event.status == TaskStatus.done
              ? TextDecoration.lineThrough
              : null,
        ),
      ),
    );

    final tappable = GestureDetector(
      onTap: event.path != null && onOpenNote != null
          ? () => onOpenNote!(event.path!)
          : null,
      child: chip,
    );

    // Перетаскивать можно только задачи: событие из внешнего календаря Lumen
    // не двигает.
    if (event.source != 'task') return tappable;
    return Draggable<String>(
      data: event.id,
      feedback: Material(
          color: Colors.transparent,
          child: Opacity(opacity: 0.85, child: chip)),
      childWhenDragging: Opacity(opacity: 0.3, child: chip),
      child: tappable,
    );
  }
}
