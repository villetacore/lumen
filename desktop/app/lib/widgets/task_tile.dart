import 'package:flutter/material.dart';
import 'package:intl/intl.dart';

import '../models.dart';

/// Строка задачи: чекбокс, текст и компактные чипы с метаданными.
class TaskTile extends StatelessWidget {
  const TaskTile({super.key, required this.task, required this.onToggle});

  final Task task;
  final ValueChanged<TaskStatus> onToggle;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final muted = theme.colorScheme.onSurfaceVariant;
    final isClosed =
        task.status == TaskStatus.done || task.status == TaskStatus.cancelled;

    return InkWell(
      onTap: () => onToggle(task.status.next),
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 4, horizontal: 6),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            _Checkbox(
                status: task.status, onTap: () => onToggle(task.status.next)),
            const SizedBox(width: 9),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    task.text,
                    style: theme.textTheme.bodyMedium?.copyWith(
                      color: isClosed ? muted : null,
                      decoration: isClosed ? TextDecoration.lineThrough : null,
                    ),
                  ),
                  const SizedBox(height: 2),
                  Wrap(
                    spacing: 6,
                    runSpacing: 2,
                    children: [
                      if (task.priority != null)
                        _Chip(task.priority!, emphasis: true),
                      if (task.date != null)
                        _Chip(_formatDate(task.date!), warning: task.isOverdue),
                      if (task.estimate != null) _Chip(task.estimate!),
                      if (task.project != null) _Chip('◆ ${task.project}'),
                      _Chip(task.notePath.split('/').last),
                    ],
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  static String _formatDate(DateTime d) {
    final today = DateTime.now();
    final days = DateTime(d.year, d.month, d.day)
        .difference(DateTime(today.year, today.month, today.day))
        .inDays;
    return switch (days) {
      0 => 'сегодня',
      1 => 'завтра',
      -1 => 'вчера',
      _ => DateFormat('d MMM', 'ru').format(d),
    };
  }
}

class _Checkbox extends StatelessWidget {
  const _Checkbox({required this.status, required this.onTap});

  final TaskStatus status;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final (Color border, Color fill, String mark) = switch (status) {
      TaskStatus.done => (scheme.primary, scheme.primary, '✓'),
      TaskStatus.doing => (scheme.tertiary, Colors.transparent, '/'),
      TaskStatus.cancelled => (scheme.outline, Colors.transparent, '×'),
      TaskStatus.deferred => (scheme.outline, Colors.transparent, '>'),
      TaskStatus.todo => (scheme.outline, Colors.transparent, ''),
    };

    return GestureDetector(
      onTap: onTap,
      child: Container(
        width: 16,
        height: 16,
        margin: const EdgeInsets.only(top: 3),
        decoration: BoxDecoration(
          border: Border.all(color: border, width: 1.5),
          borderRadius: BorderRadius.circular(4),
          color: fill,
        ),
        alignment: Alignment.center,
        child: Text(
          mark,
          style: TextStyle(
            fontSize: 10,
            height: 1,
            color: status == TaskStatus.done ? scheme.onPrimary : border,
          ),
        ),
      ),
    );
  }
}

class _Chip extends StatelessWidget {
  const _Chip(this.label, {this.warning = false, this.emphasis = false});

  final String label;
  final bool warning;
  final bool emphasis;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final color = warning ? scheme.error : scheme.onSurfaceVariant;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 1),
      decoration: BoxDecoration(
        borderRadius: BorderRadius.circular(9),
        border: Border.all(color: color.withValues(alpha: 0.35)),
      ),
      child: Text(
        label,
        style: TextStyle(
          fontSize: 11,
          color: color,
          fontWeight: emphasis ? FontWeight.w600 : FontWeight.w400,
        ),
      ),
    );
  }
}
