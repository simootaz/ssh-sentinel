/// Formatting helpers shared by the screens. Timestamps from the backend are
/// UTC; they are shown in the phone's local time.
library;

import '../models/enums.dart';

String _two(int n) => n.toString().padLeft(2, '0');

/// "2026-09-14 22:12:07" in local time.
String formatDateTime(DateTime? value) {
  if (value == null) return '-';
  final t = value.toLocal();
  return '${t.year}-${_two(t.month)}-${_two(t.day)} '
      '${_two(t.hour)}:${_two(t.minute)}:${_two(t.second)}';
}

/// "22:12:07" in local time.
String formatTime(DateTime? value) {
  if (value == null) return '-';
  final t = value.toLocal();
  return '${_two(t.hour)}:${_two(t.minute)}:${_two(t.second)}';
}

/// "3 d 2 h", "5 h 10 min", "12 min", "45 s".
String formatDuration(Duration d) {
  if (d.isNegative) d = Duration.zero;
  if (d.inDays >= 1) {
    final hours = d.inHours % 24;
    return hours == 0 ? '${d.inDays} d' : '${d.inDays} d $hours h';
  }
  if (d.inHours >= 1) {
    final minutes = d.inMinutes % 60;
    return minutes == 0 ? '${d.inHours} h' : '${d.inHours} h $minutes min';
  }
  if (d.inMinutes >= 1) return '${d.inMinutes} min';
  return '${d.inSeconds} s';
}

/// "permanent", "expires in 5 h 10 min", "expired".
String formatExpiry(DateTime? expiresAt, [DateTime? now]) {
  if (expiresAt == null) return 'permanent';
  final left = expiresAt.difference(now ?? DateTime.now());
  if (left.isNegative) return 'expired';
  return 'expires in ${formatDuration(left)}';
}

/// "just now", "5 min ago", "3 h ago", "2 d ago".
String formatAgo(DateTime? value, [DateTime? now]) {
  if (value == null) return '-';
  final elapsed = (now ?? DateTime.now()).difference(value);
  if (elapsed.inSeconds < 60) return 'just now';
  return '${formatDuration(elapsed)} ago';
}

String contextLabel(RequestContext context) =>
    context == RequestContext.sudo ? 'sudo' : 'ssh';

/// Human label for a request status.
String statusLabel(RequestStatus status) => switch (status) {
      RequestStatus.pending => 'pending',
      RequestStatus.approved => 'approved',
      RequestStatus.denied => 'denied',
      RequestStatus.timeout => 'timed out',
      RequestStatus.whitelisted => 'whitelisted',
      RequestStatus.blockedIp => 'blocked IP',
      RequestStatus.blockedGeo => 'blocked country',
      RequestStatus.notified => 'notified',
      RequestStatus.unknown => 'unknown',
    };

/// "approved by Pixel 8", "denied by autoblock", "timed out".
String outcomeLabel({
  required RequestStatus status,
  DecidedBy? decidedBy,
  String? decidedByDevice,
}) {
  final base = statusLabel(status);
  if (decidedBy == DecidedBy.admin) {
    return '$base by ${decidedByDevice ?? 'an admin'}';
  }
  return switch (decidedBy) {
    DecidedBy.whitelist => '$base (whitelist)',
    DecidedBy.autoblock => '$base (auto-block)',
    DecidedBy.georule => '$base (geo rule)',
    _ => base,
  };
}

/// Seconds of a TTL as a short label: "1 h", "24 h", "permanent".
String ttlLabel(int? seconds) =>
    seconds == null ? 'permanent' : formatDuration(Duration(seconds: seconds));
