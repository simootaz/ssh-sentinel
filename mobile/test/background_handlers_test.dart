// The notification buttons handled without the app open.
import 'package:flutter_local_notifications/flutter_local_notifications.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:ssh_sentinel/models/enums.dart';
import 'package:ssh_sentinel/models/incoming_request.dart';
import 'package:ssh_sentinel/models/verdict_result.dart';
import 'package:ssh_sentinel/services/api_exceptions.dart';
import 'package:ssh_sentinel/services/background_handlers.dart';
import 'package:ssh_sentinel/services/notification_service.dart';
import 'package:ssh_sentinel/services/settings_store.dart';

import 'fakes/fake_api.dart';

/// Records what would have been shown instead of talking to Android.
class RecordingNotifications extends NotificationService {
  final List<String> events = [];

  @override
  Future<void> showRequest(IncomingRequest request) async =>
      events.add('request:${request.id}');

  @override
  Future<void> showOutcome(String requestId,
          {required String title, required String body}) async =>
      events.add('outcome:$title');

  @override
  Future<void> showError(String requestId, String message) async =>
      events.add('error:$message');

  @override
  Future<void> cancelRequest(String requestId) async =>
      events.add('cancel:$requestId');
}

IncomingRequest request({bool expired = false}) {
  final now = DateTime.now().toUtc();
  return IncomingRequest(
    id: 'req-1',
    context: RequestContext.sudo,
    server: 'web-01',
    username: 'deploy',
    command: 'sudo systemctl restart nginx',
    createdAt: now.subtract(const Duration(seconds: 5)),
    expiresAt: expired
        ? now.subtract(const Duration(minutes: 1))
        : now.add(const Duration(minutes: 1)),
  );
}

NotificationResponse tap(String? actionId, {String? payload}) =>
    NotificationResponse(
      notificationResponseType: NotificationResponseType.selectedNotificationAction,
      actionId: actionId,
      payload: payload ?? NotificationService.payloadFor(request()),
    );

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late FakeSentinelApi api;
  late SettingsStore settings;
  late RecordingNotifications notifications;

  setUp(() async {
    api = FakeSentinelApi();
    settings = SettingsStore(store: MemoryKeyValueStore());
    await settings.setBackend(baseUrl: 'https://api.example.com', adminToken: 't');
    await settings.setRegistration(deviceId: 'dev-1', fcmToken: 'fcm');
    notifications = RecordingNotifications();
  });

  Future<void> handle(NotificationResponse response) => handleNotificationAction(
      response, api: api, settings: settings, notifications: notifications);

  test('Approve sends approve with this device id and shows the outcome', () async {
    await handle(tap(NotificationService.actionApprove));
    expect(api.verdicts.single.verdict, Verdict.approve);
    expect(api.verdicts.single.requestId, 'req-1');
    expect(api.verdicts.single.deviceId, 'dev-1');
    expect(notifications.events, ['outcome:Approved: deploy on web-01 (sudo)']);
  });

  test('Deny sends deny', () async {
    await handle(tap(NotificationService.actionDeny));
    expect(api.verdicts.single.verdict, Verdict.deny);
    expect(notifications.events.single, startsWith('outcome:Denied'));
  });

  test('409 shows who decided and does not retry', () async {
    api.verdictFailWith = AlreadyDecidedException(const AlreadyDecided(
        status: RequestStatus.approved, decidedByDevice: 'Pixel 8'));
    await handle(tap(NotificationService.actionApprove));
    expect(api.verdicts, hasLength(1));
    expect(notifications.events, ['outcome:Request already decided by Pixel 8']);
  });

  test('409 timeout says the request expired', () async {
    api.verdictFailWith = AlreadyDecidedException(
        const AlreadyDecided(status: RequestStatus.timeout));
    await handle(tap(NotificationService.actionDeny));
    expect(notifications.events, ['outcome:Request expired before anyone answered']);
  });

  test('a network error is shown and the buttons come back', () async {
    api.verdictFailWith = const NetworkException('connection refused');
    await handle(tap(NotificationService.actionApprove));
    expect(notifications.events, [
      'error:Backend unreachable: connection refused',
      'request:req-1',
    ]);
  });

  test('after expiry a failed verdict is not re-shown with buttons', () async {
    api.verdictFailWith = const NetworkException('down');
    await handle(tap(NotificationService.actionApprove,
        payload: NotificationService.payloadFor(request(expired: true))));
    expect(notifications.events, ['error:Backend unreachable: down']);
  });

  test('Always allow and a plain tap send nothing: the app handles them', () async {
    await handle(tap(NotificationService.actionApproveAlways));
    await handle(tap(null));
    expect(api.verdicts, isEmpty);
    expect(notifications.events, isEmpty);
  });

  test('a payload that is not ours is ignored', () async {
    await handle(tap(NotificationService.actionApprove, payload: 'garbage'));
    await handle(tap(NotificationService.actionApprove, payload: '{"type": "access_request"}'));
    expect(api.verdicts, isEmpty);
  });

  test('push data shows a request and dismisses on request_decided', () async {
    await showPushNotification(request().toPushData(), notifications);
    await showPushNotification(
        {'type': 'request_decided', 'request_id': 'req-1', 'status': 'approved'},
        notifications);
    await showPushNotification({'type': 'something_new'}, notifications);
    expect(notifications.events, ['request:req-1', 'cancel:req-1']);
  });
}
