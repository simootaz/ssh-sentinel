import 'package:flutter_secure_storage/flutter_secure_storage.dart';

/// What the API client needs for every call.
class BackendConfig {
  const BackendConfig({required this.baseUrl, required this.adminToken});

  /// Base URL without a trailing slash, the routes are appended to it.
  final String baseUrl;
  final String adminToken;
}

/// A string key-value store. The app uses Android's encrypted storage; tests
/// use [MemoryKeyValueStore].
abstract class KeyValueStore {
  Future<String?> read(String key);
  Future<void> write(String key, String? value);
}

/// Backed by flutter_secure_storage: Android Keystore keys, encrypted values.
class SecureKeyValueStore implements KeyValueStore {
  SecureKeyValueStore({FlutterSecureStorage? storage})
      : _storage = storage ??
            const FlutterSecureStorage(
              aOptions: AndroidOptions(encryptedSharedPreferences: true),
            );

  final FlutterSecureStorage _storage;

  @override
  Future<String?> read(String key) => _storage.read(key: key);

  @override
  Future<void> write(String key, String? value) =>
      value == null ? _storage.delete(key: key) : _storage.write(key: key, value: value);
}

/// In-memory store for tests.
class MemoryKeyValueStore implements KeyValueStore {
  final Map<String, String> values = {};

  @override
  Future<String?> read(String key) async => values[key];

  @override
  Future<void> write(String key, String? value) async {
    if (value == null) {
      values.remove(key);
    } else {
      values[key] = value;
    }
  }
}

/// The phone's settings: backend URL, admin token, device registration.
class SettingsStore {
  SettingsStore({KeyValueStore? store}) : _store = store ?? SecureKeyValueStore();

  static const _baseUrlKey = 'base_url';
  static const _adminTokenKey = 'admin_token';
  static const _deviceIdKey = 'device_id';
  static const _deviceLabelKey = 'device_label';
  static const _fcmTokenKey = 'registered_fcm_token';

  final KeyValueStore _store;

  Future<String?> get baseUrl => _store.read(_baseUrlKey);
  Future<String?> get adminToken => _store.read(_adminTokenKey);

  /// Id returned by `POST /devices`, sent as `device_id` with every verdict.
  Future<String?> get deviceId => _store.read(_deviceIdKey);
  Future<String?> get deviceLabel => _store.read(_deviceLabelKey);

  /// The FCM token last sent to the backend, to notice a rotation.
  Future<String?> get registeredFcmToken => _store.read(_fcmTokenKey);

  Future<void> setBackend({required String baseUrl, required String adminToken}) async {
    await _store.write(_baseUrlKey, normalizeBaseUrl(baseUrl));
    await _store.write(_adminTokenKey, adminToken.trim());
  }

  Future<void> setDeviceLabel(String? label) =>
      _store.write(_deviceLabelKey, label == null || label.trim().isEmpty ? null : label.trim());

  Future<void> setRegistration({required String deviceId, required String fcmToken}) async {
    await _store.write(_deviceIdKey, deviceId);
    await _store.write(_fcmTokenKey, fcmToken);
  }

  Future<void> clearRegistration() async {
    await _store.write(_deviceIdKey, null);
    await _store.write(_fcmTokenKey, null);
  }

  /// Null until both the URL and the token are set.
  Future<BackendConfig?> config() async {
    final url = await baseUrl;
    final token = await adminToken;
    if (url == null || url.isEmpty || token == null || token.isEmpty) return null;
    return BackendConfig(baseUrl: url, adminToken: token);
  }

  Future<bool> get isConfigured async => await config() != null;

  /// Trims whitespace and trailing slashes so `$baseUrl/verdict` is always right.
  static String normalizeBaseUrl(String raw) {
    var url = raw.trim();
    while (url.endsWith('/')) {
      url = url.substring(0, url.length - 1);
    }
    return url;
  }

  /// A short reason when the URL cannot be used, null when it looks fine.
  static String? validateBaseUrl(String raw) {
    final url = normalizeBaseUrl(raw);
    if (url.isEmpty) return 'Enter the backend URL.';
    final uri = Uri.tryParse(url);
    if (uri == null || uri.host.isEmpty) return 'Not a valid URL.';
    if (uri.scheme != 'https' && uri.scheme != 'http') {
      return 'The URL must start with https://';
    }
    return null;
  }
}
