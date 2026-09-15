import 'enums.dart';
import 'json_helpers.dart';

/// Default wait window of the backend, used when a push has no `expires_at`.
const Duration requestWindow = Duration(seconds: 30);

/// A request carried by an `access_request` or `access_notice` push.
///
/// Every value in an FCM data message is a string and "" means unknown.
/// The same map is used as the notification payload, so the background
/// handler can rebuild the request without the network.
class IncomingRequest {
  const IncomingRequest({
    required this.id,
    required this.context,
    required this.server,
    required this.username,
    required this.createdAt,
    required this.expiresAt,
    this.notice = false,
    this.sourceIp,
    this.geoCountry,
    this.geoCity,
    this.command,
  });

  /// Parses the `data` map of a push, or a notification payload.
  factory IncomingRequest.fromPushData(Map<String, dynamic> data) {
    final type = PushType.parse(readString(data['type']));
    final createdAt = readDateTime(data['created_at']) ?? DateTime.now().toUtc();
    return IncomingRequest(
      id: readString(data['request_id']) ?? '',
      notice: type == PushType.accessNotice,
      context: RequestContext.parse(readString(data['context'])),
      server: readString(data['server']) ?? '',
      username: readString(data['username']) ?? '',
      sourceIp: readString(data['source_ip']),
      geoCountry: readString(data['geo_country']),
      geoCity: readString(data['geo_city']),
      command: readString(data['command']),
      createdAt: createdAt,
      expiresAt: readDateTime(data['expires_at']) ?? createdAt.add(requestWindow),
    );
  }

  final String id;

  /// True for `access_notice`: notify mode, informational, no buttons.
  final bool notice;
  final RequestContext context;
  final String server;
  final String username;

  /// Null for sudo, shown as "local".
  final String? sourceIp;
  final String? geoCountry;
  final String? geoCity;

  /// The sudo command line, when the agent could read it.
  final String? command;
  final DateTime createdAt;

  /// The countdown runs from this instant, not from when the push arrived.
  final DateTime expiresAt;

  bool get isSudo => context == RequestContext.sudo;

  String get sourceLabel => sourceIp ?? 'local';

  /// "Paris, FR", "FR", or null when nothing is known.
  String? get geoLabel {
    final parts = [geoCity, geoCountry].whereType<String>().toList();
    return parts.isEmpty ? null : parts.join(', ');
  }

  /// "sudo on web-01" or "SSH login on web-01".
  String get title => isSudo ? 'sudo on $server' : 'SSH login on $server';

  Duration remaining([DateTime? now]) {
    final left = expiresAt.difference(now ?? DateTime.now());
    return left.isNegative ? Duration.zero : left;
  }

  bool isExpired([DateTime? now]) => remaining(now) == Duration.zero;

  /// Same shape as the push data, so [fromPushData] reads it back.
  Map<String, String> toPushData() => {
        'type': notice ? PushType.accessNotice.wire : PushType.accessRequest.wire,
        'request_id': id,
        'context': context.wire,
        'server': server,
        'username': username,
        'source_ip': sourceIp ?? '',
        'geo_country': geoCountry ?? '',
        'geo_city': geoCity ?? '',
        'command': command ?? '',
        'created_at': writeDateTime(createdAt) ?? '',
        'expires_at': writeDateTime(expiresAt) ?? '',
      };
}
