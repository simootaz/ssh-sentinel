// Whitelist screen against the in-memory fake backend.
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:ssh_sentinel/models/enums.dart';
import 'package:ssh_sentinel/models/whitelist_entry.dart';
import 'package:ssh_sentinel/screens/whitelist/whitelist_screen.dart';
import 'package:ssh_sentinel/services/api_exceptions.dart';

import '../fakes/fake_api.dart';

/// The fake only records call names; this one also keeps the last
/// `POST /whitelist` body so the tests can check what the form sent.
class _RecordingApi extends FakeSentinelApi {
  ({String username, RequestContext context, String? server, int? ttlSeconds})? lastAdd;

  @override
  Future<WhitelistEntry> addWhitelistEntry({
    required String username,
    RequestContext context = RequestContext.ssh,
    String? server,
    int? ttlSeconds,
  }) {
    lastAdd = (username: username, context: context, server: server, ttlSeconds: ttlSeconds);
    return super.addWhitelistEntry(
        username: username, context: context, server: server, ttlSeconds: ttlSeconds);
  }
}

WhitelistEntry entry({
  String id = 'wl-1',
  String username = 'ansible',
  RequestContext context = RequestContext.ssh,
  String? server,
  DateTime? expiresAt,
  String? createdByDevice,
}) =>
    WhitelistEntry(
      id: id,
      username: username,
      context: context,
      server: server,
      expiresAt: expiresAt,
      createdAt: DateTime.utc(2026, 9, 10, 8),
      createdByDevice: createdByDevice,
    );

Future<void> pumpScreen(WidgetTester tester, FakeSentinelApi api) async {
  await tester.pumpWidget(MaterialApp(home: WhitelistScreen(api: api)));
  await tester.pumpAndSettle();
}

Future<void> openForm(WidgetTester tester) async {
  await tester.tap(find.byKey(const ValueKey('add_entry')));
  await tester.pumpAndSettle();
  expect(find.byKey(const ValueKey('submit_entry')), findsOneWidget);
}

Future<void> submit(WidgetTester tester) async {
  await tester.tap(find.byKey(const ValueKey('submit_entry')));
  await tester.pumpAndSettle();
}

Finder key(String name) => find.byKey(ValueKey(name));

void main() {
  group('list', () {
    testWidgets('renders a global permanent entry and a server entry with expiry',
        (tester) async {
      final api = FakeSentinelApi()
        ..whitelist = [
          entry(id: 'wl-1', username: 'ansible'),
          entry(
            id: 'wl-2',
            username: 'deploy',
            context: RequestContext.sudo,
            server: 'web-01',
            expiresAt: DateTime.now().toUtc().add(const Duration(hours: 5)),
            createdByDevice: 'Pixel 8',
          ),
        ];
      await pumpScreen(tester, api);

      expect(api.calls, ['listWhitelist']);
      expect(find.text('ansible'), findsOneWidget);
      expect(find.text('every server'), findsOneWidget);
      expect(find.text('permanent'), findsOneWidget);
      expect(find.text('ssh'), findsOneWidget);

      expect(find.text('deploy'), findsOneWidget);
      expect(find.text('web-01'), findsOneWidget);
      expect(find.textContaining('expires in'), findsOneWidget);
      expect(find.text('sudo'), findsOneWidget);
      expect(find.text('added by Pixel 8'), findsOneWidget);

      expect(key('delete_wl-1'), findsOneWidget);
      expect(key('delete_wl-2'), findsOneWidget);
    });

    testWidgets('empty state', (tester) async {
      await pumpScreen(tester, FakeSentinelApi());
      expect(find.text('No always-allow entries'), findsOneWidget);
      expect(key('add_entry'), findsOneWidget);
    });

    testWidgets('error then retry', (tester) async {
      final api = FakeSentinelApi()..failWith = const NetworkException('connection refused');
      await pumpScreen(tester, api);
      expect(find.textContaining('Backend unreachable'), findsOneWidget);

      api
        ..failWith = null
        ..whitelist = [entry()];
      await tester.tap(find.text('Retry'));
      await tester.pumpAndSettle();

      expect(find.textContaining('Backend unreachable'), findsNothing);
      expect(find.text('ansible'), findsOneWidget);
      expect(api.calls, ['listWhitelist', 'listWhitelist']);
    });

    testWidgets('pull to refresh reloads the list', (tester) async {
      final api = FakeSentinelApi()..whitelist = [entry()];
      await pumpScreen(tester, api);
      api.whitelist = [entry(), entry(id: 'wl-2', username: 'deploy')];

      await tester.fling(find.text('ansible'), const Offset(0, 300), 1000);
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));
      await tester.pumpAndSettle();

      expect(api.calls, ['listWhitelist', 'listWhitelist']);
      expect(find.text('deploy'), findsOneWidget);
    });
  });

  group('add', () {
    testWidgets('sudo on one server for 24 hours', (tester) async {
      final api = _RecordingApi();
      await pumpScreen(tester, api);
      await openForm(tester);

      await tester.enterText(key('username_field'), 'ansible');
      await tester.tap(find.descendant(of: key('context_choice'), matching: find.text('sudo')));
      await tester.enterText(key('server_field'), 'web-01');
      await tester.tap(key('ttl_24h'));
      await tester.pumpAndSettle();
      await submit(tester);

      final sent = api.lastAdd;
      expect(sent, isNotNull);
      expect(sent!.username, 'ansible');
      expect(sent.context, RequestContext.sudo);
      expect(sent.server, 'web-01');
      expect(sent.ttlSeconds, 86400);
      expect(api.calls, ['listWhitelist', 'addWhitelistEntry', 'listWhitelist']);

      // Back on the list, with the new entry and a confirmation.
      expect(key('submit_entry'), findsNothing);
      expect(find.text('ansible'), findsOneWidget);
      expect(find.text('web-01'), findsOneWidget);
      expect(find.text('sudo'), findsOneWidget);
      // The snack repeats the expiry, so look inside the row only.
      expect(
        find.descendant(of: find.byType(ListTile), matching: find.textContaining('expires in')),
        findsOneWidget,
      );
      expect(find.textContaining('Added ansible (sudo) on web-01'), findsOneWidget);
    });

    testWidgets('defaults: ssh, every server, 24 hours', (tester) async {
      final api = _RecordingApi();
      await pumpScreen(tester, api);
      await openForm(tester);

      await tester.enterText(key('username_field'), 'deploy');
      await submit(tester);

      final sent = api.lastAdd;
      expect(sent, isNotNull);
      expect(sent!.context, RequestContext.ssh);
      expect(sent.server, isNull);
      expect(sent.ttlSeconds, 86400);
      expect(find.text('every server'), findsOneWidget);
    });

    testWidgets('permanent sends no ttl', (tester) async {
      final api = _RecordingApi();
      await pumpScreen(tester, api);
      await openForm(tester);

      await tester.enterText(key('username_field'), 'deploy');
      await tester.tap(key('ttl_permanent'));
      await tester.pumpAndSettle();
      await submit(tester);

      expect(api.lastAdd?.ttlSeconds, isNull);
      expect(find.text('permanent'), findsOneWidget);
    });

    testWidgets('custom duration goes through the picker', (tester) async {
      final api = _RecordingApi();
      await pumpScreen(tester, api);
      await openForm(tester);

      await tester.enterText(key('username_field'), 'deploy');
      await tester.tap(key('ttl_custom'));
      await tester.pumpAndSettle();
      await tester.enterText(key('ttl_custom_value'), '3');
      await tester.tap(key('ttl_custom_unit'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('days').last);
      await tester.pumpAndSettle();
      await tester.tap(key('ttl_custom_ok'));
      await tester.pumpAndSettle();

      expect(find.text('Custom (3 d)'), findsOneWidget);
      await submit(tester);
      expect(api.lastAdd?.ttlSeconds, 3 * 86400);
    });

    testWidgets('empty username is refused without a call', (tester) async {
      final api = _RecordingApi();
      await pumpScreen(tester, api);
      await openForm(tester);

      await submit(tester);

      expect(find.text('Enter a username.'), findsOneWidget);
      expect(api.lastAdd, isNull);
      expect(key('submit_entry'), findsOneWidget);
    });

    testWidgets('unknown server shows an inline error and keeps the form', (tester) async {
      final api = FakeSentinelApi();
      await pumpScreen(tester, api);
      await openForm(tester);

      await tester.enterText(key('username_field'), 'deploy');
      await tester.enterText(key('server_field'), 'nope-99');
      api.failWith = const NotFoundException('unknown server');
      await submit(tester);
      api.failWith = null;

      expect(find.text('Unknown server name: nope-99'), findsOneWidget);
      expect(key('submit_entry'), findsOneWidget);
      expect(api.whitelist, isEmpty);
    });
  });

  group('delete', () {
    testWidgets('asks, deletes, refreshes', (tester) async {
      final api = FakeSentinelApi()..whitelist = [entry()];
      await pumpScreen(tester, api);

      await tester.tap(key('delete_wl-1'));
      await tester.pumpAndSettle();
      expect(find.text('Remove entry?'), findsOneWidget);
      expect(api.calls, ['listWhitelist']);

      await tester.tap(find.widgetWithText(FilledButton, 'Remove'));
      await tester.pumpAndSettle();

      expect(api.calls, ['listWhitelist', 'deleteWhitelistEntry', 'listWhitelist']);
      expect(find.text('ansible'), findsNothing);
      expect(find.text('No always-allow entries'), findsOneWidget);
      expect(find.text('Removed ansible (ssh).'), findsOneWidget);
    });

    testWidgets('cancel keeps the entry', (tester) async {
      final api = FakeSentinelApi()..whitelist = [entry()];
      await pumpScreen(tester, api);

      await tester.tap(key('delete_wl-1'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Cancel'));
      await tester.pumpAndSettle();

      expect(api.calls, ['listWhitelist']);
      expect(find.text('ansible'), findsOneWidget);
    });
  });
}
