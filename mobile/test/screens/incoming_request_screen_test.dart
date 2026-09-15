// Widget tests of the incoming request screen against the fake API.
//
// The countdown runs on a periodic timer, so every test ends by pumping an
// empty widget (the screen is disposed, the timer cancelled) and avoids
// pumpAndSettle, which may never settle while the countdown runs.
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:ssh_sentinel/models/enums.dart';
import 'package:ssh_sentinel/models/incoming_request.dart';
import 'package:ssh_sentinel/models/verdict_result.dart';
import 'package:ssh_sentinel/screens/incoming_request/incoming_request_screen.dart';
import 'package:ssh_sentinel/services/api_exceptions.dart';
import 'package:ssh_sentinel/services/pending_requests.dart';

import '../fakes/fake_api.dart';

const requestId = '5f1c9b2e-2c3a-4f6a-9b1e-0d3a2b7c4e11';
const deviceId = '0c9d1e2f-3a4b-4c5d-8e6f-7a8b9c0d1e2f';

const denyKey = ValueKey('deny_button');
const approveKey = ValueKey('approve_button');
const alwaysKey = ValueKey('always_button');
const closeKey = ValueKey('close_button');

/// An SSH login from Paris, one minute left unless [expiresAt] says otherwise.
IncomingRequest sshRequest({DateTime? expiresAt, bool notice = false}) {
  final now = DateTime.now().toUtc();
  return IncomingRequest(
    id: requestId,
    notice: notice,
    context: RequestContext.ssh,
    server: 'web-01',
    username: 'deploy',
    sourceIp: '203.0.113.42',
    geoCountry: 'FR',
    geoCity: 'Paris',
    createdAt: now,
    expiresAt: expiresAt ?? now.add(const Duration(minutes: 1)),
  );
}

/// A sudo invocation: no source IP, no geo.
IncomingRequest sudoRequest({String? command = 'sudo systemctl restart nginx'}) {
  final now = DateTime.now().toUtc();
  return IncomingRequest(
    id: requestId,
    context: RequestContext.sudo,
    server: 'web-01',
    username: 'deploy',
    command: command,
    createdAt: now,
    expiresAt: now.add(const Duration(minutes: 1)),
  );
}

Future<void> pumpScreen(
  WidgetTester tester,
  IncomingRequest request, {
  required FakeSentinelApi api,
  required PendingRequests pending,
  bool openTtlPicker = false,
}) async {
  await tester.pumpWidget(MaterialApp(
    home: IncomingRequestScreen(
      request: request,
      api: api,
      pending: pending,
      deviceId: deviceId,
      openTtlPicker: openTtlPicker,
    ),
  ));
  await tester.pump();
}

/// Lets the fake answer and the screen rebuild.
Future<void> settle(WidgetTester tester) async {
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 300));
}

/// Disposes the screen so the countdown timer is cancelled.
Future<void> closeScreen(WidgetTester tester) =>
    tester.pumpWidget(const SizedBox());

bool isEnabled(WidgetTester tester, Key key) =>
    tester.widget<ButtonStyleButton>(find.byKey(key)).enabled;

void expectButtons(WidgetTester tester, {required bool enabled}) {
  for (final key in [denyKey, approveKey, alwaysKey]) {
    expect(find.byKey(key), findsOneWidget);
    expect(isEnabled(tester, key), enabled, reason: '$key enabled');
  }
}

void expectNoButtons() {
  for (final key in [denyKey, approveKey, alwaysKey]) {
    expect(find.byKey(key), findsNothing);
  }
}

/// A fake whose denial reaches the auto-block threshold.
class AutoBlockingApi extends FakeSentinelApi {
  @override
  Future<VerdictResult> sendVerdict({
    required String requestId,
    required Verdict verdict,
    int? ttlSeconds,
    String? deviceId,
  }) async {
    final result = await super.sendVerdict(
      requestId: requestId,
      verdict: verdict,
      ttlSeconds: ttlSeconds,
      deviceId: deviceId,
    );
    return VerdictResult(
      requestId: result.requestId,
      status: result.status,
      decidedAt: result.decidedAt,
      decidedByDevice: result.decidedByDevice,
      autoBlocked:
          const AutoBlocked(id: 'blk-1', ip: '203.0.113.42', denialCount: 3),
    );
  }
}

void main() {
  late FakeSentinelApi api;
  late PendingRequests pending;

  setUp(() {
    api = FakeSentinelApi();
    pending = PendingRequests();
  });

  group('details', () {
    testWidgets('sudo request shows the command, local source and sudo badge',
        (tester) async {
      await pumpScreen(tester, sudoRequest(), api: api, pending: pending);

      expect(find.text('sudo on web-01'), findsOneWidget);
      expect(find.text('sudo'), findsOneWidget);
      expect(find.text('ssh'), findsNothing);
      expect(find.text('deploy'), findsOneWidget);
      expect(find.text('local'), findsOneWidget);
      expect(find.text('unknown'), findsOneWidget);
      expect(find.text('sudo systemctl restart nginx'), findsOneWidget);
      expectButtons(tester, enabled: true);

      await closeScreen(tester);
    });

    testWidgets('sudo request without a command says so', (tester) async {
      await pumpScreen(tester, sudoRequest(command: null),
          api: api, pending: pending);

      expect(find.text('command not captured'), findsOneWidget);

      await closeScreen(tester);
    });

    testWidgets('ssh request shows the IP, the geo and the countdown',
        (tester) async {
      await pumpScreen(tester, sshRequest(), api: api, pending: pending);

      expect(find.text('SSH login on web-01'), findsOneWidget);
      expect(find.text('ssh'), findsOneWidget);
      expect(find.text('203.0.113.42'), findsOneWidget);
      expect(find.text('Paris, FR'), findsOneWidget);
      expect(find.textContaining('s left'), findsOneWidget);
      expect(find.text('Command'), findsNothing);
      expectButtons(tester, enabled: true);

      await closeScreen(tester);
    });

    testWidgets('notice request has no buttons', (tester) async {
      await pumpScreen(tester, sshRequest(notice: true),
          api: api, pending: pending);

      expect(find.text('Notify only, nothing to decide'), findsOneWidget);
      expect(find.text('203.0.113.42'), findsOneWidget);
      expectNoButtons();

      await closeScreen(tester);
    });
  });

  group('verdicts', () {
    testWidgets('Approve sends approve with the device id', (tester) async {
      final request = sshRequest();
      pending.add(request);
      await pumpScreen(tester, request, api: api, pending: pending);

      await tester.tap(find.byKey(approveKey));
      await settle(tester);

      expect(api.verdicts, hasLength(1));
      expect(api.verdicts.single.requestId, requestId);
      expect(api.verdicts.single.verdict, Verdict.approve);
      expect(api.verdicts.single.ttlSeconds, isNull);
      expect(api.verdicts.single.deviceId, deviceId);
      expect(find.text('Approved by you'), findsOneWidget);
      expect(find.byKey(closeKey), findsOneWidget);
      expectNoButtons();
      expect(pending.outcome(requestId)?.mine, isTrue);
      expect(pending.outcome(requestId)?.status, RequestStatus.approved);

      await closeScreen(tester);
    });

    testWidgets('Deny sends deny', (tester) async {
      await pumpScreen(tester, sshRequest(), api: api, pending: pending);

      await tester.tap(find.byKey(denyKey));
      await settle(tester);

      expect(api.verdicts.single.verdict, Verdict.deny);
      expect(api.verdicts.single.deviceId, deviceId);
      expect(find.text('Denied by you'), findsOneWidget);
      expectNoButtons();

      await closeScreen(tester);
    });

    testWidgets('a denial that reaches the auto-block threshold says so',
        (tester) async {
      final blocking = AutoBlockingApi();
      await pumpScreen(tester, sshRequest(), api: blocking, pending: pending);

      await tester.tap(find.byKey(denyKey));
      await settle(tester);

      expect(find.text('Denied by you'), findsOneWidget);
      expect(find.text('203.0.113.42 is now blocked after 3 denials'),
          findsOneWidget);

      await closeScreen(tester);
    });

    testWidgets('Always allow opens the picker, 1 hour sends 3600 s',
        (tester) async {
      await pumpScreen(tester, sshRequest(), api: api, pending: pending);

      await tester.tap(find.byKey(alwaysKey));
      await settle(tester);
      expect(find.byKey(const ValueKey('ttl_1h')), findsOneWidget);
      expect(api.verdicts, isEmpty);

      await tester.tap(find.byKey(const ValueKey('ttl_1h')));
      await settle(tester);

      expect(api.verdicts.single.verdict, Verdict.approveAlways);
      expect(api.verdicts.single.ttlSeconds, 3600);
      expect(api.verdicts.single.deviceId, deviceId);
      expect(find.text('Approved by you'), findsOneWidget);
      expect(find.textContaining('whitelisted for ssh'), findsOneWidget);
      expect(find.textContaining('expires 20'), findsOneWidget);
      expectNoButtons();

      await closeScreen(tester);
    });

    testWidgets('permanent sends a null TTL and says permanently',
        (tester) async {
      await pumpScreen(tester, sshRequest(), api: api, pending: pending);

      await tester.tap(find.byKey(alwaysKey));
      await settle(tester);
      await tester.tap(find.byKey(const ValueKey('ttl_permanent')));
      await settle(tester);

      expect(api.verdicts.single.verdict, Verdict.approveAlways);
      expect(api.verdicts.single.ttlSeconds, isNull);
      expect(find.textContaining('permanently'), findsOneWidget);

      await closeScreen(tester);
    });

    testWidgets('dismissing the picker sends nothing', (tester) async {
      await pumpScreen(tester, sshRequest(), api: api, pending: pending);

      await tester.tap(find.byKey(alwaysKey));
      await settle(tester);
      // Tap the barrier above the sheet.
      await tester.tapAt(const Offset(20, 20));
      await settle(tester);

      expect(api.verdicts, isEmpty);
      expect(find.byKey(const ValueKey('ttl_1h')), findsNothing);
      expectButtons(tester, enabled: true);

      await closeScreen(tester);
    });

    testWidgets('openTtlPicker opens the picker after the first frame',
        (tester) async {
      await pumpScreen(tester, sshRequest(),
          api: api, pending: pending, openTtlPicker: true);
      await settle(tester);

      expect(find.byKey(const ValueKey('ttl_24h')), findsOneWidget);
      await tester.tap(find.byKey(const ValueKey('ttl_24h')));
      await settle(tester);

      expect(api.verdicts.single.verdict, Verdict.approveAlways);
      expect(api.verdicts.single.ttlSeconds, 86400);

      await closeScreen(tester);
    });

    testWidgets('buttons are disabled and a progress bar shows while sending',
        (tester) async {
      api.delay = const Duration(seconds: 1);
      await pumpScreen(tester, sshRequest(), api: api, pending: pending);

      await tester.tap(find.byKey(approveKey));
      await tester.pump();

      expectButtons(tester, enabled: false);
      expect(find.byKey(const ValueKey('sending_indicator')), findsOneWidget);

      await tester.pump(const Duration(seconds: 2));
      await settle(tester);

      expect(find.text('Approved by you'), findsOneWidget);
      expect(find.byKey(const ValueKey('sending_indicator')), findsNothing);

      await closeScreen(tester);
    });

    testWidgets('Close pops the route', (tester) async {
      await tester.pumpWidget(MaterialApp(
        home: Builder(
          builder: (context) => Scaffold(
            body: TextButton(
              onPressed: () => Navigator.of(context).push(
                MaterialPageRoute<void>(
                  builder: (_) => IncomingRequestScreen(
                    request: sshRequest(),
                    api: api,
                    pending: pending,
                  ),
                ),
              ),
              child: const Text('open'),
            ),
          ),
        ),
      ));
      await tester.tap(find.text('open'));
      await settle(tester);
      expect(find.byKey(approveKey), findsOneWidget);

      await tester.tap(find.byKey(approveKey));
      await settle(tester);
      await tester.tap(find.byKey(closeKey));
      // The page transition takes a few hundred milliseconds.
      await tester.pump();
      await tester.pump(const Duration(seconds: 1));

      expect(find.byKey(closeKey), findsNothing);
      expect(find.text('open'), findsOneWidget);

      await closeScreen(tester);
    });
  });

  group('already decided', () {
    testWidgets('409 shows who decided and removes the buttons',
        (tester) async {
      final request = sshRequest();
      pending.add(request);
      api.verdictFailWith = const AlreadyDecidedException(AlreadyDecided(
        status: RequestStatus.approved,
        decidedByDevice: 'Pixel 8',
      ));
      await pumpScreen(tester, request, api: api, pending: pending);

      await tester.tap(find.byKey(approveKey));
      await settle(tester);

      expect(api.verdicts, hasLength(1));
      expect(find.textContaining('already decided by Pixel 8'), findsOneWidget);
      expect(find.text('Approved by Pixel 8'), findsOneWidget);
      expect(find.byKey(closeKey), findsOneWidget);
      expectNoButtons();
      expect(pending.outcome(requestId)?.decidedByDevice, 'Pixel 8');
      expect(pending.outcome(requestId)?.mine, isFalse);

      await closeScreen(tester);
    });

    testWidgets('409 timeout says the request expired', (tester) async {
      api.verdictFailWith = const AlreadyDecidedException(
          AlreadyDecided(status: RequestStatus.timeout));
      await pumpScreen(tester, sshRequest(), api: api, pending: pending);

      await tester.tap(find.byKey(denyKey));
      await settle(tester);

      expect(find.text('Expired before anyone answered'), findsOneWidget);
      expectNoButtons();

      await closeScreen(tester);
    });

    testWidgets('an outcome recorded from outside disables the buttons',
        (tester) async {
      final request = sshRequest();
      pending.add(request);
      await pumpScreen(tester, request, api: api, pending: pending);
      expectButtons(tester, enabled: true);

      pending.markDecided(
        requestId,
        const DecisionOutcome(
            status: RequestStatus.denied, decidedByDevice: 'Pixel 8'),
      );
      await tester.pump();

      expect(find.text('Denied by Pixel 8'), findsOneWidget);
      expectNoButtons();
      expect(api.verdicts, isEmpty);

      await closeScreen(tester);
    });

    testWidgets('an outcome already known when the screen opens is shown',
        (tester) async {
      final request = sshRequest();
      pending.add(request);
      pending.markDecided(
        requestId,
        const DecisionOutcome(
            status: RequestStatus.approved, decidedByDevice: 'Pixel 8'),
      );
      await pumpScreen(tester, request, api: api, pending: pending);

      expect(find.text('Approved by Pixel 8'), findsOneWidget);
      expectNoButtons();

      await closeScreen(tester);
    });
  });

  group('expiry', () {
    testWidgets('an already expired request disables the buttons',
        (tester) async {
      final request = sshRequest(
          expiresAt: DateTime.now().toUtc().subtract(const Duration(minutes: 1)));
      await pumpScreen(tester, request, api: api, pending: pending);
      await tester.pump();

      expect(find.text('Expired, nobody answered in time'), findsOneWidget);
      expectButtons(tester, enabled: false);

      await closeScreen(tester);
    });

    testWidgets('the countdown reaching zero disables the buttons',
        (tester) async {
      final request = sshRequest(
          expiresAt: DateTime.now().toUtc().add(const Duration(seconds: 2)));
      await pumpScreen(tester, request, api: api, pending: pending);
      expectButtons(tester, enabled: true);
      expect(find.text('Expired, nobody answered in time'), findsNothing);

      // The countdown reads the wall clock, which pump() does not advance:
      // let real time pass, then let the countdown timer tick.
      await tester.runAsync(
          () => Future<void>.delayed(const Duration(milliseconds: 2500)));
      await tester.pump(const Duration(milliseconds: 300));
      await tester.pump();

      expect(find.text('Expired, nobody answered in time'), findsOneWidget);
      expectButtons(tester, enabled: false);
      expect(api.verdicts, isEmpty);

      await closeScreen(tester);
    });
  });

  group('errors', () {
    testWidgets('a network error shows a message and keeps the buttons',
        (tester) async {
      api.verdictFailWith = const NetworkException('connection refused');
      await pumpScreen(tester, sshRequest(), api: api, pending: pending);

      await tester.tap(find.byKey(approveKey));
      await settle(tester);

      expect(find.textContaining('Backend unreachable'), findsOneWidget);
      expect(find.textContaining('connection refused'), findsOneWidget);
      expectButtons(tester, enabled: true);
      expect(pending.outcome(requestId), isNull);

      // A retry goes through once the backend answers.
      api.verdictFailWith = null;
      await tester.tap(find.byKey(approveKey));
      await settle(tester);

      expect(api.verdicts, hasLength(2));
      expect(find.text('Approved by you'), findsOneWidget);

      await closeScreen(tester);
    });
  });
}
