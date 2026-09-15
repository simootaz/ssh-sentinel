import 'package:flutter/material.dart';

import 'screens/blocked_ips/blocked_ips_screen.dart';
import 'screens/geo_rules/geo_rules_screen.dart';
import 'screens/history/history_screen.dart';
import 'screens/settings/settings_screen.dart';
import 'screens/whitelist/whitelist_screen.dart';
import 'services/app_services.dart';

/// Root widget: theme, navigator and the five tabs. The incoming request
/// screen is pushed on top by the push service.
class SentinelApp extends StatelessWidget {
  const SentinelApp({super.key, required this.services, this.navigatorKey});

  final AppServices services;
  final GlobalKey<NavigatorState>? navigatorKey;

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'ssh-sentinel',
      navigatorKey: navigatorKey,
      theme: ThemeData(colorSchemeSeed: Colors.teal, brightness: Brightness.light),
      darkTheme: ThemeData(colorSchemeSeed: Colors.teal, brightness: Brightness.dark),
      home: HomeShell(services: services),
    );
  }
}

/// Bottom navigation between history, whitelist, blocked IPs, geo rules and
/// settings. Opens on Settings until the backend is configured.
class HomeShell extends StatefulWidget {
  const HomeShell({super.key, required this.services});

  final AppServices services;

  @override
  State<HomeShell> createState() => _HomeShellState();
}

class _HomeShellState extends State<HomeShell> {
  static const _settingsTab = 4;

  int _index = 0;
  bool _configured = true;

  @override
  void initState() {
    super.initState();
    _checkConfigured(jumpToSettings: true);
  }

  Future<void> _checkConfigured({bool jumpToSettings = false}) async {
    final configured = await widget.services.settings.isConfigured;
    if (!mounted) return;
    setState(() {
      _configured = configured;
      if (!configured && jumpToSettings) _index = _settingsTab;
    });
  }

  @override
  Widget build(BuildContext context) {
    final services = widget.services;
    return Scaffold(
      body: Column(
        children: [
          if (!_configured && _index != _settingsTab)
            MaterialBanner(
              forceActionsBelow: true,
              content: const Text('Backend URL and admin token are not set.'),
              leading: const Icon(Icons.warning_amber),
              actions: [
                TextButton(
                  onPressed: () => setState(() => _index = _settingsTab),
                  child: const Text('Open Settings'),
                ),
              ],
            ),
          Expanded(
            child: IndexedStack(
              index: _index,
              children: [
                HistoryScreen(api: services.api),
                WhitelistScreen(api: services.api),
                BlockedIpsScreen(api: services.api),
                GeoRulesScreen(api: services.api),
                SettingsScreen(
                  settings: services.settings,
                  api: services.api,
                  registrar: services.registrar,
                  pushAvailable: services.firebaseReady,
                  onSaved: _checkConfigured,
                ),
              ],
            ),
          ),
        ],
      ),
      bottomNavigationBar: NavigationBar(
        selectedIndex: _index,
        onDestinationSelected: (i) => setState(() => _index = i),
        destinations: const [
          NavigationDestination(icon: Icon(Icons.history), label: 'History'),
          NavigationDestination(
              icon: Icon(Icons.verified_user_outlined), label: 'Whitelist'),
          NavigationDestination(icon: Icon(Icons.block), label: 'Blocked IPs'),
          NavigationDestination(icon: Icon(Icons.public_off), label: 'Geo rules'),
          NavigationDestination(icon: Icon(Icons.settings), label: 'Settings'),
        ],
      ),
    );
  }
}
