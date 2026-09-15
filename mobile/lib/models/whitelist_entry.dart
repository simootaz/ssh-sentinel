import 'enums.dart';
import 'json_helpers.dart';

/// One always-allow entry, per user and context. `server` null means every
/// server, `expiresAt` null means permanent.
class WhitelistEntry {
  const WhitelistEntry({
    required this.id,
    required this.username,
    required this.context,
    required this.createdAt,
    this.server,
    this.expiresAt,
    this.createdFromRequest,
    this.createdByDevice,
  });

  factory WhitelistEntry.fromJson(Map<String, dynamic> json) => WhitelistEntry(
        id: readString(json['id']) ?? '',
        username: readString(json['username']) ?? '',
        context: RequestContext.parse(readString(json['context'])),
        server: readString(json['server']),
        expiresAt: readDateTime(json['expires_at']),
        createdAt: readDateTime(json['created_at']) ?? DateTime.now().toUtc(),
        createdFromRequest: readString(json['created_from_request']),
        createdByDevice: readString(json['created_by_device']),
      );

  final String id;
  final String username;
  final RequestContext context;
  final String? server;
  final DateTime? expiresAt;
  final DateTime createdAt;
  final String? createdFromRequest;
  final String? createdByDevice;

  bool get isPermanent => expiresAt == null;
  bool get isGlobal => server == null;

  /// "every server" or the server name.
  String get serverLabel => server ?? 'every server';
}
