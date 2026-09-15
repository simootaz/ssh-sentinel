// Push payloads, list models and settings, against docs/architecture.md.
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:ssh_sentinel/models/blocked_ip.dart';
import 'package:ssh_sentinel/models/device.dart';
import 'package:ssh_sentinel/models/enums.dart';
import 'package:ssh_sentinel/models/geo_rule.dart';
import 'package:ssh_sentinel/models/incoming_request.dart';
import 'package:ssh_sentinel/models/push_message.dart';
import 'package:ssh_sentinel/models/whitelist_entry.dart';
import 'package:ssh_sentinel/services/settings_store.dart';

Map<String, dynamic> j(String source) => jsonDecode(source) as Map<String, dynamic>;

const sudoPush = {
  'type': 'access_request',
  'request_id': '5f1c9b2e-2c3a-4f6a-9b1e-0d3a2b7c4e11',
  'context': 'sudo',
  'server': 'web-01',
  'username': 'deploy',
  'source_ip': '',
  'geo_country': '',
  'geo_city': '',
  'command': 'sudo systemctl restart nginx',
  'created_at': '2026-09-14T20:11:40Z',
  'expires_at': '2026-09-14T20:12:10Z',
};

void main() {
  group('push messages', () {
    test('access_request with empty strings for unknown values', () {
      final message = PushMessage.parse(Map.of(sudoPush));
      expect(message, isA<AccessRequestPush>());
      final r = (message! as AccessRequestPush).request;
      expect(r.notice, isFalse);
      expect(r.isSudo, isTrue);
      expect(r.sourceIp, isNull);
      expect(r.sourceLabel, 'local');
      expect(r.geoLabel, isNull);
      expect(r.title, 'sudo on web-01');
      expect(r.expiresAt, DateTime.utc(2026, 9, 14, 20, 12, 10));
      final before = DateTime.utc(2026, 9, 14, 20, 12, 0);
      expect(r.isExpired(before), isFalse);
      expect(r.remaining(before), const Duration(seconds: 10));
      expect(r.isExpired(DateTime.utc(2026, 9, 14, 20, 12, 11)), isTrue);
    });

    test('access_notice has no buttons, geo is joined', () {
      final r = IncomingRequest.fromPushData({
        ...sudoPush,
        'type': 'access_notice',
        'context': 'ssh',
        'server': 'win-01',
        'source_ip': '203.0.113.42',
        'geo_country': 'FR',
        'geo_city': 'Paris',
        'command': '',
      });
      expect(r.notice, isTrue);
      expect(r.geoLabel, 'Paris, FR');
      expect(r.title, 'SSH login on win-01');
      expect(r.command, isNull);
    });

    test('round trip through the notification payload', () {
      final original = IncomingRequest.fromPushData(
          {...sudoPush, 'context': 'ssh', 'source_ip': '203.0.113.42', 'geo_country': 'FR'});
      final copy = IncomingRequest.fromPushData(
          jsonDecode(jsonEncode(original.toPushData())) as Map<String, dynamic>);
      expect(copy.id, original.id);
      expect(copy.sourceIp, '203.0.113.42');
      expect(copy.geoCountry, 'FR');
      expect(copy.geoCity, isNull);
      expect(copy.expiresAt, original.expiresAt);
    });

    test('request_decided', () {
      final message = PushMessage.parse({
        'type': 'request_decided',
        'request_id': 'id',
        'status': 'approved',
        'decided_by_device': 'Pixel 8',
      });
      final d = (message! as RequestDecidedPush).decision;
      expect(d.status, RequestStatus.approved);
      expect(d.decidedByDevice, 'Pixel 8');
    });

    test('unknown type and missing id are ignored', () {
      expect(PushMessage.parse({'type': 'something_else'}), isNull);
      expect(PushMessage.parse({'type': 'access_request'}), isNull);
      expect(PushMessage.parse({}), isNull);
    });
  });

  group('lists', () {
    test('whitelist entry, global and permanent', () {
      final e = WhitelistEntry.fromJson(j('''
{"id": "c1d2", "username": "ansible", "context": "ssh", "server": null,
 "expires_at": null, "created_at": "2026-09-10T08:00:00Z",
 "created_from_request": null, "created_by_device": null}'''));
      expect(e.isGlobal, isTrue);
      expect(e.isPermanent, isTrue);
      expect(e.serverLabel, 'every server');
    });

    test('device', () {
      final d = Device.fromJson(j('''
{"id": "0c9d", "platform": "android", "label": "Pixel 8",
 "created_at": "2026-09-01T09:00:00Z", "last_seen_at": "2026-09-14T20:00:00Z"}'''));
      expect(d.displayLabel, 'Pixel 8');
      expect(d.lastSeenAt, DateTime.utc(2026, 9, 14, 20));
    });

    test('blocked ip', () {
      final b = BlockedIp.fromJson(j('''
{"id": "2b3c", "ip": "203.0.113.42", "reason": "autoblock", "denial_count": 3,
 "first_denied_at": "2026-09-14T19:40:12Z", "last_denied_at": "2026-09-14T20:12:07Z",
 "hit_count": 12, "last_hit_at": "2026-09-14T20:30:01Z",
 "created_at": "2026-09-14T20:12:07Z", "expires_at": null}'''));
      expect(b.denialCount, 3);
      expect(b.hitCount, 12);
      expect(b.expiresAt, isNull);
    });

    test('geo rule is upper-cased', () {
      final g = GeoRule.fromJson(j(
          '{"id": "7e8f", "country": "kp", "note": "no staff there", "created_at": "2026-09-14T18:00:00Z"}'));
      expect(g.country, 'KP');
      expect(g.note, 'no staff there');
    });
  });

  group('settings', () {
    test('base URL is trimmed of trailing slashes and validated', () {
      expect(SettingsStore.normalizeBaseUrl(' https://api.example.com/ '), 'https://api.example.com');
      expect(SettingsStore.validateBaseUrl('https://api.example.com'), isNull);
      expect(SettingsStore.validateBaseUrl('api.example.com'), isNotNull);
      expect(SettingsStore.validateBaseUrl(''), isNotNull);
    });

    test('config is null until both values are set', () async {
      final store = SettingsStore(store: MemoryKeyValueStore());
      expect(await store.config(), isNull);
      await store.setBackend(baseUrl: 'https://api.example.com/', adminToken: ' tok ');
      final config = await store.config();
      expect(config?.baseUrl, 'https://api.example.com');
      expect(config?.adminToken, 'tok');
    });
  });
}
