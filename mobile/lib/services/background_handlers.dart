import 'package:firebase_core/firebase_core.dart';
import 'package:firebase_messaging/firebase_messaging.dart';
import 'package:flutter_local_notifications/flutter_local_notifications.dart';

import '../models/enums.dart';
import '../models/push_message.dart';
import '../widgets/format.dart';
import 'api_client.dart';
import 'api_exceptions.dart';
import 'http_api_client.dart';
import 'notification_service.dart';
import 'settings_store.dart';

/// Runs in its own isolate when a push arrives while the app is in the
/// background or closed. It only shows or dismisses a notification; the
/// backend is not needed here.
@pragma('vm:entry-point')
Future<void> firebaseMessagingBackgroundHandler(RemoteMessage message) async {
  await Firebase.initializeApp();
  final notifications = NotificationService();
  await notifications.init(onBackgroundResponse: notificationActionBackground);
  await showPushNotification(message.data, notifications);
}

/// Shows the notification for `access_request` and `access_notice`,
/// dismisses it for `request_decided`, ignores unknown types.
Future<void> showPushNotification(
  Map<String, dynamic> data,
  NotificationService notifications,
) async {
  switch (PushMessage.parse(data)) {
    case AccessRequestPush(:final request):
      await notifications.showRequest(request);
    case RequestDecidedPush(:final decision):
      await notifications.cancelRequest(decision.requestId);
    case null:
      break;
  }
}

/// Runs in its own isolate when Deny or Approve is tapped on a notification
/// and the app is not in the foreground. Sends the verdict to the backend
/// directly, which is what makes a decision from the lock screen work.
@pragma('vm:entry-point')
Future<void> notificationActionBackground(NotificationResponse response) async {
  final settings = SettingsStore();
  final api = HttpSentinelApi(configProvider: settings.config);
  final notifications = NotificationService();
  await notifications.init(onBackgroundResponse: notificationActionBackground);
  await handleNotificationAction(
    response,
    api: api,
    settings: settings,
    notifications: notifications,
  );
}

/// The logic behind a notification button, kept free of isolate plumbing so
/// it can be tested with a fake API.
Future<void> handleNotificationAction(
  NotificationResponse response, {
  required SentinelApi api,
  required SettingsStore settings,
  required NotificationService notifications,
}) async {
  final request = NotificationService.requestFromPayload(response.payload);
  if (request == null) return;

  final verdict = switch (response.actionId) {
    NotificationService.actionApprove => Verdict.approve,
    NotificationService.actionDeny => Verdict.deny,
    // "Always allow" and a plain tap open the app instead.
    _ => null,
  };
  if (verdict == null) return;

  final who = '${request.username} on ${request.server}'
      '${request.isSudo ? ' (sudo)' : ''}';
  try {
    final result = await api.sendVerdict(
      requestId: request.id,
      verdict: verdict,
      deviceId: await settings.deviceId,
    );
    final word = switch (result.status) {
      RequestStatus.approved => 'Approved',
      RequestStatus.denied => 'Denied',
      _ => statusLabel(result.status),
    };
    final blocked = result.autoBlocked;
    await notifications.showOutcome(
      request.id,
      title: '$word: $who',
      body: blocked == null
          ? 'Sent from this phone'
          : 'Sent from this phone. ${blocked.ip} is now blocked '
              'after ${blocked.denialCount} denials.',
    );
  } on AlreadyDecidedException catch (e) {
    // Another phone was faster, or the request timed out. Show it, no retry.
    await notifications.showOutcome(
      request.id,
      title: 'Request ${e.decision.description}',
      body: who,
    );
  } catch (e) {
    await notifications.showError(request.id, describeError(e));
    // The button removed the notification; put it back while it can be used.
    if (!request.isExpired()) await notifications.showRequest(request);
  }
}
