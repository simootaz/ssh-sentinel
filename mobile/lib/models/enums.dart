/// Enumerations of contract v1 (docs/architecture.md, section 5).
///
/// Every parser keeps an `unknown` value so that a newer backend adding a
/// value never crashes the app: the contract says unknown fields and values
/// are ignored, not rejected.
library;

/// `context`: an SSH login or a sudo invocation.
enum RequestContext {
  ssh('ssh'),
  sudo('sudo');

  const RequestContext(this.wire);

  /// The string sent and received on the wire.
  final String wire;

  static RequestContext parse(String? value) {
    if (value == 'sudo') return RequestContext.sudo;
    return RequestContext.ssh;
  }
}

/// Request `status`.
enum RequestStatus {
  pending('pending'),
  approved('approved'),
  denied('denied'),
  timeout('timeout'),
  whitelisted('whitelisted'),
  blockedIp('blocked_ip'),
  blockedGeo('blocked_geo'),
  notified('notified'),
  unknown('unknown');

  const RequestStatus(this.wire);

  final String wire;

  static RequestStatus parse(String? value) {
    for (final status in RequestStatus.values) {
      if (status.wire == value) return status;
    }
    return RequestStatus.unknown;
  }

  /// True when the login or command was let through.
  bool get isAllowed =>
      this == approved || this == whitelisted || this == notified;

  /// True when the login or command was refused.
  bool get isRefused =>
      this == denied || this == timeout || this == blockedIp || this == blockedGeo;
}

/// `decided_by`: what produced the outcome of a request.
enum DecidedBy {
  admin('admin'),
  whitelist('whitelist'),
  timeout('timeout'),
  autoblock('autoblock'),
  georule('georule'),
  unknown('unknown');

  const DecidedBy(this.wire);

  final String wire;

  static DecidedBy? parse(String? value) {
    if (value == null) return null;
    for (final by in DecidedBy.values) {
      if (by.wire == value) return by;
    }
    return DecidedBy.unknown;
  }
}

/// `verdict` sent by the app in `POST /verdict`.
enum Verdict {
  approve('approve'),
  deny('deny'),
  approveAlways('approve_always');

  const Verdict(this.wire);

  final String wire;
}

/// Push `type`.
enum PushType {
  accessRequest('access_request'),
  accessNotice('access_notice'),
  requestDecided('request_decided'),
  unknown('unknown');

  const PushType(this.wire);

  final String wire;

  static PushType parse(String? value) {
    for (final type in PushType.values) {
      if (type.wire == value) return type;
    }
    return PushType.unknown;
  }
}
