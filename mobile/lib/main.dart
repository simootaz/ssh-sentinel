import 'dart:async';

import 'package:firebase_core/firebase_core.dart';
import 'package:firebase_messaging/firebase_messaging.dart';
import 'package:flutter/material.dart';

import 'app.dart';
import 'services/app_services.dart';
import 'services/background_handlers.dart';
import 'services/push_service.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();

  final firebaseReady = await _initFirebase();
  if (firebaseReady) {
    // Must be registered before runApp: pushes received while the app is
    // closed are handled in a separate isolate.
    FirebaseMessaging.onBackgroundMessage(firebaseMessagingBackgroundHandler);
  }

  final services = AppServices.production(firebaseReady: firebaseReady);
  final navigatorKey = GlobalKey<NavigatorState>();
  runApp(SentinelApp(services: services, navigatorKey: navigatorKey));

  await PushService(services: services, navigatorKey: navigatorKey).init();

  // First launch, or a token that rotated while the app was closed. Errors
  // are shown in Settings, where the user can retry.
  unawaited(services.registrar.register().catchError((Object e) {
    debugPrint('device registration skipped: $e');
    return null;
  }));
}

/// False when google-services.json is missing from the build: the app still
/// runs for the lists and Settings, and says so there.
Future<bool> _initFirebase() async {
  try {
    await Firebase.initializeApp();
    return true;
  } catch (e) {
    debugPrint('Firebase not initialised, push disabled: $e');
    return false;
  }
}
