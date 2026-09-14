// The home shell: tabs, and Settings first while nothing is configured.
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:ssh_sentinel/app.dart';
import 'package:ssh_sentinel/services/app_services.dart';
import 'package:ssh_sentinel/services/device_registration.dart';
import 'package:ssh_sentinel/services/notification_service.dart';
import 'package:ssh_sentinel/services/pending_requests.dart';
import 'package:ssh_sentinel/services/settings_store.dart';

import 'fakes/fake_api.dart';

AppServices services({bool configured = false}) {
  final settings = SettingsStore(store: MemoryKeyValueStore());
  if (configured) {
    settings.setBackend(baseUrl: 'https://api.example.com', adminToken: 'tok');
  }
  final api = FakeSentinelApi();
  return AppServices(
    settings: settings,
    api: api,
    pending: PendingRequests(),
    notifications: NotificationService(),
    registrar: DeviceRegistrar(
      api: api,
      settings: settings,
      fcmTokenProvider: () async => 'fcm-token',
    ),
  );
}

void main() {
  testWidgets('opens on Settings until the backend is configured', (tester) async {
    tester.view.physicalSize = const Size(800, 2000);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    await tester.pumpWidget(SentinelApp(services: services()));
    await tester.pumpAndSettle();

    expect(find.byKey(const ValueKey('base_url_field')), findsOneWidget);

    await tester.tap(find.text('History'));
    await tester.pumpAndSettle();
    expect(find.text('Backend URL and admin token are not set.'), findsOneWidget);

    await tester.tap(find.text('Open Settings'));
    await tester.pumpAndSettle();
    expect(find.byKey(const ValueKey('base_url_field')), findsOneWidget);
  });

  testWidgets('opens on History when configured and switches tabs', (tester) async {
    await tester.pumpWidget(SentinelApp(services: services(configured: true)));
    await tester.pumpAndSettle();

    expect(find.text('No requests yet'), findsOneWidget);
    expect(find.text('Backend URL and admin token are not set.'), findsNothing);

    for (final tab in ['Whitelist', 'Blocked IPs', 'Geo rules']) {
      await tester.tap(find.text(tab));
      await tester.pumpAndSettle();
    }
    expect(find.text('No country blocks'), findsOneWidget);
  });
}
