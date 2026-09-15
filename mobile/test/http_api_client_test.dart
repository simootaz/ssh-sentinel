// The HTTP client against contract v1: headers, bodies, status mapping.
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:ssh_sentinel/models/enums.dart';
import 'package:ssh_sentinel/services/api_exceptions.dart';
import 'package:ssh_sentinel/services/http_api_client.dart';
import 'package:ssh_sentinel/services/settings_store.dart';

const config = BackendConfig(baseUrl: 'https://api.example.com', adminToken: 'secret');

HttpSentinelApi apiWith(MockClient client, {BackendConfig? cfg = config}) =>
    HttpSentinelApi(configProvider: () async => cfg, client: client);

http.Response json(int status, String body) =>
    http.Response(body, status, headers: {'content-type': 'application/json'});

void main() {
  test('POST /verdict sends the bearer token and the contract body', () async {
    http.Request? seen;
    final api = apiWith(MockClient((request) async {
      seen = request;
      return json(200, '{"request_id": "r1", "status": "approved", "decided_by_device": "Pixel 8"}');
    }));
    final result = await api.sendVerdict(
        requestId: 'r1', verdict: Verdict.approveAlways, ttlSeconds: 3600, deviceId: 'd1');
    expect(seen!.method, 'POST');
    expect(seen!.url.toString(), 'https://api.example.com/verdict');
    expect(seen!.headers['Authorization'], 'Bearer secret');
    expect(seen!.headers['Content-Type'], startsWith('application/json'));
    expect(jsonDecode(seen!.body), {
      'request_id': 'r1',
      'verdict': 'approve_always',
      'ttl_seconds': 3600,
      'device_id': 'd1',
    });
    expect(result.status, RequestStatus.approved);
    expect(result.decidedByDevice, 'Pixel 8');
  });

  test('ttl_seconds is only sent with approve_always', () async {
    late String body;
    final api = apiWith(MockClient((request) async {
      body = request.body;
      return json(200, '{"request_id": "r1", "status": "denied"}');
    }));
    await api.sendVerdict(requestId: 'r1', verdict: Verdict.deny, ttlSeconds: 99);
    expect(jsonDecode(body), {'request_id': 'r1', 'verdict': 'deny'});
  });

  test('409 on /verdict becomes AlreadyDecidedException with the winner', () async {
    final api = apiWith(MockClient((_) async => json(409,
        '{"error": "request already decided", "status": "approved", "decided_by_device": "Pixel 8"}')));
    try {
      await api.sendVerdict(requestId: 'r1', verdict: Verdict.approve);
      fail('expected AlreadyDecidedException');
    } on AlreadyDecidedException catch (e) {
      expect(e.decision.status, RequestStatus.approved);
      expect(e.decision.decidedByDevice, 'Pixel 8');
      expect(e.decision.isTimeout, isFalse);
    }
  });

  test('409 elsewhere is a plain ConflictException', () async {
    final api = apiWith(MockClient((_) async => json(409, '{"error": "country already has a rule"}')));
    await expectLater(api.addGeoRule(country: 'kp'),
        throwsA(isA<ConflictException>().having((e) => e.message, 'message', 'country already has a rule')));
  });

  test('401, 404 and 500 map to their exceptions', () async {
    Future<void> check(int status, Matcher matcher) async {
      final api = apiWith(MockClient((_) async => json(status, '{"error": "nope"}')));
      await expectLater(api.listDevices(), throwsA(matcher));
    }

    await check(401, isA<UnauthorizedException>());
    await check(403, isA<UnauthorizedException>());
    await check(404, isA<NotFoundException>());
    await check(500, isA<ApiException>().having((e) => e.statusCode, 'status', 500));
  });

  test('GET /history passes the filters as query parameters', () async {
    late Uri url;
    final api = apiWith(MockClient((request) async {
      url = request.url;
      return json(200, '{"items": [], "next_before": "2026-09-14T20:11:40Z"}');
    }));
    final page = await api.history(
        limit: 20, before: '2026-09-14T21:00:00Z', server: 'web-01',
        context: RequestContext.sudo, status: RequestStatus.blockedIp);
    expect(url.path, '/history');
    expect(url.queryParameters, {
      'limit': '20',
      'before': '2026-09-14T21:00:00Z',
      'server': 'web-01',
      'context': 'sudo',
      'status': 'blocked_ip',
    });
    expect(page.hasMore, isTrue);
  });

  test('DELETE with an empty 204 succeeds', () async {
    late http.Request seen;
    final api = apiWith(MockClient((request) async {
      seen = request;
      return http.Response('', 204);
    }));
    await api.deleteWhitelistEntry('abc');
    expect(seen.method, 'DELETE');
    expect(seen.url.path, '/whitelist/abc');
  });

  test('GET /healthz sends no Authorization header', () async {
    late http.Request seen;
    final api = apiWith(MockClient((request) async {
      seen = request;
      return json(200, '{"status": "ok", "db": "ok"}');
    }));
    expect((await api.health()).isOk, isTrue);
    expect(seen.headers.containsKey('Authorization'), isFalse);
  });

  test('missing configuration fails before any request', () async {
    var called = false;
    final api = apiWith(MockClient((_) async {
      called = true;
      return json(200, '{}');
    }), cfg: null);
    await expectLater(api.listWhitelist(), throwsA(isA<NotConfiguredException>()));
    expect(called, isFalse);
  });

  test('connection failures become NetworkException', () async {
    final api = apiWith(MockClient((_) async => throw http.ClientException('Connection refused')));
    await expectLater(api.listWhitelist(), throwsA(isA<NetworkException>()));
  });

  test('a non-JSON answer is reported, not thrown as a format error', () async {
    final api = apiWith(MockClient((_) async => http.Response('<html>502</html>', 502)));
    await expectLater(api.listWhitelist(),
        throwsA(isA<ApiException>().having((e) => e.message, 'message', contains('not JSON'))));
  });
}
