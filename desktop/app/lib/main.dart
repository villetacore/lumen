import 'package:flutter/material.dart';
import 'package:intl/date_symbol_data_local.dart';

import 'data/lumen_repository.dart';
import 'app_shell.dart';
import 'theme.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  await initializeDateFormatting('ru');
  runApp(const LumenApp());
}

class LumenApp extends StatelessWidget {
  const LumenApp({super.key});

  @override
  Widget build(BuildContext context) {
    // На M2 здесь появится выбор источника: HTTP-сервер либо локальный vault
    // через lumen-core. Экраны зависят только от LumenRepository.
    final repository = HttpRepository();

    return MaterialApp(
      title: 'Lumen',
      debugShowCheckedModeBanner: false,
      theme: LumenTheme.light(),
      darkTheme: LumenTheme.dark(),
      themeMode: ThemeMode.system,
      home: AppShell(repository: repository),
    );
  }
}
