import 'package:device_info_plus/device_info_plus.dart';
import 'package:firebase_messaging/firebase_messaging.dart';
import 'package:flutter/foundation.dart';

import 'api_client.dart';
import 'device_registration.dart';
import 'http_api_client.dart';
import 'notification_service.dart';
import 'pending_requests.dart';
import 'settings_store.dart';

/// The long-lived objects of the app, built once in `main` and handed to the
/// screens. Tests build one with fakes.
class AppServices {
  AppServices({
    required this.settings,
    required this.api,
    required this.pending,
    required this.notifications,
    required this.registrar,
    this.firebaseReady = false,
  });

  /// Real storage, real backend, FCM when Firebase is initialised.
  factory AppServices.production({required bool firebaseReady}) {
    final settings = SettingsStore();
    final api = HttpSentinelApi(configProvider: settings.config);
    return AppServices(
      settings: settings,
      api: api,
      pending: PendingRequests(),
      notifications: NotificationService(),
      registrar: DeviceRegistrar(
        api: api,
        settings: settings,
        fcmTokenProvider: firebaseReady ? fcmToken : () async => null,
        defaultLabelProvider: androidDeviceLabel,
      ),
      firebaseReady: firebaseReady,
    );
  }

  final SettingsStore settings;
  final SentinelApi api;
  final PendingRequests pending;
  final NotificationService notifications;
  final DeviceRegistrar registrar;

  /// False when google-services.json was missing at build time: the app
  /// works for lists and settings, but no push arrives.
  final bool firebaseReady;
}

/// The FCM token of this phone, null when it cannot be obtained (no Google
/// Play services, Firebase not configured).
Future<String?> fcmToken() async {
  try {
    return await FirebaseMessaging.instance.getToken();
  } catch (e) {
    debugPrint('FCM token unavailable: $e');
    return null;
  }
}

/// "Google Pixel 8", the default label sent to `POST /devices`.
Future<String?> androidDeviceLabel() async {
  try {
    final info = await DeviceInfoPlugin().androidInfo;
    final label = '${info.manufacturer} ${info.model}'.trim();
    return label.isEmpty ? null : label;
  } catch (_) {
    return null;
  }
}
