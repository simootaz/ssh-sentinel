import 'enums.dart';
import 'geo.dart';
import 'json_helpers.dart';

/// One row of `GET /history`: an SSH login or sudo invocation that reached
/// the backend, with its outcome.
class AccessRequest {
  const AccessRequest({
    required this.id,
    required this.server,
    required this.hostname,
    required this.context,
    required this.username,
    required this.status,
    required this.createdAt,
    this.sourceIp,
    this.tty,
    this.command,
    this.geo,
    this.decidedBy,
    this.decidedByDevice,
    this.expiresAt,
    this.decidedAt,
  });

  factory AccessRequest.fromJson(Map<String, dynamic> json) {
    final geoJson = json['geo'];
    return AccessRequest(
      id: readString(json['id']) ?? '',
      server: readString(json['server']) ?? '',
      hostname: readString(json['hostname']) ?? readString(json['server']) ?? '',
      context: RequestContext.parse(readString(json['context'])),
      username: readString(json['username']) ?? '',
      sourceIp: readString(json['source_ip']),
      tty: readString(json['tty']),
      command: readString(json['command']),
      geo: geoJson is Map<String, dynamic> ? Geo.fromJson(geoJson) : null,
      status: RequestStatus.parse(readString(json['status'])),
      decidedBy: DecidedBy.parse(readString(json['decided_by'])),
      decidedByDevice: readString(json['decided_by_device']),
      createdAt: readDateTime(json['created_at']) ?? DateTime.now().toUtc(),
      expiresAt: readDateTime(json['expires_at']),
      decidedAt: readDateTime(json['decided_at']),
    );
  }

  final String id;

  /// Server name from the enrolment, shown in the app.
  final String server;

  /// Hostname as reported by the agent, display only.
  final String hostname;
  final RequestContext context;
  final String username;

  /// Null for sudo: PAM gives sudo no remote host.
  final String? sourceIp;
  final String? tty;

  /// sudo only, best effort.
  final String? command;
  final Geo? geo;
  final RequestStatus status;
  final DecidedBy? decidedBy;

  /// Label of the phone that decided, when an admin did.
  final String? decidedByDevice;
  final DateTime createdAt;
  final DateTime? expiresAt;
  final DateTime? decidedAt;

  bool get isSudo => context == RequestContext.sudo;

  /// What to show for the source: the IP, or "local" when there is none.
  String get sourceLabel => sourceIp ?? 'local';
}

/// One page of `GET /history`.
class HistoryPage {
  const HistoryPage({required this.items, this.nextBefore});

  factory HistoryPage.fromJson(Map<String, dynamic> json) => HistoryPage(
        items: readItems(json).map(AccessRequest.fromJson).toList(),
        nextBefore: readString(json['next_before']),
      );

  final List<AccessRequest> items;

  /// Pass back as `before` to get the next page. Absent on the last page.
  final String? nextBefore;

  bool get hasMore => nextBefore != null;
}
