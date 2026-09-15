import 'enums.dart';
import 'incoming_request.dart';
import 'json_helpers.dart';

/// A `request_decided` push: another phone, or the timeout, ended the request.
class RequestDecided {
  const RequestDecided({
    required this.requestId,
    required this.status,
    this.decidedByDevice,
  });

  factory RequestDecided.fromPushData(Map<String, dynamic> data) =>
      RequestDecided(
        requestId: readString(data['request_id']) ?? '',
        status: RequestStatus.parse(readString(data['status'])),
        decidedByDevice: readString(data['decided_by_device']),
      );

  final String requestId;
  final RequestStatus status;
  final String? decidedByDevice;
}

/// A parsed push. [parse] returns null for a type this app does not know,
/// which is how additive contract changes stay harmless.
sealed class PushMessage {
  const PushMessage();

  static PushMessage? parse(Map<String, dynamic> data) {
    switch (PushType.parse(readString(data['type']))) {
      case PushType.accessRequest:
      case PushType.accessNotice:
        final request = IncomingRequest.fromPushData(data);
        return request.id.isEmpty ? null : AccessRequestPush(request);
      case PushType.requestDecided:
        final decided = RequestDecided.fromPushData(data);
        return decided.requestId.isEmpty ? null : RequestDecidedPush(decided);
      case PushType.unknown:
        return null;
    }
  }
}

/// `access_request` or `access_notice`.
final class AccessRequestPush extends PushMessage {
  const AccessRequestPush(this.request);

  final IncomingRequest request;
}

/// `request_decided`.
final class RequestDecidedPush extends PushMessage {
  const RequestDecidedPush(this.decision);

  final RequestDecided decision;
}
