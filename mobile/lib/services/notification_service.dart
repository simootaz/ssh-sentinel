import 'dart:convert';

import 'package:flutter_local_notifications/flutter_local_notifications.dart';

import '../models/incoming_request.dart';

/// Builds and dismisses the notifications. Pushes are data-only, so the app
/// owns every notification in every state: foreground, background, closed.
class NotificationService {
  NotificationService({FlutterLocalNotificationsPlugin? plugin})
      : _plugin = plugin ?? FlutterLocalNotificationsPlugin();

  static const requestChannel = AndroidNotificationChannel(
    'access_requests',
    'Access requests',
    description: 'SSH logins and sudo waiting for a verdict',
    importance: Importance.max,
  );
  static const noticeChannel = AndroidNotificationChannel(
    'access_notices',
    'Notify-only logins',
    description: 'Logins on servers in notify mode, nothing to decide',
    importance: Importance.defaultImportance,
  );
  static const outcomeChannel = AndroidNotificationChannel(
    'verdict_outcomes',
    'Verdict outcomes',
    description: 'What happened after a button was tapped',
    importance: Importance.low,
  );

  /// Action ids of the three buttons.
  static const actionApprove = 'approve';
  static const actionDeny = 'deny';
  static const actionApproveAlways = 'approve_always';

  final FlutterLocalNotificationsPlugin _plugin;
  bool _initialized = false;

  /// Sets up the plugin and the channels. Safe to call more than once.
  Future<void> init({
    DidReceiveNotificationResponseCallback? onResponse,
    DidReceiveBackgroundNotificationResponseCallback? onBackgroundResponse,
  }) async {
    if (_initialized) return;
    await _plugin.initialize(
      const InitializationSettings(
        android: AndroidInitializationSettings('ic_notification'),
      ),
      onDidReceiveNotificationResponse: onResponse,
      onDidReceiveBackgroundNotificationResponse: onBackgroundResponse,
    );
    final android = _plugin.resolvePlatformSpecificImplementation<
        AndroidFlutterLocalNotificationsPlugin>();
    if (android != null) {
      for (final channel in [requestChannel, noticeChannel, outcomeChannel]) {
        await android.createNotificationChannel(channel);
      }
    }
    _initialized = true;
  }

  /// Android 13+ permission prompt. True when granted or not needed.
  Future<bool> requestPermission() async {
    final android = _plugin.resolvePlatformSpecificImplementation<
        AndroidFlutterLocalNotificationsPlugin>();
    return await android?.requestNotificationsPermission() ?? true;
  }

  /// Tells whether a notification tap or button launched the app.
  Future<NotificationAppLaunchDetails?> launchDetails() =>
      _plugin.getNotificationAppLaunchDetails();

  /// Notification id of a request, stable across isolates.
  static int idFor(String requestId) => requestId.hashCode & 0x7fffffff;

  static String payloadFor(IncomingRequest request) =>
      jsonEncode(request.toPushData());

  /// The request carried by a notification, null when the payload is not ours.
  static IncomingRequest? requestFromPayload(String? payload) {
    if (payload == null || payload.isEmpty) return null;
    try {
      final decoded = jsonDecode(payload);
      if (decoded is! Map<String, dynamic>) return null;
      final request = IncomingRequest.fromPushData(decoded);
      return request.id.isEmpty ? null : request;
    } on FormatException {
      return null;
    }
  }

  /// "deploy from 203.0.113.42 (Paris, FR)" or "deploy ran sudo ... / source: local".
  static String bodyFor(IncomingRequest request) {
    if (request.isSudo) {
      final command = request.command ?? 'sudo (command not captured)';
      return '${request.username} ran $command\nsource: ${request.sourceLabel}';
    }
    final geo = request.geoLabel;
    return '${request.username} from ${request.sourceLabel}'
        '${geo == null ? '' : ' ($geo)'}';
  }

  /// Shows, or refreshes, the notification of a request. The buttons and
  /// the countdown are only there while a verdict is still possible.
  Future<void> showRequest(IncomingRequest request) async {
    final decidable = !request.notice && !request.isExpired();
    final channel = request.notice ? noticeChannel : requestChannel;
    final body = bodyFor(request);
    final details = AndroidNotificationDetails(
      channel.id,
      channel.name,
      channelDescription: channel.description,
      icon: 'ic_notification',
      importance: channel.importance,
      priority: request.notice ? Priority.defaultPriority : Priority.high,
      visibility: NotificationVisibility.public,
      styleInformation: BigTextStyleInformation(body),
      // The clock in the notification counts down to expires_at.
      when: request.expiresAt.millisecondsSinceEpoch,
      usesChronometer: decidable,
      chronometerCountDown: decidable,
      actions: decidable
          ? const [
              AndroidNotificationAction(actionDeny, 'Deny'),
              AndroidNotificationAction(actionApprove, 'Approve'),
              // Needs the TTL picker, so this one opens the app.
              AndroidNotificationAction(actionApproveAlways, 'Always allow',
                  showsUserInterface: true, cancelNotification: false),
            ]
          : null,
    );
    await _plugin.show(
      idFor(request.id),
      request.notice ? '${request.title} (notify only)' : request.title,
      body,
      NotificationDetails(android: details),
      payload: payloadFor(request),
    );
  }

  /// Replaces the request notification with its outcome.
  Future<void> showOutcome(String requestId,
      {required String title, required String body}) async {
    final details = AndroidNotificationDetails(
      outcomeChannel.id,
      outcomeChannel.name,
      channelDescription: outcomeChannel.description,
      icon: 'ic_notification',
      importance: outcomeChannel.importance,
      priority: Priority.low,
      styleInformation: BigTextStyleInformation(body),
    );
    await _plugin.show(idFor(requestId), title, body,
        NotificationDetails(android: details));
  }

  /// A separate notification for a verdict that could not be sent.
  Future<void> showError(String requestId, String message) async {
    final details = AndroidNotificationDetails(
      outcomeChannel.id,
      outcomeChannel.name,
      channelDescription: outcomeChannel.description,
      icon: 'ic_notification',
      importance: Importance.high,
      priority: Priority.high,
      styleInformation: BigTextStyleInformation(message),
    );
    await _plugin.show(idFor('$requestId:error'), 'Verdict not sent', message,
        NotificationDetails(android: details));
  }

  /// Dismisses the notification of a request, decided or not.
  Future<void> cancelRequest(String requestId) => _plugin.cancel(idFor(requestId));
}
