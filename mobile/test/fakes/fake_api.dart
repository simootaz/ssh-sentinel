import 'package:ssh_sentinel/models/access_request.dart';
import 'package:ssh_sentinel/models/blocked_ip.dart';
import 'package:ssh_sentinel/models/device.dart';
import 'package:ssh_sentinel/models/enums.dart';
import 'package:ssh_sentinel/models/geo_rule.dart';
import 'package:ssh_sentinel/models/verdict_result.dart';
import 'package:ssh_sentinel/models/whitelist_entry.dart';
import 'package:ssh_sentinel/services/api_client.dart';
import 'package:ssh_sentinel/services/api_exceptions.dart';

/// One recorded `POST /verdict`.
class RecordedVerdict {
  const RecordedVerdict({
    required this.requestId,
    required this.verdict,
    this.ttlSeconds,
    this.deviceId,
  });

  final String requestId;
  final Verdict verdict;
  final int? ttlSeconds;
  final String? deviceId;
}

/// In-memory backend for widget tests. Fill the lists, set [failWith] to
/// make every call throw, [verdictFailWith] for `POST /verdict` only.
class FakeSentinelApi implements SentinelApi {
  List<AccessRequest> requests = [];
  List<WhitelistEntry> whitelist = [];
  List<Device> devices = [];
  List<BlockedIp> blockedIps = [];
  List<GeoRule> geoRules = [];

  /// Thrown by every call while set.
  Object? failWith;

  /// Thrown by [sendVerdict] only, for instance an [AlreadyDecidedException].
  Object? verdictFailWith;

  /// Added before every call answers, to test loading states.
  Duration delay = Duration.zero;

  /// Label reported as `decided_by_device` for this phone.
  String deviceLabel = 'Test phone';

  final List<RecordedVerdict> verdicts = [];
  final List<String> calls = [];
  final Map<String, String> _tokens = {};
  int _nextId = 1;

  String _id(String prefix) => '$prefix-${_nextId++}';

  Future<void> _before(String call) async {
    calls.add(call);
    if (delay > Duration.zero) await Future<void>.delayed(delay);
    final error = failWith;
    if (error != null) throw error;
  }

  @override
  Future<VerdictResult> sendVerdict({
    required String requestId,
    required Verdict verdict,
    int? ttlSeconds,
    String? deviceId,
  }) async {
    await _before('sendVerdict');
    verdicts.add(RecordedVerdict(
        requestId: requestId, verdict: verdict, ttlSeconds: ttlSeconds, deviceId: deviceId));
    final error = verdictFailWith;
    if (error != null) throw error;
    final now = DateTime.now().toUtc();
    final approved = verdict != Verdict.deny;
    return VerdictResult(
      requestId: requestId,
      status: approved ? RequestStatus.approved : RequestStatus.denied,
      decidedAt: now,
      decidedByDevice: deviceLabel,
      whitelistEntry: verdict == Verdict.approveAlways
          ? VerdictWhitelistEntry(
              id: _id('wl'),
              username: 'user',
              context: RequestContext.ssh,
              expiresAt: ttlSeconds == null ? null : now.add(Duration(seconds: ttlSeconds)),
            )
          : null,
    );
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
    await _before('history');
    final beforeAt = before == null ? null : DateTime.parse(before);
    final sorted = [...requests]..sort((a, b) => b.createdAt.compareTo(a.createdAt));
    final filtered = sorted.where((r) {
      if (beforeAt != null && !r.createdAt.isBefore(beforeAt)) return false;
      if (server != null && server.isNotEmpty && r.server != server) return false;
      if (username != null && username.isNotEmpty && r.username != username) return false;
      if (context != null && r.context != context) return false;
      if (status != null && r.status != status) return false;
      return true;
    }).toList();
    final size = limit ?? 50;
    final page = filtered.take(size).toList();
    return HistoryPage(
      items: page,
      nextBefore: filtered.length > size ? page.last.createdAt.toIso8601String() : null,
    );
  }

  @override
  Future<List<WhitelistEntry>> listWhitelist() async {
    await _before('listWhitelist');
    return List.of(whitelist);
  }

  @override
  Future<WhitelistEntry> addWhitelistEntry({
    required String username,
    RequestContext context = RequestContext.ssh,
    String? server,
    int? ttlSeconds,
  }) async {
    await _before('addWhitelistEntry');
    final now = DateTime.now().toUtc();
    whitelist.removeWhere(
        (e) => e.username == username && e.context == context && e.server == server);
    final entry = WhitelistEntry(
      id: _id('wl'),
      username: username,
      context: context,
      server: server,
      expiresAt: ttlSeconds == null ? null : now.add(Duration(seconds: ttlSeconds)),
      createdAt: now,
      createdByDevice: deviceLabel,
    );
    whitelist.add(entry);
    return entry;
  }

  @override
  Future<void> deleteWhitelistEntry(String id) async {
    await _before('deleteWhitelistEntry');
    _removeOrThrow(whitelist, (e) => e.id == id);
  }

  @override
  Future<Device> registerDevice({
    required String fcmToken,
    String platform = 'android',
    String? label,
  }) async {
    await _before('registerDevice');
    final now = DateTime.now().toUtc();
    final existingId = _tokens[fcmToken];
    devices.removeWhere((d) => d.id == existingId);
    final device = Device(
      id: existingId ?? _id('dev'),
      platform: platform,
      label: label,
      createdAt: now,
      lastSeenAt: now,
    );
    _tokens[fcmToken] = device.id;
    devices.add(device);
    return device;
  }

  @override
  Future<List<Device>> listDevices() async {
    await _before('listDevices');
    return List.of(devices);
  }

  @override
  Future<void> deleteDevice(String id) async {
    await _before('deleteDevice');
    _removeOrThrow(devices, (d) => d.id == id);
  }

  @override
  Future<List<BlockedIp>> listBlockedIps() async {
    await _before('listBlockedIps');
    return List.of(blockedIps);
  }

  @override
  Future<void> unblockIp(String id) async {
    await _before('unblockIp');
    _removeOrThrow(blockedIps, (b) => b.id == id);
  }

  @override
  Future<List<GeoRule>> listGeoRules() async {
    await _before('listGeoRules');
    return List.of(geoRules);
  }

  @override
  Future<GeoRule> addGeoRule({required String country, String? note}) async {
    await _before('addGeoRule');
    final code = country.trim().toUpperCase();
    if (code.length != 2 || !RegExp(r'^[A-Z]{2}$').hasMatch(code)) {
      throw const ApiException('unknown country code', statusCode: 400);
    }
    if (geoRules.any((r) => r.country == code)) {
      throw const ConflictException('country already has a rule');
    }
    final rule = GeoRule(
        id: _id('geo'), country: code, note: note, createdAt: DateTime.now().toUtc());
    geoRules.add(rule);
    return rule;
  }

  @override
  Future<void> deleteGeoRule(String id) async {
    await _before('deleteGeoRule');
    _removeOrThrow(geoRules, (r) => r.id == id);
  }

  @override
  Future<Health> health() async {
    await _before('health');
    return const Health(status: 'ok', db: 'ok');
  }

  static void _removeOrThrow<T>(List<T> list, bool Function(T) test) {
    final before = list.length;
    list.removeWhere(test);
    if (list.length == before) throw const NotFoundException('not found');
  }
}
