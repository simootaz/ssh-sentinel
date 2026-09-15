// Contract v1 models, checked against the examples of docs/architecture.md.
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:ssh_sentinel/models/access_request.dart';
import 'package:ssh_sentinel/models/enums.dart';
import 'package:ssh_sentinel/models/verdict_result.dart';

Map<String, dynamic> j(String source) => jsonDecode(source) as Map<String, dynamic>;

void main() {
  group('GET /history', () {
    test('parses a sudo row with a null source IP', () {
      final page = HistoryPage.fromJson(j('''
{"items": [{"id": "5f1c9b2e-2c3a-4f6a-9b1e-0d3a2b7c4e11", "server": "web-01",
  "hostname": "web-01", "context": "sudo", "username": "deploy", "source_ip": null,
  "tty": "pts/0", "command": "sudo systemctl restart nginx", "geo": null,
  "status": "approved", "decided_by": "admin", "decided_by_device": "Pixel 8",
  "created_at": "2026-09-14T20:11:40Z", "expires_at": "2026-09-14T20:12:10Z",
  "decided_at": "2026-09-14T20:12:07Z"}],
 "next_before": "2026-09-14T20:11:40Z"}'''));
      expect(page.items, hasLength(1));
      expect(page.nextBefore, '2026-09-14T20:11:40Z');
      final r = page.items.single;
      expect(r.context, RequestContext.sudo);
      expect(r.isSudo, isTrue);
      expect(r.sourceIp, isNull);
      expect(r.sourceLabel, 'local');
      expect(r.command, 'sudo systemctl restart nginx');
      expect(r.status, RequestStatus.approved);
      expect(r.decidedBy, DecidedBy.admin);
      expect(r.decidedByDevice, 'Pixel 8');
      expect(r.createdAt, DateTime.utc(2026, 9, 14, 20, 11, 40));
      expect(r.decidedAt, DateTime.utc(2026, 9, 14, 20, 12, 7));
    });

    test('parses geo and unknown values without crashing', () {
      final r = AccessRequest.fromJson(j('''
{"id": "x", "server": "web-01", "hostname": "web-01", "context": "ssh",
 "username": "deploy", "source_ip": "203.0.113.42", "geo": {"country": "FR", "city": "Paris"},
 "status": "something_new", "decided_by": "robot", "created_at": "2026-09-14T20:11:40Z"}'''));
      expect(r.geo?.label, 'Paris, FR');
      expect(r.geo?.asn, isNull);
      expect(r.status, RequestStatus.unknown);
      expect(r.decidedBy, DecidedBy.unknown);
      expect(r.sourceLabel, '203.0.113.42');
    });

    test('last page has no next_before', () {
      expect(HistoryPage.fromJson(j('{"items": []}')).hasMore, isFalse);
    });
  });

  group('POST /verdict', () {
    test('parses the 200 body with a whitelist entry', () {
      final v = VerdictResult.fromJson(j('''
{"request_id": "5f1c9b2e-2c3a-4f6a-9b1e-0d3a2b7c4e11", "status": "approved",
 "decided_at": "2026-09-14T20:12:07Z", "decided_by_device": "Pixel 8",
 "whitelist_entry": {"id": "9a7d3e10-6b2f-4c55-8e0a-1f2b3c4d5e6f", "username": "deploy",
   "context": "ssh", "server": "web-01", "expires_at": "2026-09-14T21:12:07Z"},
 "auto_blocked": null}'''));
      expect(v.status, RequestStatus.approved);
      expect(v.whitelistEntry?.server, 'web-01');
      expect(v.whitelistEntry?.expiresAt, DateTime.utc(2026, 9, 14, 21, 12, 7));
      expect(v.autoBlocked, isNull);
    });

    test('parses auto_blocked', () {
      final v = VerdictResult.fromJson(j('''
{"request_id": "x", "status": "denied", "decided_at": "2026-09-14T20:12:07Z",
 "auto_blocked": {"id": "2b3c", "ip": "203.0.113.42", "denial_count": 3}}'''));
      expect(v.autoBlocked?.ip, '203.0.113.42');
      expect(v.autoBlocked?.denialCount, 3);
    });

    test('parses the 409 body, decided by another phone or by the timeout', () {
      final other = AlreadyDecided.fromJson(j('''
{"error": "request already decided", "status": "approved",
 "decided_at": "2026-09-14T20:12:07Z", "decided_by_device": "Pixel 8"}'''));
      expect(other.decidedByDevice, 'Pixel 8');
      expect(other.isTimeout, isFalse);
      expect(other.description, 'already decided by Pixel 8');

      final timeout = AlreadyDecided.fromJson(
          j('{"error": "request already decided", "status": "timeout"}'));
      expect(timeout.isTimeout, isTrue);
      expect(timeout.description, 'expired before anyone answered');
    });
  });
}
