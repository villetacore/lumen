import 'package:flutter/material.dart';

import '../models.dart';

/// Форма настроек, построенная по схеме с сервера. Одна и та же для интеграций
/// и для хранилищ паролей — схему присылает бэкенд, клиент её только рисует.
class ConfigForm extends StatelessWidget {
  const ConfigForm({
    super.key,
    required this.fields,
    required this.values,
    required this.onChanged,
  });

  final List<ConfigField> fields;
  final Map<String, String> values;
  final void Function(String key, String value) onChanged;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        for (final f in fields)
          if (f.type == 'bool')
            CheckboxListTile(
              dense: true,
              contentPadding: EdgeInsets.zero,
              controlAffinity: ListTileControlAffinity.leading,
              value: values[f.key] == 'true',
              onChanged: (v) => onChanged(f.key, v == true ? 'true' : 'false'),
              title: Text(f.label),
              subtitle: f.help == null ? null : Text(f.help!),
            )
          else
            Padding(
              padding: const EdgeInsets.symmetric(vertical: 4),
              child: TextFormField(
                initialValue: values[f.key] ?? '',
                obscureText: false,
                autocorrect: false,
                enableSuggestions: false,
                decoration: InputDecoration(
                  isDense: true,
                  border: const OutlineInputBorder(),
                  labelText: f.required ? '${f.label} *' : f.label,
                  hintText: f.placeholder,
                  helperText: f.help,
                  helperMaxLines: 3,
                ),
                onChanged: (v) => onChanged(f.key, v),
              ),
            ),
      ],
    );
  }
}

/// Плашка ошибки в едином стиле.
class ErrorBanner extends StatelessWidget {
  const ErrorBanner(this.message, {super.key, this.onRetry});

  final String message;
  final VoidCallback? onRetry;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Container(
      width: double.infinity,
      margin: const EdgeInsets.symmetric(vertical: 6),
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
      decoration: BoxDecoration(
        border: Border.all(color: scheme.error.withValues(alpha: 0.4)),
        borderRadius: BorderRadius.circular(6),
        color: scheme.error.withValues(alpha: 0.08),
      ),
      child: Row(
        children: [
          Expanded(child: Text(message, style: TextStyle(color: scheme.error))),
          if (onRetry != null)
            TextButton(onPressed: onRetry, child: const Text('Повторить')),
        ],
      ),
    );
  }
}

/// Заголовок раздела: та же плотная типографика, что и в вебе.
class SectionHead extends StatelessWidget {
  const SectionHead(this.text, {super.key, this.trailing});

  final String text;
  final Widget? trailing;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(top: 14, bottom: 4),
      child: Row(
        children: [
          Text(
            text.toUpperCase(),
            style: Theme.of(context).textTheme.labelSmall?.copyWith(
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                  fontWeight: FontWeight.w600,
                ),
          ),
          const SizedBox(width: 8),
          const Expanded(child: Divider()),
          if (trailing != null) ...[const SizedBox(width: 8), trailing!],
        ],
      ),
    );
  }
}
