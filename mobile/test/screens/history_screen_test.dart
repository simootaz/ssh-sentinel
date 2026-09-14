// History screen against the fake API: rows, states, filters, paging, details.
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:ssh_sentinel/models/access_request.dart';
import 'package:ssh_sentinel/models/enums.dart';
import 'package:ssh_sentinel/models/geo.dart';
import 'package:ssh_sentinel/screens/history/history_screen.dart';
import 'package:ssh_sentinel/services/api_exceptions.dart';
import 'package:ssh_sentinel/widgets/format.dart';

import '../fakes/fake_api.dart';

const sshId = '5f1c9b2e-2c3a-4f6a-9b1e-0d3a2b7c4e11';
const sudoId = '7b2e4d10-9c1f-4a8b-b3e6-2d4f6a8c0e22';
final base = DateTime.utc(2026, 9, 14, 20, 11, 40);

/// A request with sensible defaults, so each test only spells out what it
/// cares about.
AccessRequest request({
  required String id,
  required DateTime createdAt,
  RequestContext context = RequestContext.ssh,
  String server = 'web-01',
  String username = 'deploy',
  String? sourceIp,
  String? tty,
  String? command,
  Geo? geo,
  RequestStatus status = RequestStatus.approved,
  DecidedBy? decidedBy = DecidedBy.admin,
  String? decidedByDevice = 'Pixel 8',
}) =>
    AccessRequest(
      id: id,
      server: server,
      hostname: server,
      context: context,
      username: username,
      sourceIp: sourceIp,
      tty: tty,
      command: command,
      geo: geo,
      status: status,
      decidedBy: decidedBy,
      decidedByDevice: decidedByDevice,
      createdAt: createdAt,
      expiresAt: createdAt.add(const Duration(seconds: 30)),
      decidedAt: decidedBy == null ? null : createdAt.add(const Duration(seconds: 27)),
    );

/// An SSH login from Paris, approved from a phone.
AccessRequest sshApproved() => request(
      id: sshId,
      createdAt: base,
      username: 'alice',
      sourceIp: '203.0.113.42',
      tty: 'ssh',
      geo: const Geo(country: 'FR', city: 'Paris'),
    );

/// A sudo command on another server, denied from a phone. No source IP.
AccessRequest sudoDenied() => request(
      id: sudoId,
      createdAt: base.subtract(const Duration(minutes: 1)),
      context: RequestContext.sudo,
      server: 'db-01',
      username: 'deploy',
      tty: 'pts/0',
      command: 'sudo systemctl restart nginx',
      status: RequestStatus.denied,
    );

void main() {
  late FakeSentinelApi api;

  setUp(() {
    api = FakeSentinelApi();
  });

  Future<void> pumpScreen(WidgetTester tester) async {
    await tester.pumpWidget(MaterialApp(home: HistoryScreen(api: api)));
    await tester.pumpAndSettle();
  }

  Future<void> openFilters(WidgetTester tester) async {
    await tester.tap(find.byKey(const ValueKey('filter_toggle')));
    await tester.pumpAndSettle();
  }

  Finder badgeCount(String count) => find.descendant(
        of: find.byKey(const ValueKey('filter_count')),
        matching: find.text(count),
      );

  group('HistoryScreen', () {
    testWidgets('renders ssh and sudo rows', (tester) async {
      api.requests = [sshApproved(), sudoDenied()];
      await pumpScreen(tester);

      expect(find.text('History'), findsOneWidget);
      expect(api.calls, ['history']);

      // ssh row: IP with geo, and who approved.
      expect(find.text('ssh'), findsOneWidget);
      expect(find.text('alice on web-01'), findsOneWidget);
      expect(find.text('203.0.113.42 · Paris, FR'), findsOneWidget);
      expect(find.text('approved by Pixel 8'), findsOneWidget);
      expect(find.text(formatDateTime(base)), findsOneWidget);

      // sudo row: no IP so "local", the command, and who denied.
      expect(find.text('sudo'), findsOneWidget);
      expect(find.text('deploy on db-01'), findsOneWidget);
      expect(find.text('local'), findsOneWidget);
      expect(find.text('sudo systemctl restart nginx'), findsOneWidget);
      expect(find.text('denied by Pixel 8'), findsOneWidget);

      // No paging control when the fake has nothing more.
      expect(find.byKey(const ValueKey('load_more')), findsNothing);
    });

    testWidgets('shows the empty state', (tester) async {
      await pumpScreen(tester);

      expect(find.text('No requests yet'), findsOneWidget);
      expect(find.byType(ListTile), findsNothing);
    });

    testWidgets('shows the error and retries', (tester) async {
      api.requests = [sshApproved()];
      api.failWith = const NetworkException('connection refused');
      await pumpScreen(tester);

      expect(find.text('Backend unreachable: connection refused'), findsOneWidget);
      expect(find.text('alice on web-01'), findsNothing);

      api.failWith = null;
      await tester.tap(find.text('Retry'));
      await tester.pumpAndSettle();

      expect(api.calls, ['history', 'history']);
      expect(find.text('Backend unreachable: connection refused'), findsNothing);
      expect(find.text('alice on web-01'), findsOneWidget);
    });

    testWidgets('context filter keeps only sudo rows and can be cleared',
        (tester) async {
      api.requests = [sshApproved(), sudoDenied()];
      await pumpScreen(tester);
      expect(badgeCount('1'), findsNothing);

      await openFilters(tester);
      await tester.tap(find.descendant(
        of: find.byKey(const ValueKey('context_filter')),
        matching: find.text('sudo'),
      ));
      await tester.pumpAndSettle();

      // A second call went out with the filter, and only the sudo row is left.
      expect(api.calls, ['history', 'history']);
      expect(find.text('deploy on db-01'), findsOneWidget);
      expect(find.text('alice on web-01'), findsNothing);
      expect(badgeCount('1'), findsOneWidget);

      await tester.tap(find.byKey(const ValueKey('clear_filters')));
      await tester.pumpAndSettle();

      expect(api.calls, ['history', 'history', 'history']);
      expect(find.text('deploy on db-01'), findsOneWidget);
      expect(find.text('alice on web-01'), findsOneWidget);
      expect(badgeCount('1'), findsNothing);
    });

    testWidgets('status filter keeps only matching rows', (tester) async {
      api.requests = [sshApproved(), sudoDenied()];
      await pumpScreen(tester);

      await openFilters(tester);
      await tester.tap(find.byKey(const ValueKey('status_filter')));
      await tester.pumpAndSettle();
      // The menu is drawn last, on top of the closed button.
      await tester.tap(find.text('denied').last);
      await tester.pumpAndSettle();

      expect(api.calls, ['history', 'history']);
      expect(find.text('deploy on db-01'), findsOneWidget);
      expect(find.text('alice on web-01'), findsNothing);
    });

    testWidgets('server and username filters apply on Apply', (tester) async {
      api.requests = [sshApproved(), sudoDenied()];
      await pumpScreen(tester);

      await openFilters(tester);
      await tester.enterText(find.byKey(const ValueKey('server_filter')), 'web-01');
      await tester.enterText(find.byKey(const ValueKey('username_filter')), 'alice');
      // Typing alone does not reload.
      expect(api.calls, ['history']);

      await tester.tap(find.byKey(const ValueKey('apply_filters')));
      await tester.pumpAndSettle();

      expect(api.calls, ['history', 'history']);
      expect(find.text('alice on web-01'), findsOneWidget);
      expect(find.text('deploy on db-01'), findsNothing);
      expect(badgeCount('2'), findsOneWidget);
    });

    testWidgets('load more appends the next page', (tester) async {
      // 60 requests, one minute apart: the fake pages 50 then 10.
      api.requests = [
        for (var i = 0; i < 60; i++)
          request(
            id: 'req-$i',
            createdAt: base.subtract(Duration(minutes: i)),
            username: 'user$i',
            sourceIp: '203.0.113.$i',
          ),
      ];
      await pumpScreen(tester);
      expect(api.calls, ['history']);

      final scrollable = find.byType(Scrollable).first;
      final loadMore = find.byKey(const ValueKey('load_more'));
      await tester.scrollUntilVisible(loadMore, 400, scrollable: scrollable);
      await tester.pumpAndSettle();
      await tester.tap(loadMore);
      await tester.pumpAndSettle();

      expect(api.calls, ['history', 'history']);
      // Last page: the button is gone and the oldest request is reachable.
      expect(loadMore, findsNothing);
      await tester.scrollUntilVisible(
        find.text('user59 on web-01'),
        400,
        scrollable: scrollable,
      );
      expect(find.text('user59 on web-01'), findsOneWidget);
    });

    testWidgets('tapping a row opens the details', (tester) async {
      api.requests = [sshApproved(), sudoDenied()];
      await pumpScreen(tester);

      await tester.tap(find.text('deploy on db-01'));
      await tester.pumpAndSettle();

      expect(find.text('Request details'), findsOneWidget);
      expect(find.text(sudoId), findsOneWidget);
      expect(find.text('pts/0'), findsOneWidget);
      expect(find.text('db-01'), findsNWidgets(2)); // server and hostname
      expect(find.text('admin'), findsOneWidget);
      expect(find.text(formatDateTime(base.subtract(const Duration(minutes: 1)))),
          findsNWidgets(2)); // row time and the "created" line
    });
  });
}
