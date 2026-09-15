import '../models/access_request.dart';
import '../models/blocked_ip.dart';
import '../models/device.dart';
import '../models/enums.dart';
import '../models/geo_rule.dart';
import '../models/verdict_result.dart';
import '../models/whitelist_entry.dart';

/// `GET /healthz`.
class Health {
  const Health({required this.status, required this.db});

  factory Health.fromJson(Map<String, dynamic> json) => Health(
        status: json['status']?.toString() ?? 'unknown',
        db: json['db']?.toString() ?? 'unknown',
      );

  final String status;
  final String db;

  bool get isOk => status == 'ok';
}

/// The routes of contract v1 the app calls (docs/architecture.md, section 5).
///
/// Screens depend on this interface; [HttpSentinelApi] talks to the backend
/// and the tests use a fake.
abstract class SentinelApi {
  /// `POST /verdict`. Throws [AlreadyDecidedException] on 409.
  Future<VerdictResult> sendVerdict({
    required String requestId,
    required Verdict verdict,
    int? ttlSeconds,
    String? deviceId,
  });

  /// `GET /history`, newest first. Pass a page's `nextBefore` as [before].
  Future<HistoryPage> history({
    int? limit,
    String? before,
    String? server,
    String? username,
    RequestContext? context,
    RequestStatus? status,
  });

  /// `GET /whitelist`.
  Future<List<WhitelistEntry>> listWhitelist();

  /// `POST /whitelist`. [server] null means every server, [ttlSeconds] null
  /// means permanent.
  Future<WhitelistEntry> addWhitelistEntry({
    required String username,
    RequestContext context = RequestContext.ssh,
    String? server,
    int? ttlSeconds,
  });

  /// `DELETE /whitelist/{id}`.
  Future<void> deleteWhitelistEntry(String id);

  /// `POST /devices`, upsert on the FCM token.
  Future<Device> registerDevice({
    required String fcmToken,
    String platform = 'android',
    String? label,
  });

  /// `GET /devices`.
  Future<List<Device>> listDevices();

  /// `DELETE /devices/{id}`, for a lost or replaced phone.
  Future<void> deleteDevice(String id);

  /// `GET /blocked-ips`.
  Future<List<BlockedIp>> listBlockedIps();

  /// `DELETE /blocked-ips/{id}`.
  Future<void> unblockIp(String id);

  /// `GET /geo-rules`.
  Future<List<GeoRule>> listGeoRules();

  /// `POST /geo-rules`. Throws [ConflictException] when the country has a rule.
  Future<GeoRule> addGeoRule({required String country, String? note});

  /// `DELETE /geo-rules/{id}`.
  Future<void> deleteGeoRule(String id);

  /// `GET /healthz`, no auth. Checks the URL, not the token.
  Future<Health> health();
}
