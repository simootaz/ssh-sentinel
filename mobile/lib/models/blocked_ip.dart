import 'json_helpers.dart';

/// An IP the backend auto-blocked after repeated denials.
class BlockedIp {
  const BlockedIp({
    required this.id,
    required this.ip,
    required this.reason,
    required this.denialCount,
    required this.hitCount,
    this.firstDeniedAt,
    this.lastDeniedAt,
    this.lastHitAt,
    this.createdAt,
    this.expiresAt,
  });

  factory BlockedIp.fromJson(Map<String, dynamic> json) => BlockedIp(
        id: readString(json['id']) ?? '',
        ip: readString(json['ip']) ?? '',
        reason: readString(json['reason']) ?? 'autoblock',
        denialCount: readInt(json['denial_count']) ?? 0,
        firstDeniedAt: readDateTime(json['first_denied_at']),
        lastDeniedAt: readDateTime(json['last_denied_at']),
        hitCount: readInt(json['hit_count']) ?? 0,
        lastHitAt: readDateTime(json['last_hit_at']),
        createdAt: readDateTime(json['created_at']),
        expiresAt: readDateTime(json['expires_at']),
      );

  final String id;
  final String ip;
  final String reason;

  /// Denials that triggered the block.
  final int denialCount;
  final DateTime? firstDeniedAt;
  final DateTime? lastDeniedAt;

  /// Requests denied without a push since the block.
  final int hitCount;
  final DateTime? lastHitAt;
  final DateTime? createdAt;

  /// Null means blocked until unblocked from the app.
  final DateTime? expiresAt;
}
