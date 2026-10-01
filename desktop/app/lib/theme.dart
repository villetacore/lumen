import 'package:flutter/material.dart';

/// Тема повторяет веб-клиент: плотная сетка, тёплый акцент, спокойный фон.
class LumenTheme {
  static const seed = Color(0xFFB0553A);

  static ThemeData light() => _base(ColorScheme.fromSeed(seedColor: seed));

  static ThemeData dark() => _base(
        ColorScheme.fromSeed(seedColor: seed, brightness: Brightness.dark),
      );

  static ThemeData _base(ColorScheme scheme) {
    final isDark = scheme.brightness == Brightness.dark;
    return ThemeData(
      colorScheme: scheme,
      useMaterial3: true,
      scaffoldBackgroundColor:
          isDark ? const Color(0xFF17181A) : const Color(0xFFFBFBFA),
      visualDensity: VisualDensity.compact,
      dividerTheme: DividerThemeData(
        space: 1,
        thickness: 1,
        color: isDark ? const Color(0xFF2B2D31) : const Color(0xFFE2E2DD),
      ),
      listTileTheme: const ListTileThemeData(
        dense: true,
        visualDensity: VisualDensity.compact,
        minVerticalPadding: 2,
      ),
      textTheme: const TextTheme(
        titleLarge: TextStyle(fontSize: 19, fontWeight: FontWeight.w600),
        bodyMedium: TextStyle(fontSize: 13.5, height: 1.5),
        labelSmall: TextStyle(fontSize: 11, letterSpacing: 0.8),
      ),
    );
  }
}
