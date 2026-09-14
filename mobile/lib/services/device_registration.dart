import '../models/device.dart';
import 'api_client.dart';
import 'settings_store.dart';

/// Registers this phone with `POST /devices` at first launch and whenever
/// the FCM token rotates. The returned id is stored and sent as `device_id`
/// with every verdict, so history shows which phone decided.
class DeviceRegistrar {
  DeviceRegistrar({
    required this.api,
    required this.settings,
    required this.fcmTokenProvider,
    this.defaultLabelProvider,
  });

  final SentinelApi api;
  final SettingsStore settings;

  /// Returns the current FCM token, or null when Firebase is not set up.
  final Future<String?> Function() fcmTokenProvider;

  /// Returns a label such as "Pixel 8" when the user typed none.
  final Future<String?> Function()? defaultLabelProvider;

  /// Registers when the backend is configured and the phone is not yet
  /// registered, or the token changed, or [force] is true. Returns null when
  /// nothing was done. Throws the API error when the call fails.
  Future<Device?> register({bool force = false}) async {
    if (!await settings.isConfigured) return null;
    final token = await fcmTokenProvider();
    if (token == null || token.isEmpty) return null;

    final deviceId = await settings.deviceId;
    final registeredToken = await settings.registeredFcmToken;
    if (!force && deviceId != null && registeredToken == token) return null;

    return _register(token);
  }

  /// FCM rotated the token: the backend must learn the new one.
  Future<Device?> onTokenRefresh(String token) async {
    if (!await settings.isConfigured) return null;
    return _register(token);
  }

  Future<Device> _register(String token) async {
    var label = await settings.deviceLabel;
    if (label == null || label.isEmpty) {
      label = await defaultLabelProvider?.call();
      if (label != null && label.isNotEmpty) await settings.setDeviceLabel(label);
    }
    final device = await api.registerDevice(fcmToken: token, label: label);
    await settings.setRegistration(deviceId: device.id, fcmToken: token);
    return device;
  }
}
