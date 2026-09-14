import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:http/http.dart' as http;

import '../models/access_request.dart';
import '../models/blocked_ip.dart';
import '../models/device.dart';
import '../models/enums.dart';
import '../models/geo_rule.dart';
import '../models/json_helpers.dart';
import '../models/verdict_result.dart';
import '../models/whitelist_entry.dart';
import 'api_client.dart';
import 'api_exceptions.dart';
import 'settings_store.dart';

/// [SentinelApi] over HTTPS with the `http` package.
///
/// The configuration is read on every call, so a change in Settings applies
/// at once and the background isolate can build a client from storage.
class HttpSentinelApi implements SentinelApi {
  HttpSentinelApi({
    required Future<BackendConfig?> Function() configProvider,
    http.Client? client,
    this.timeout = const Duration(seconds: 15),
  })  : _configProvider = configProvider,
        _client = client ?? http.Client();

  final Future<BackendConfig?> Function() _configProvider;
  final http.Client _client;
  final Duration timeout;

  @override
  Future<VerdictResult> sendVerdict({
    required String requestId,
    required Verdict verdict,
    int? ttlSeconds,
    String? deviceId,
  }) async {
    final body = await _send('POST', '/verdict', body: {
      'request_id': requestId,
      'verdict': verdict.wire,
      if (verdict == Verdict.approveAlways) 'ttl_seconds': ttlSeconds,
      if (deviceId != null) 'device_id': deviceId,
    });
    return VerdictResult.fromJson(body);
  }

  @override
  Future<HistoryPage> history({
    int? limit,
    String? before,
    String? server,
    String? username,
    RequestContext? context,
    RequestStatus? status,
  }) async {
    final body = await _send('GET', '/history', query: {
      if (limit != null) 'limit': '$limit',
      if (before != null) 'before': before,
      if (server != null && server.isNotEmpty) 'server': server,
      if (username != null && username.isNotEmpty) 'username': username,
      if (context != null) 'context': context.wire,
      if (status != null) 'status': status.wire,
    });
    return HistoryPage.fromJson(body);
  }

  @override
  Future<List<WhitelistEntry>> listWhitelist() async =>
      readItems(await _send('GET', '/whitelist')).map(WhitelistEntry.fromJson).toList();

  @override
  Future<WhitelistEntry> addWhitelistEntry({
    required String username,
    RequestContext context = RequestContext.ssh,
    String? server,
    int? ttlSeconds,
  }) async =>
      WhitelistEntry.fromJson(await _send('POST', '/whitelist', body: {
        'username': username,
        'context': context.wire,
        'server': server,
        'ttl_seconds': ttlSeconds,
      }));

  @override
  Future<void> deleteWhitelistEntry(String id) => _send('DELETE', '/whitelist/$id');

  @override
  Future<Device> registerDevice({
    required String fcmToken,
    String platform = 'android',
    String? label,
  }) async =>
      Device.fromJson(await _send('POST', '/devices', body: {
        'fcm_token': fcmToken,
        'platform': platform,
        'label': label,
      }));

  @override
  Future<List<Device>> listDevices() async =>
      readItems(await _send('GET', '/devices')).map(Device.fromJson).toList();

  @override
  Future<void> deleteDevice(String id) => _send('DELETE', '/devices/$id');

  @override
  Future<List<BlockedIp>> listBlockedIps() async =>
      readItems(await _send('GET', '/blocked-ips')).map(BlockedIp.fromJson).toList();

  @override
  Future<void> unblockIp(String id) => _send('DELETE', '/blocked-ips/$id');

  @override
  Future<List<GeoRule>> listGeoRules() async =>
      readItems(await _send('GET', '/geo-rules')).map(GeoRule.fromJson).toList();

  @override
  Future<GeoRule> addGeoRule({required String country, String? note}) async =>
      GeoRule.fromJson(await _send('POST', '/geo-rules', body: {
        'country': country.trim().toUpperCase(),
        if (note != null && note.trim().isNotEmpty) 'note': note.trim(),
      }));

  @override
  Future<void> deleteGeoRule(String id) => _send('DELETE', '/geo-rules/$id');

  @override
  Future<Health> health() async =>
      Health.fromJson(await _send('GET', '/healthz', auth: false));

  /// Sends one request and maps the answer to a JSON map or an exception.
  Future<Map<String, dynamic>> _send(
    String method,
    String path, {
    Map<String, dynamic>? body,
    Map<String, String>? query,
    bool auth = true,
  }) async {
    final config = await _configProvider();
    if (config == null) throw const NotConfiguredException();

    final uri = Uri.parse('${config.baseUrl}$path')
        .replace(queryParameters: query == null || query.isEmpty ? null : query);
    final request = http.Request(method, uri);
    request.headers['Accept'] = 'application/json';
    if (auth) request.headers['Authorization'] = 'Bearer ${config.adminToken}';
    if (body != null) {
      request.headers['Content-Type'] = 'application/json';
      request.body = jsonEncode(body);
    }

    http.Response response;
    try {
      final streamed = await _client.send(request).timeout(timeout);
      response = await http.Response.fromStream(streamed).timeout(timeout);
    } on TimeoutException {
      throw const NetworkException('no answer from the backend');
    } on SocketException catch (e) {
      throw NetworkException(e.osError?.message ?? e.message);
    } on HandshakeException catch (e) {
      throw NetworkException('TLS error: ${e.message}');
    } on http.ClientException catch (e) {
      throw NetworkException(e.message);
    }

    final json = _decode(response);
    final status = response.statusCode;
    if (status >= 200 && status < 300) return json;

    final message = json['error']?.toString() ?? 'HTTP $status';
    switch (status) {
      case 401:
      case 403:
        throw UnauthorizedException(message, statusCode: status);
      case 404:
        throw NotFoundException(message);
      case 409:
        if (path == '/verdict') {
          throw AlreadyDecidedException(AlreadyDecided.fromJson(json));
        }
        throw ConflictException(message);
      default:
        throw ApiException(message, statusCode: status);
    }
  }

  static Map<String, dynamic> _decode(http.Response response) {
    if (response.body.isEmpty) return const {};
    try {
      final decoded = jsonDecode(response.body);
      return decoded is Map<String, dynamic> ? decoded : const {};
    } on FormatException {
      return {'error': 'unexpected answer (HTTP ${response.statusCode}, not JSON)'};
    }
  }
}
