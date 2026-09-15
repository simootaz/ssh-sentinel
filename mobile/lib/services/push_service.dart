import 'package:firebase_messaging/firebase_messaging.dart';
import 'package:flutter/material.dart';
import 'package:flutter_local_notifications/flutter_local_notifications.dart';

import '../models/incoming_request.dart';
import '../models/push_message.dart';
import '../screens/incoming_request/incoming_request_screen.dart';
import 'app_services.dart';
import 'background_handlers.dart';
import 'notification_service.dart';
import 'pending_requests.dart';

/// Wires FCM and the notifications to the running app.
///
/// - Foreground push: the incoming request screen opens at once.
/// - Background or closed: the background handlers show a notification; a
///   tap or "Always allow" lands here through [_onNotificationResponse].
/// - `request_decided`, or a 409, marks the request decided in
///   [PendingRequests]; the notification is dismissed from that signal.
class PushService {
  PushService({required this.services, required this.navigatorKey});

  final AppServices services;
  final GlobalKey<NavigatorState> navigatorKey;

  final Set<String> _openScreens = {};
  final Set<String> _dismissed = {};

  Future<void> init() async {
    await services.notifications.init(
      onResponse: _onNotificationResponse,
      onBackgroundResponse: notificationActionBackground,
    );
    services.pending.addListener(_onPendingChanged);

    if (services.firebaseReady) {
      final messaging = FirebaseMessaging.instance;
      await messaging.requestPermission();
      FirebaseMessaging.onMessage.listen(_onForegroundMessage);
      messaging.onTokenRefresh.listen((token) {
        services.registrar.onTokenRefresh(token).catchError((Object e) {
          debugPrint('device re-registration failed: $e');
          return null;
        });
      });
    }

    // A tap on a notification while the app was closed.
    final launch = await services.notifications.launchDetails();
    final response = launch?.notificationResponse;
    if (launch?.didNotificationLaunchApp == true && response != null) {
      _onNotificationResponse(response);
    }
  }

  Future<void> _onForegroundMessage(RemoteMessage message) async {
    switch (PushMessage.parse(message.data)) {
      case AccessRequestPush(:final request):
        if (request.notice) {
          await services.notifications.showRequest(request);
          return;
        }
        services.pending.add(request);
        await openRequest(request);
      case RequestDecidedPush(:final decision):
        services.pending.markDecided(
          decision.requestId,
          DecisionOutcome(
            status: decision.status,
            decidedByDevice: decision.decidedByDevice,
          ),
        );
        await services.notifications.cancelRequest(decision.requestId);
      case null:
        break;
    }
  }

  /// A tap on the notification body, or on "Always allow".
  void _onNotificationResponse(NotificationResponse response) {
    final request = NotificationService.requestFromPayload(response.payload);
    if (request == null || request.notice) return;
    services.pending.add(request);
    openRequest(
      request,
      openTtlPicker: response.actionId == NotificationService.actionApproveAlways,
    );
  }

  /// Pushes the incoming request screen, once per request.
  Future<void> openRequest(IncomingRequest request,
      {bool openTtlPicker = false}) async {
    if (_openScreens.contains(request.id)) return;
    final navigator = navigatorKey.currentState;
    if (navigator == null) {
      // First frame not built yet: try again right after it.
      WidgetsBinding.instance.addPostFrameCallback(
          (_) => openRequest(request, openTtlPicker: openTtlPicker));
      return;
    }
    final deviceId = await services.settings.deviceId;
    _openScreens.add(request.id);
    try {
      await navigator.push(MaterialPageRoute<void>(
        settings: RouteSettings(name: 'incoming/${request.id}'),
        fullscreenDialog: true,
        builder: (_) => IncomingRequestScreen(
          request: request,
          api: services.api,
          pending: services.pending,
          deviceId: deviceId,
          openTtlPicker: openTtlPicker,
        ),
      ));
    } finally {
      _openScreens.remove(request.id);
    }
  }

  void _onPendingChanged() {
    for (final entry in services.pending.all) {
      if (entry.isDecided && _dismissed.add(entry.request.id)) {
        services.notifications.cancelRequest(entry.request.id);
      }
    }
  }
}
