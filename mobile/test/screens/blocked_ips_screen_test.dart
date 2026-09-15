// Blocked IPs screen against the fake API: list, empty, error and unblock.
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:ssh_sentinel/models/blocked_ip.dart';
import 'package:ssh_sentinel/screens/blocked_ips/blocked_ips_screen.dart';
import 'package:ssh_sentinel/services/api_exceptions.dart';
import 'package:ssh_sentinel/widgets/format.dart';

import '../fakes/fake_api.dart';

BlockedIp blocked({
  required String id,
  required String ip,
  int denials = 3,
  int hits = 12,
  DateTime? lastHitAt,
  DateTime? expiresAt,
}) =>
    BlockedIp(
      id: id,
      ip: ip,
      reason: 'autoblock',
      denialCount: denials,
      firstDeniedAt: DateTime.utc(2026, 9, 14, 19, 40, 12),
      lastDeniedAt: DateTime.utc(2026, 9, 14, 20, 12, 7),
      hitCount: hits,
      lastHitAt: lastHitAt,
      createdAt: DateTime.utc(2026, 9, 14, 20, 12, 7),
      expiresAt: expiresAt,
    );

Future<void> pumpScreen(WidgetTester tester, FakeSentinelApi api) async {
  await tester.pumpWidget(MaterialApp(home: BlockedIpsScreen(api: api)));
  await tester.pumpAndSettle();
}

/// The confirm button of the open dialog (the row has its own "Unblock").
Finder dialogButton(String label) => find.descendant(
      of: find.byType(AlertDialog),
      matching: find.text(label),
    );

void main() {
  group('BlockedIpsScreen', () {
    testWidgets('renders one card per IP with counts and expiry',
        (tester) async {
      final api = FakeSentinelApi()
        ..blockedIps = [
          blocked(
            id: 'b1',
            ip: '203.0.113.42',
            lastHitAt: DateTime.now().toUtc().subtract(const Duration(minutes: 5)),
          ),
          blocked(
            id: 'b2',
            ip: '198.51.100.7',
            denials: 5,
            hits: 0,
            expiresAt: DateTime.now().toUtc().add(const Duration(hours: 2)),
          ),
        ];
      await pumpScreen(tester, api);

      expect(api.calls, ['listBlockedIps']);
      expect(find.text('Blocked IPs'), findsOneWidget);
      expect(find.textContaining('Blocked after repeated admin denials'),
          findsOneWidget);
      expect(find.byType(Card), findsNWidgets(2));

      // First row: permanent block with a recent refused attempt.
      expect(find.text('203.0.113.42'), findsOneWidget);
      expect(find.text('3 denials'), findsOneWidget);
      expect(find.text('12 attempts refused since the block'), findsOneWidget);
      expect(find.text('until unblocked'), findsOneWidget);
      expect(find.text('5 min ago'), findsOneWidget);
      // Denial timestamps go through the shared formatter (local time).
      expect(
        find.text(formatDateTime(DateTime.utc(2026, 9, 14, 19, 40, 12))),
        findsNWidgets(2),
      );
      expect(find.byKey(const ValueKey('unblock_b1')), findsOneWidget);

      // Second row: block with a TTL and no hit yet.
      expect(find.text('198.51.100.7'), findsOneWidget);
      expect(find.text('5 denials'), findsOneWidget);
      expect(find.text('0 attempts refused since the block'), findsOneWidget);
      expect(find.textContaining('expires in'), findsOneWidget);
      expect(find.byKey(const ValueKey('unblock_b2')), findsOneWidget);

      // The reason shows as a tag on each card.
      expect(find.text('autoblock'), findsNWidgets(2));
    });

    testWidgets('empty state', (tester) async {
      final api = FakeSentinelApi();
      await pumpScreen(tester, api);

      expect(
        find.text('No blocked IPs. An IP is blocked after repeated denials.'),
        findsOneWidget,
      );
      expect(find.byType(Card), findsNothing);
      expect(find.byType(RefreshIndicator), findsOneWidget);
    });

    testWidgets('error then retry', (tester) async {
      final api = FakeSentinelApi()
        ..failWith = const NetworkException('connection refused');
      await pumpScreen(tester, api);

      expect(find.textContaining('Backend unreachable'), findsOneWidget);
      expect(find.byType(Card), findsNothing);

      api
        ..failWith = null
        ..blockedIps = [blocked(id: 'b1', ip: '203.0.113.42')];
      await tester.tap(find.text('Retry'));
      await tester.pumpAndSettle();

      expect(api.calls, ['listBlockedIps', 'listBlockedIps']);
      expect(find.textContaining('Backend unreachable'), findsNothing);
      expect(find.text('203.0.113.42'), findsOneWidget);
    });

    testWidgets('refresh button reloads the list', (tester) async {
      final api = FakeSentinelApi()
        ..blockedIps = [blocked(id: 'b1', ip: '203.0.113.42')];
      await pumpScreen(tester, api);

      api.blockedIps.add(blocked(id: 'b2', ip: '198.51.100.7'));
      await tester.tap(find.byIcon(Icons.refresh));
      await tester.pumpAndSettle();

      expect(api.calls, ['listBlockedIps', 'listBlockedIps']);
      expect(find.byType(Card), findsNWidgets(2));
      expect(find.text('198.51.100.7'), findsOneWidget);
    });

    testWidgets('refresh failure keeps the list and shows the error',
        (tester) async {
      final api = FakeSentinelApi()
        ..blockedIps = [blocked(id: 'b1', ip: '203.0.113.42')];
      await pumpScreen(tester, api);

      api.failWith = const NetworkException('timeout');
      await tester.tap(find.byIcon(Icons.refresh));
      await tester.pumpAndSettle();

      expect(api.calls, ['listBlockedIps', 'listBlockedIps']);
      expect(find.text('203.0.113.42'), findsOneWidget);
      expect(find.textContaining('Backend unreachable'), findsOneWidget);
      expect(find.text('Retry'), findsNothing);
    });

    testWidgets('unblock: confirm calls the API and removes the row',
        (tester) async {
      final api = FakeSentinelApi()
        ..blockedIps = [
          blocked(id: 'b1', ip: '203.0.113.42'),
          blocked(id: 'b2', ip: '198.51.100.7'),
        ];
      await pumpScreen(tester, api);

      await tester.tap(find.byKey(const ValueKey('unblock_b1')));
      await tester.pumpAndSettle();

      expect(find.byType(AlertDialog), findsOneWidget);
      expect(find.text('Unblock 203.0.113.42?'), findsOneWidget);
      expect(
        find.textContaining('The next login from 203.0.113.42 will be pushed'),
        findsOneWidget,
      );
      expect(api.calls, ['listBlockedIps'], reason: 'nothing sent yet');

      await tester.tap(dialogButton('Unblock'));
      await tester.pumpAndSettle();

      expect(api.calls, ['listBlockedIps', 'unblockIp']);
      expect(api.blockedIps.map((b) => b.id), ['b2']);
      expect(find.byType(AlertDialog), findsNothing);
      expect(find.text('203.0.113.42'), findsNothing);
      expect(find.byKey(const ValueKey('unblock_b1')), findsNothing);
      expect(find.text('198.51.100.7'), findsOneWidget);
      expect(find.text('203.0.113.42 unblocked'), findsOneWidget);
    });

    testWidgets('unblock: cancelling the dialog does not call the API',
        (tester) async {
      final api = FakeSentinelApi()
        ..blockedIps = [blocked(id: 'b1', ip: '203.0.113.42')];
      await pumpScreen(tester, api);

      await tester.tap(find.byKey(const ValueKey('unblock_b1')));
      await tester.pumpAndSettle();
      await tester.tap(dialogButton('Cancel'));
      await tester.pumpAndSettle();

      expect(api.calls, ['listBlockedIps']);
      expect(api.blockedIps.map((b) => b.id), ['b1']);
      expect(find.byType(AlertDialog), findsNothing);
      expect(find.text('203.0.113.42'), findsOneWidget);
      expect(find.byKey(const ValueKey('unblock_b1')), findsOneWidget);
    });

    testWidgets('unblock: 404 means already unblocked, list is refreshed',
        (tester) async {
      final api = FakeSentinelApi()
        ..blockedIps = [
          blocked(id: 'b1', ip: '203.0.113.42'),
          blocked(id: 'b2', ip: '198.51.100.7'),
        ];
      await pumpScreen(tester, api);

      // Another phone unblocked b1 in the meantime.
      api.blockedIps.removeWhere((b) => b.id == 'b1');

      await tester.tap(find.byKey(const ValueKey('unblock_b1')));
      await tester.pumpAndSettle();
      await tester.tap(dialogButton('Unblock'));
      await tester.pumpAndSettle();

      expect(api.calls, ['listBlockedIps', 'unblockIp', 'listBlockedIps']);
      expect(find.text('203.0.113.42'), findsNothing);
      expect(find.text('198.51.100.7'), findsOneWidget);
      expect(find.text('203.0.113.42 was already unblocked'), findsOneWidget);
    });

    testWidgets('unblock: other errors keep the row and show the message',
        (tester) async {
      final api = FakeSentinelApi()
        ..blockedIps = [blocked(id: 'b1', ip: '203.0.113.42')];
      await pumpScreen(tester, api);

      api.failWith = const UnauthorizedException('bad token', statusCode: 401);
      await tester.tap(find.byKey(const ValueKey('unblock_b1')));
      await tester.pumpAndSettle();
      await tester.tap(dialogButton('Unblock'));
      await tester.pumpAndSettle();

      expect(api.calls, ['listBlockedIps', 'unblockIp']);
      expect(find.text('203.0.113.42'), findsOneWidget);
      expect(find.textContaining('Check the admin token'), findsOneWidget);
    });
  });
}
