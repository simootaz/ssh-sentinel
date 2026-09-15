// Pending request store and device registration.
import 'package:flutter_test/flutter_test.dart';
import 'package:ssh_sentinel/models/enums.dart';
import 'package:ssh_sentinel/models/incoming_request.dart';
import 'package:ssh_sentinel/services/device_registration.dart';
import 'package:ssh_sentinel/services/pending_requests.dart';
import 'package:ssh_sentinel/services/settings_store.dart';

import 'fakes/fake_api.dart';

IncomingRequest request(String id, {bool expired = false}) {
  final now = DateTime.now().toUtc();
  return IncomingRequest(
    id: id,
    context: RequestContext.ssh,
    server: 'web-01',
    username: 'deploy',
    sourceIp: '203.0.113.42',
    createdAt: now,
    expiresAt: expired ? now.subtract(const Duration(seconds: 1)) : now.add(const Duration(seconds: 30)),
  );
}

void main() {
  group('PendingRequests', () {
    test('records outcomes and notifies listeners', () {
      final pending = PendingRequests();
      var notified = 0;
      pending.addListener(() => notified++);
      pending.add(request('a'));
      expect(pending.undecided, hasLength(1));
      pending.markDecided('a', const DecisionOutcome(status: RequestStatus.approved, decidedByDevice: 'Pixel 8'));
      expect(pending.outcome('a')?.description, 'Approved by Pixel 8');
      expect(pending.undecided, isEmpty);
      expect(notified, 2);
    });

    test('a request_decided that arrives before its request is kept', () {
      final pending = PendingRequests();
      pending.markDecided('b', const DecisionOutcome(status: RequestStatus.denied));
      final entry = pending.add(request('b'));
      expect(entry.isDecided, isTrue);
      expect(entry.outcome?.status, RequestStatus.denied);
    });

    test('our own verdict wins over a foreign outcome, not the reverse', () {
      final pending = PendingRequests();
      pending.add(request('c'));
      pending.markDecided('c', const DecisionOutcome(status: RequestStatus.approved, decidedByDevice: 'Other'));
      pending.markDecided('c', const DecisionOutcome(status: RequestStatus.approved, mine: true));
      expect(pending.outcome('c')?.mine, isTrue);
      pending.markDecided('c', const DecisionOutcome(status: RequestStatus.denied, decidedByDevice: 'Other'));
      expect(pending.outcome('c')?.mine, isTrue);
    });

    test('expired requests are not undecided, timeouts are described', () {
      final pending = PendingRequests();
      pending.add(request('d', expired: true));
      expect(pending.undecided, isEmpty);
      expect(const DecisionOutcome(status: RequestStatus.timeout).description,
          'Expired, nobody answered');
    });
  });

  group('DeviceRegistrar', () {
    late FakeSentinelApi api;
    late SettingsStore settings;
    String? token = 'tok-1';

    DeviceRegistrar registrar() => DeviceRegistrar(
          api: api,
          settings: settings,
          fcmTokenProvider: () async => token,
          defaultLabelProvider: () async => 'Google Pixel 8',
        );

    setUp(() {
      api = FakeSentinelApi();
      settings = SettingsStore(store: MemoryKeyValueStore());
      token = 'tok-1';
    });

    test('does nothing until the backend is configured', () async {
      expect(await registrar().register(), isNull);
      expect(api.calls, isEmpty);
    });

    test('registers once, stores the id and the default label', () async {
      await settings.setBackend(baseUrl: 'https://api.example.com', adminToken: 't');
      final device = await registrar().register();
      expect(device?.label, 'Google Pixel 8');
      expect(await settings.deviceId, device?.id);
      expect(await settings.deviceLabel, 'Google Pixel 8');
      expect(await settings.registeredFcmToken, 'tok-1');

      expect(await registrar().register(), isNull, reason: 'same token, already registered');
      expect(api.calls.where((c) => c == 'registerDevice'), hasLength(1));
    });

    test('re-registers when the token rotates or when forced', () async {
      await settings.setBackend(baseUrl: 'https://api.example.com', adminToken: 't');
      await registrar().register();
      token = 'tok-2';
      expect(await registrar().register(), isNotNull);
      expect(await settings.registeredFcmToken, 'tok-2');
      await registrar().onTokenRefresh('tok-3');
      expect(await settings.registeredFcmToken, 'tok-3');
      expect(await registrar().register(force: true), isNotNull);
      expect(api.calls.where((c) => c == 'registerDevice'), hasLength(4));
    });

    test('without a push token nothing is sent', () async {
      await settings.setBackend(baseUrl: 'https://api.example.com', adminToken: 't');
      token = null;
      expect(await registrar().register(), isNull);
      expect(api.calls, isEmpty);
    });

    test('a typed label is kept over the default', () async {
      await settings.setBackend(baseUrl: 'https://api.example.com', adminToken: 't');
      await settings.setDeviceLabel('Ops phone');
      final device = await registrar().register();
      expect(device?.label, 'Ops phone');
    });
  });
}
