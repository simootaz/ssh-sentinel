import 'json_helpers.dart';

/// A registered phone (`POST /devices`, `GET /devices`). The FCM token is
/// never returned by the backend.
class Device {
  const Device({
    required this.id,
    required this.platform,
    this.label,
    this.createdAt,
    this.lastSeenAt,
  });

  factory Device.fromJson(Map<String, dynamic> json) => Device(
        id: readString(json['id']) ?? '',
        platform: readString(json['platform']) ?? 'android',
        label: readString(json['label']),
        createdAt: readDateTime(json['created_at']),
        lastSeenAt: readDateTime(json['last_seen_at']),
      );

  final String id;
  final String platform;
  final String? label;
  final DateTime? createdAt;
  final DateTime? lastSeenAt;

  String get displayLabel => label ?? 'unnamed $platform phone';
}
