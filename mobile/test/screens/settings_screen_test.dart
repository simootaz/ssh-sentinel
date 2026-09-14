// Settings screen against the fake API and an in-memory settings store.
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:ssh_sentinel/models/device.dart';
import 'package:ssh_sentinel/screens/settings/settings_screen.dart';
import 'package:ssh_sentinel/services/api_exceptions.dart';
import 'package:ssh_sentinel/services/device_registration.dart';
import 'package:ssh_sentinel/services/settings_store.dart';

import '../fakes/fake_api.dart';

const urlField = ValueKey('base_url_field');
const tokenField = ValueKey('admin_token_field');
const labelField = ValueKey('device_label_field');
const saveButton = ValueKey('save_button');
const testButton = ValueKey('test_button');

/// Everything one test needs, built fresh for each test.
class Harness {
  Harness({Future<String?> Function()? fcmTokenProvider}) {
    registrar = DeviceRegistrar(
      api: api,
      settings: store,
      fcmTokenProvider: fcmTokenProvider ?? () async => 'fcm-token',
    );
  }

  final api = FakeSentinelApi();
  final store = SettingsStore(store: MemoryKeyValueStore());
  late final DeviceRegistrar registrar;
  int savedCalls = 0;

  /// Pumps the screen. The ListView builds rows lazily, so the test viewport
  /// is made tall enough for the whole page to be on screen at once; the
  /// tests then do not need to scroll to the phone list at the bottom.
  Future<void> pump(WidgetTester tester, {bool pushAvailable = true}) async {
    tester.view.physicalSize = const Size(800, 2400);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    await tester.pumpWidget(MaterialApp(
      home: SettingsScreen(
        settings: store,
        api: api,
        registrar: registrar,
        pushAvailable: pushAvailable,
        onSaved: () => savedCalls++,
      ),
    ));
    await tester.pumpAndSettle();
  }
}

/// Text inside the status card only. The phone list shows its own error when
/// the backend fails, so an unscoped text finder would count both.
Finder statusText(String fragment) => find.descendant(
      of: find.byKey(const ValueKey('status_card')),
      matching: find.textContaining(fragment),
    );

String fieldText(WidgetTester tester, Key key) =>
    tester.widget<TextField>(find.byKey(key)).controller!.text;

Future<void> fillAndSave(WidgetTester tester,
    {String url = 'https://api.example.com/', String token = 'secret'}) async {
  await tester.enterText(find.byKey(urlField), url);
  await tester.enterText(find.byKey(tokenField), token);
  await tester.tap(find.byKey(saveButton));
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('fields show the stored values', (tester) async {
    final h = Harness();
    await h.store.setBackend(baseUrl: 'https://api.example.com', adminToken: 'secret');
    await h.store.setDeviceLabel('Pixel 8');
    await h.store.setRegistration(deviceId: 'dev-a', fcmToken: 'fcm-token');

    await h.pump(tester);

    expect(fieldText(tester, urlField), 'https://api.example.com');
    expect(fieldText(tester, tokenField), 'secret');
    expect(fieldText(tester, labelField), 'Pixel 8');
    expect(find.text('Pixel 8'), findsWidgets);
    expect(find.text('device id dev-a'), findsOneWidget);
    expect(tester.widget<TextField>(find.byKey(tokenField)).obscureText, isTrue);
  });

  testWidgets('the token field has a show/hide toggle', (tester) async {
    final h = Harness();
    await h.pump(tester);

    expect(tester.widget<TextField>(find.byKey(tokenField)).obscureText, isTrue);
    await tester.tap(find.byTooltip('Show token'));
    await tester.pumpAndSettle();
    expect(tester.widget<TextField>(find.byKey(tokenField)).obscureText, isFalse);
  });

  testWidgets('not configured: no error, a hint to save first', (tester) async {
    final h = Harness();
    await h.pump(tester);

    expect(find.text('Save the settings to list the phones'), findsOneWidget);
    expect(find.text('not registered'), findsOneWidget);
    expect(h.api.calls, isEmpty);
  });

  testWidgets('save stores the normalised URL, registers and keeps the id',
      (tester) async {
    final h = Harness();
    await h.pump(tester);

    await tester.enterText(find.byKey(labelField), 'Pixel 8');
    await fillAndSave(tester, url: 'https://api.example.com/', token: ' secret ');

    expect(await h.store.baseUrl, 'https://api.example.com');
    expect(await h.store.adminToken, 'secret');
    expect(await h.store.deviceLabel, 'Pixel 8');
    expect(h.savedCalls, 1);
    expect(h.api.calls, contains('registerDevice'));
    expect(await h.store.deviceId, 'dev-1');
    expect(await h.store.registeredFcmToken, 'fcm-token');
    expect(find.text('Saved. Registered as Pixel 8 (id dev-1)'), findsOneWidget);
    expect(find.text('device id dev-1'), findsOneWidget);
    // The list was refreshed and this phone is marked.
    expect(find.text('this phone'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('an invalid URL shows the reason and stores nothing', (tester) async {
    final h = Harness();
    await h.pump(tester);

    await fillAndSave(tester, url: 'api.example.com', token: 'secret');

    expect(find.text('Not a valid URL.'), findsOneWidget);
    expect(await h.store.baseUrl, isNull);
    expect(await h.store.adminToken, isNull);
    expect(h.savedCalls, 0);
    expect(h.api.calls, isEmpty);
    expect(find.textContaining('Saved'), findsNothing);
  });

  testWidgets('an empty token shows the reason and stores nothing', (tester) async {
    final h = Harness();
    await h.pump(tester);

    await fillAndSave(tester, url: 'https://api.example.com', token: '');

    expect(find.text('Enter the admin token.'), findsOneWidget);
    expect(await h.store.baseUrl, isNull);
    expect(h.api.calls, isEmpty);
  });

  testWidgets('unreachable backend: the error is shown, nothing crashes',
      (tester) async {
    final h = Harness();
    h.api.failWith = const NetworkException('connection refused');
    await h.pump(tester);

    await fillAndSave(tester);

    expect(statusText('Backend unreachable'), findsOneWidget);
    expect(statusText('connection refused'), findsOneWidget);
    // The settings were saved even though the registration failed.
    expect(await h.store.baseUrl, 'https://api.example.com');
    expect(await h.store.deviceId, isNull);
    expect(tester.takeException(), isNull);
    // The buttons are usable again.
    expect(tester.widget<FilledButton>(find.byKey(saveButton)).enabled, isTrue);
  });

  testWidgets('wrong token: the message mentions the token', (tester) async {
    final h = Harness();
    h.api.failWith = const UnauthorizedException('invalid token', statusCode: 401);
    await h.pump(tester);

    await fillAndSave(tester, token: 'wrong');

    expect(statusText('Check the admin token'), findsOneWidget);
    expect(statusText('invalid token'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('test connection checks the URL then the token', (tester) async {
    final h = Harness();
    await h.pump(tester);

    await tester.enterText(find.byKey(urlField), 'https://api.example.com');
    await tester.enterText(find.byKey(tokenField), 'secret');
    await tester.tap(find.byKey(testButton));
    await tester.pumpAndSettle();

    expect(await h.store.baseUrl, 'https://api.example.com');
    expect(h.api.calls, containsAllInOrder(['health', 'listDevices']));
    expect(h.api.calls, isNot(contains('registerDevice')));
    expect(find.text('Backend reachable, token accepted'), findsOneWidget);
  });

  testWidgets('test connection reports a wrong token', (tester) async {
    final h = Harness();
    h.api.failWith = const UnauthorizedException('invalid token', statusCode: 401);
    await h.pump(tester);

    await tester.enterText(find.byKey(urlField), 'https://api.example.com');
    await tester.enterText(find.byKey(tokenField), 'wrong');
    await tester.tap(find.byKey(testButton));
    await tester.pumpAndSettle();

    expect(statusText('Check the admin token'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('registered phones are listed and can be removed', (tester) async {
    final h = Harness();
    await h.store.setBackend(baseUrl: 'https://api.example.com', adminToken: 'secret');
    await h.store.setRegistration(deviceId: 'dev-a', fcmToken: 'fcm-token');
    final now = DateTime.now().toUtc();
    h.api.devices = [
      Device(id: 'dev-a', platform: 'android', label: 'Pixel 8', lastSeenAt: now),
      Device(
        id: 'dev-b',
        platform: 'android',
        label: 'Old phone',
        lastSeenAt: now.subtract(const Duration(days: 2)),
      ),
    ];

    await h.pump(tester);

    expect(h.api.calls, contains('listDevices'));
    expect(find.text('Pixel 8'), findsWidgets);
    expect(find.text('Old phone'), findsOneWidget);
    expect(find.text('this phone'), findsOneWidget);
    expect(find.textContaining('last seen 2 d ago'), findsOneWidget);

    // Cancel first: nothing happens.
    await tester.tap(find.byKey(const ValueKey('delete_device_dev-b')));
    await tester.pumpAndSettle();
    expect(find.text('It stops receiving pushes at once. History keeps the label.'),
        findsOneWidget);
    await tester.tap(find.text('Cancel'));
    await tester.pumpAndSettle();
    expect(h.api.calls, isNot(contains('deleteDevice')));

    // Confirm: the phone is gone from the backend and from the list.
    await tester.tap(find.byKey(const ValueKey('delete_device_dev-b')));
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(FilledButton, 'Remove'));
    await tester.pumpAndSettle();

    expect(h.api.calls, contains('deleteDevice'));
    expect(h.api.devices.map((d) => d.id), ['dev-a']);
    expect(find.text('Old phone'), findsNothing);
    // This phone was not the one removed: its registration stays.
    expect(await h.store.deviceId, 'dev-a');
  });

  testWidgets('removing this phone clears its registration', (tester) async {
    final h = Harness();
    await h.store.setBackend(baseUrl: 'https://api.example.com', adminToken: 'secret');
    await h.store.setRegistration(deviceId: 'dev-a', fcmToken: 'fcm-token');
    h.api.devices = [
      const Device(id: 'dev-a', platform: 'android', label: 'Pixel 8'),
    ];

    await h.pump(tester);

    await tester.tap(find.byKey(const ValueKey('delete_device_dev-a')));
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(FilledButton, 'Remove'));
    await tester.pumpAndSettle();

    expect(await h.store.deviceId, isNull);
    expect(await h.store.registeredFcmToken, isNull);
    expect(find.text('not registered'), findsOneWidget);
    expect(find.text('No phone registered yet'), findsOneWidget);
  });

  testWidgets('a failing device list shows an error with retry', (tester) async {
    final h = Harness();
    await h.store.setBackend(baseUrl: 'https://api.example.com', adminToken: 'secret');
    h.api.failWith = const NetworkException('connection refused');

    await h.pump(tester);

    expect(find.textContaining('Backend unreachable'), findsOneWidget);

    h.api.failWith = null;
    h.api.devices = [const Device(id: 'dev-a', platform: 'android', label: 'Pixel 8')];
    await tester.tap(find.text('Retry'));
    await tester.pumpAndSettle();

    expect(find.text('Pixel 8'), findsWidgets);
    expect(find.textContaining('Backend unreachable'), findsNothing);
  });

  testWidgets('without Firebase: warning shown, save says not registered',
      (tester) async {
    final h = Harness(fcmTokenProvider: () async => null);
    await h.pump(tester, pushAvailable: false);

    expect(find.textContaining('This build has no Firebase configuration'), findsOneWidget);
    expect(find.byKey(const ValueKey('copy_token')), findsNothing);

    await fillAndSave(tester);

    expect(await h.store.baseUrl, 'https://api.example.com');
    expect(h.api.calls, isNot(contains('registerDevice')));
    expect(await h.store.deviceId, isNull);
    expect(
      find.text('Saved. Push is not available in this build (no Firebase '
          'configuration), the phone was not registered'),
      findsOneWidget,
    );
    expect(find.text('not registered'), findsOneWidget);
  });

  testWidgets('with Firebase but no token yet: save says it will register later',
      (tester) async {
    final h = Harness(fcmTokenProvider: () async => null);
    await h.pump(tester);

    await fillAndSave(tester);

    expect(h.api.calls, isNot(contains('registerDevice')));
    expect(
      find.text('Saved. No push token yet, the phone will register when it gets one'),
      findsOneWidget,
    );
  });

  testWidgets('copy push token puts the token on the clipboard', (tester) async {
    final h = Harness();
    String? copied;
    tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
      SystemChannels.platform,
      (call) async {
        if (call.method == 'Clipboard.setData') {
          copied = (call.arguments as Map)['text'] as String?;
        }
        return null;
      },
    );
    addTearDown(() => tester.binding.defaultBinaryMessenger
        .setMockMethodCallHandler(SystemChannels.platform, null));

    await h.pump(tester);

    await tester.tap(find.byKey(const ValueKey('copy_token')));
    await tester.pumpAndSettle();

    expect(copied, 'fcm-token');
    expect(find.text('Push token copied to the clipboard'), findsOneWidget);
  });

  testWidgets('copy push token without a token shows a snack', (tester) async {
    final h = Harness(fcmTokenProvider: () async => null);
    await h.pump(tester);

    await tester.tap(find.byKey(const ValueKey('copy_token')));
    await tester.pumpAndSettle();

    expect(find.text('No push token available'), findsOneWidget);
  });
}
