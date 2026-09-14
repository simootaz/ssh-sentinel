import 'enums.dart';
import 'json_helpers.dart';

/// Response `200` of `POST /verdict`.
class VerdictResult {
  const VerdictResult({
    required this.requestId,
    required this.status,
    this.decidedAt,
    this.decidedByDevice,
    this.whitelistEntry,
    this.autoBlocked,
  });

  factory VerdictResult.fromJson(Map<String, dynamic> json) {
    final entry = json['whitelist_entry'];
    final blocked = json['auto_blocked'];
    return VerdictResult(
      requestId: readString(json['request_id']) ?? '',
      status: RequestStatus.parse(readString(json['status'])),
      decidedAt: readDateTime(json['decided_at']),
      decidedByDevice: readString(json['decided_by_device']),
      whitelistEntry: entry is Map<String, dynamic>
          ? VerdictWhitelistEntry.fromJson(entry)
          : null,
      autoBlocked:
          blocked is Map<String, dynamic> ? AutoBlocked.fromJson(blocked) : null,
    );
  }

  final String requestId;
  final RequestStatus status;
  final DateTime? decidedAt;
  final String? decidedByDevice;

  /// Present only for `approve_always`.
  final VerdictWhitelistEntry? whitelistEntry;

  /// Filled only when this `deny` reached the auto-block threshold.
  final AutoBlocked? autoBlocked;
}

/// The whitelist entry created or refreshed by `approve_always`.
class VerdictWhitelistEntry {
  const VerdictWhitelistEntry({
    required this.id,
    required this.username,
    required this.context,
    this.server,
    this.expiresAt,
  });

  factory VerdictWhitelistEntry.fromJson(Map<String, dynamic> json) =>
      VerdictWhitelistEntry(
        id: readString(json['id']) ?? '',
        username: readString(json['username']) ?? '',
        context: RequestContext.parse(readString(json['context'])),
        server: readString(json['server']),
        expiresAt: readDateTime(json['expires_at']),
      );

  final String id;
  final String username;
  final RequestContext context;
  final String? server;

  /// Null means permanent.
  final DateTime? expiresAt;
}

/// The IP block created by the denial that reached the threshold.
class AutoBlocked {
  const AutoBlocked({required this.id, required this.ip, required this.denialCount});

  factory AutoBlocked.fromJson(Map<String, dynamic> json) => AutoBlocked(
        id: readString(json['id']) ?? '',
        ip: readString(json['ip']) ?? '',
        denialCount: readInt(json['denial_count']) ?? 0,
      );

  final String id;
  final String ip;
  final int denialCount;
}

/// Body of the `409` answer of `POST /verdict`: another phone was faster, or
/// the request timed out before anyone answered.
class AlreadyDecided {
  const AlreadyDecided({
    required this.status,
    this.decidedAt,
    this.decidedByDevice,
  });

  factory AlreadyDecided.fromJson(Map<String, dynamic> json) => AlreadyDecided(
        status: RequestStatus.parse(readString(json['status'])),
        decidedAt: readDateTime(json['decided_at']),
        decidedByDevice: readString(json['decided_by_device']),
      );

  final RequestStatus status;
  final DateTime? decidedAt;

  /// Label of the phone that won. Null for a timeout.
  final String? decidedByDevice;

  bool get isTimeout => status == RequestStatus.timeout;

  /// "already decided by Pixel 8" or "expired before anyone answered".
  String get description {
    if (isTimeout) return 'expired before anyone answered';
    final who = decidedByDevice ?? 'another phone';
    return 'already decided by $who';
  }
}
