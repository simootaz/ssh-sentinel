import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../models/device.dart';
import '../../services/api_client.dart';
import '../../services/api_exceptions.dart';
import '../../services/device_registration.dart';
import '../../services/settings_store.dart';
import '../../widgets/format.dart';
import '../../widgets/status_views.dart';

/// Backend URL, admin token, this phone's registration and the list of
/// registered phones.
///
/// The screen sits in the home shell's IndexedStack, so it brings its own
/// Scaffold. Everything the user types is saved through [SettingsStore]
/// before any API call, because the real API client reads the URL and the
/// token from the store on every request.
class SettingsScreen extends StatefulWidget {
  const SettingsScreen({
    super.key,
    required this.settings,
    required this.api,
    required this.registrar,
    this.pushAvailable = true,
    this.onSaved,
  });

  final SettingsStore settings;
  final SentinelApi api;
  final DeviceRegistrar registrar;

  /// False when the build has no Firebase configuration: say so, no push will arrive.
  final bool pushAvailable;

  /// Called after settings were saved, so the home shell can re-check them.
  final VoidCallback? onSaved;

  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  final _urlController = TextEditingController();
  final _tokenController = TextEditingController();
  final _labelController = TextEditingController();

  bool _showToken = false;

  // Inline validation messages under the fields, null when the value is fine.
  String? _urlError;
  String? _tokenError;

  // True while "Save and register" or "Test connection" runs: both buttons
  // are disabled and a progress indicator is shown, so a slow backend cannot
  // be hit twice.
  bool _busy = false;

  // Result of the last save or test, shown in the status card.
  String? _statusMessage;
  bool _statusIsError = false;

  // This phone's registration as stored on the phone.
  String? _deviceId;
  String? _deviceLabel;

  // Registered phones from GET /devices.
  List<Device> _devices = const [];
  Object? _devicesError;
  bool _devicesLoading = false;
  bool _devicesNotConfigured = false;

  @override
  void initState() {
    super.initState();
    _loadSettings();
  }

  @override
  void dispose() {
    _urlController.dispose();
    _tokenController.dispose();
    _labelController.dispose();
    super.dispose();
  }

  /// Fills the fields from the store, then loads the phone list.
  Future<void> _loadSettings() async {
    final settings = widget.settings;
    final url = await settings.baseUrl;
    final token = await settings.adminToken;
    final label = await settings.deviceLabel;
    final deviceId = await settings.deviceId;
    if (!mounted) return;
    setState(() {
      _urlController.text = url ?? '';
      _tokenController.text = token ?? '';
      _labelController.text = label ?? '';
      _deviceId = deviceId;
      _deviceLabel = label;
    });
    await _loadDevices();
  }

  /// Re-reads this phone's id and label after a registration or a removal.
  Future<void> _refreshRegistration() async {
    final deviceId = await widget.settings.deviceId;
    final label = await widget.settings.deviceLabel;
    if (!mounted) return;
    setState(() {
      _deviceId = deviceId;
      _deviceLabel = label;
      if (label != null && _labelController.text.trim().isEmpty) {
        _labelController.text = label;
      }
    });
  }

  Future<void> _loadDevices() async {
    if (!await widget.settings.isConfigured) {
      if (!mounted) return;
      setState(() {
        _devicesNotConfigured = true;
        _devicesLoading = false;
        _devicesError = null;
        _devices = const [];
      });
      return;
    }
    setState(() {
      _devicesNotConfigured = false;
      _devicesLoading = true;
      _devicesError = null;
    });
    try {
      final devices = await widget.api.listDevices();
      if (!mounted) return;
      setState(() {
        _devices = devices;
        _devicesLoading = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _devicesError = e;
        _devicesLoading = false;
      });
    }
  }

  /// Validates the fields and writes them to the store. Returns false, with
  /// the reason shown under the field, when nothing was saved.
  Future<bool> _save() async {
    final urlError = SettingsStore.validateBaseUrl(_urlController.text);
    final tokenError =
        _tokenController.text.trim().isEmpty ? 'Enter the admin token.' : null;
    setState(() {
      _urlError = urlError;
      _tokenError = tokenError;
    });
    if (urlError != null || tokenError != null) return false;

    await widget.settings.setBackend(
      baseUrl: _urlController.text,
      adminToken: _tokenController.text,
    );
    await widget.settings.setDeviceLabel(_labelController.text);
    widget.onSaved?.call();
    return true;
  }

  void _setStatus(String message, {bool error = false}) {
    if (!mounted) return;
    setState(() {
      _statusMessage = message;
      _statusIsError = error;
    });
  }

  /// "Save and register": store the settings, then POST /devices.
  Future<void> _saveAndRegister() async {
    FocusScope.of(context).unfocus();
    setState(() => _busy = true);
    try {
      if (!await _save()) return;
      final device = await widget.registrar.register(force: true);
      if (device != null) {
        _setStatus('Saved. Registered as ${device.displayLabel} (id ${device.id})');
      } else if (!widget.pushAvailable) {
        _setStatus('Saved. Push is not available in this build (no Firebase '
            'configuration), the phone was not registered');
      } else {
        _setStatus('Saved. No push token yet, the phone will register when it gets one');
      }
    } catch (e) {
      // A NetworkException means a wrong URL or an unreachable backend, an
      // UnauthorizedException a wrong token; describeError says which.
      _setStatus(describeError(e), error: true);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
    await _refreshRegistration();
    await _loadDevices();
  }

  /// "Test connection": store the settings, then check the URL with
  /// GET /healthz (no auth) and the token with GET /devices.
  Future<void> _testConnection() async {
    FocusScope.of(context).unfocus();
    setState(() => _busy = true);
    try {
      if (!await _save()) return;
      final health = await widget.api.health();
      if (!health.isOk) {
        _setStatus('Backend reachable but degraded (status ${health.status}, '
            'db ${health.db})', error: true);
        return;
      }
      await widget.api.listDevices();
      _setStatus('Backend reachable, token accepted');
    } catch (e) {
      _setStatus(describeError(e), error: true);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
    await _loadDevices();
  }

  /// Puts the FCM token on the clipboard, to send a test push from the
  /// Firebase console or with the backend's tooling.
  Future<void> _copyPushToken() async {
    String? token;
    try {
      token = await widget.registrar.fcmTokenProvider();
    } catch (_) {
      token = null;
    }
    if (!mounted) return;
    if (token == null || token.isEmpty) {
      showSnack(context, 'No push token available', error: true);
      return;
    }
    try {
      await Clipboard.setData(ClipboardData(text: token));
      if (!mounted) return;
      showSnack(context, 'Push token copied to the clipboard');
    } catch (e) {
      if (!mounted) return;
      showSnack(context, 'Could not copy the token: $e', error: true);
    }
  }

  Future<void> _deleteDevice(Device device) async {
    final confirmed = await confirmDialog(
      context,
      title: 'Remove ${device.displayLabel}?',
      message: 'It stops receiving pushes at once. History keeps the label.',
      confirmLabel: 'Remove',
      destructive: true,
    );
    if (!confirmed || !mounted) return;
    try {
      await widget.api.deleteDevice(device.id);
      // Removing this very phone means it must register again next time.
      if (device.id == _deviceId) {
        await widget.settings.clearRegistration();
        await _refreshRegistration();
      }
      if (!mounted) return;
      showSnack(context, 'Removed ${device.displayLabel}');
    } catch (e) {
      if (!mounted) return;
      showSnack(context, describeError(e), error: true);
    }
    await _loadDevices();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Settings')),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          _buildBackendSection(context),
          const SizedBox(height: 16),
          _buildActions(context),
          if (_statusMessage != null) ...[
            const SizedBox(height: 16),
            _buildStatusCard(context),
          ],
          const SizedBox(height: 24),
          _buildThisPhoneSection(context),
          const SizedBox(height: 24),
          _buildDevicesSection(context),
        ],
      ),
    );
  }

  Widget _sectionTitle(BuildContext context, String text) => Padding(
        padding: const EdgeInsets.only(bottom: 8),
        child: Text(text, style: Theme.of(context).textTheme.titleMedium),
      );

  Widget _buildBackendSection(BuildContext context) {
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        _sectionTitle(context, 'Backend'),
        TextField(
          key: const ValueKey('base_url_field'),
          controller: _urlController,
          enabled: !_busy,
          keyboardType: TextInputType.url,
          autocorrect: false,
          enableSuggestions: false,
          decoration: InputDecoration(
            labelText: 'Backend URL',
            hintText: 'https://sentinel.example.com',
            errorText: _urlError,
            border: const OutlineInputBorder(),
          ),
        ),
        const SizedBox(height: 12),
        TextField(
          key: const ValueKey('admin_token_field'),
          controller: _tokenController,
          enabled: !_busy,
          obscureText: !_showToken,
          autocorrect: false,
          enableSuggestions: false,
          decoration: InputDecoration(
            labelText: 'Admin token',
            errorText: _tokenError,
            border: const OutlineInputBorder(),
            suffixIcon: IconButton(
              tooltip: _showToken ? 'Hide token' : 'Show token',
              icon: Icon(_showToken ? Icons.visibility_off : Icons.visibility),
              onPressed: () => setState(() => _showToken = !_showToken),
            ),
          ),
        ),
        const SizedBox(height: 12),
        TextField(
          key: const ValueKey('device_label_field'),
          controller: _labelController,
          enabled: !_busy,
          textCapitalization: TextCapitalization.words,
          decoration: const InputDecoration(
            labelText: 'This phone\'s label',
            hintText: 'Pixel 8',
            helperText: 'Shown in history as the phone that decided.',
            border: OutlineInputBorder(),
          ),
        ),
        const SizedBox(height: 12),
        Text(
          'One base URL per deployment, no trailing path: the address your '
          'reverse proxy exposes, or the Scaleway function URL. The admin '
          'token is the backend\'s ADMIN_TOKEN.',
          style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.outline),
        ),
      ],
    );
  }

  Widget _buildActions(BuildContext context) {
    return Row(
      children: [
        Expanded(
          child: FilledButton.icon(
            key: const ValueKey('save_button'),
            onPressed: _busy ? null : _saveAndRegister,
            icon: const Icon(Icons.save_outlined),
            label: const Text('Save and register'),
          ),
        ),
        const SizedBox(width: 12),
        Expanded(
          child: OutlinedButton.icon(
            key: const ValueKey('test_button'),
            onPressed: _busy ? null : _testConnection,
            icon: const Icon(Icons.network_check),
            label: const Text('Test connection'),
          ),
        ),
        if (_busy) ...[
          const SizedBox(width: 12),
          const SizedBox(
            width: 20,
            height: 20,
            child: CircularProgressIndicator(strokeWidth: 2),
          ),
        ],
      ],
    );
  }

  Widget _buildStatusCard(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Card(
      key: const ValueKey('status_card'),
      color: _statusIsError ? scheme.errorContainer : scheme.primaryContainer,
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Icon(
              _statusIsError ? Icons.error_outline : Icons.check_circle_outline,
              color: _statusIsError ? scheme.error : scheme.onPrimaryContainer,
            ),
            const SizedBox(width: 8),
            Expanded(
              child: Text(
                _statusMessage ?? '',
                style: TextStyle(
                  color: _statusIsError ? scheme.error : scheme.onPrimaryContainer,
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildThisPhoneSection(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        _sectionTitle(context, 'This phone'),
        if (!widget.pushAvailable)
          Card(
            key: const ValueKey('push_warning'),
            color: scheme.tertiaryContainer,
            child: Padding(
              padding: const EdgeInsets.all(12),
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Icon(Icons.notifications_off_outlined, color: scheme.onTertiaryContainer),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Text(
                      'This build has no Firebase configuration '
                      '(android/app/google-services.json). Lists and settings '
                      'work, no push will arrive. See TESTING.md.',
                      style: TextStyle(color: scheme.onTertiaryContainer),
                    ),
                  ),
                ],
              ),
            ),
          ),
        ListTile(
          contentPadding: EdgeInsets.zero,
          leading: const Icon(Icons.phone_android),
          title: Text(_deviceLabel ?? 'No label'),
          subtitle: Text(
            _deviceId == null ? 'not registered' : 'device id $_deviceId',
          ),
        ),
        if (widget.pushAvailable)
          Align(
            alignment: Alignment.centerLeft,
            child: TextButton.icon(
              key: const ValueKey('copy_token'),
              onPressed: _copyPushToken,
              icon: const Icon(Icons.copy),
              label: const Text('Copy push token'),
            ),
          ),
      ],
    );
  }

  Widget _buildDevicesSection(BuildContext context) {
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            Expanded(child: _sectionTitle(context, 'Registered phones')),
            IconButton(
              key: const ValueKey('refresh_devices'),
              tooltip: 'Refresh',
              icon: const Icon(Icons.refresh),
              onPressed: _devicesLoading ? null : _loadDevices,
            ),
          ],
        ),
        if (_devicesNotConfigured)
          Text(
            'Save the settings to list the phones',
            style: theme.textTheme.bodyMedium?.copyWith(color: theme.colorScheme.outline),
          )
        else if (_devicesLoading && _devices.isEmpty)
          const Padding(
            padding: EdgeInsets.all(16),
            child: Center(child: CircularProgressIndicator()),
          )
        else if (_devicesError != null)
          ErrorView(error: _devicesError!, onRetry: _loadDevices)
        else if (_devices.isEmpty)
          const EmptyView(
            message: 'No phone registered yet',
            icon: Icons.phonelink_off,
          )
        else
          Card(
            child: Column(
              children: [
                for (final device in _devices) _buildDeviceRow(context, device),
              ],
            ),
          ),
      ],
    );
  }

  Widget _buildDeviceRow(BuildContext context, Device device) {
    final isThisPhone = device.id == _deviceId;
    return ListTile(
      leading: Icon(
        device.platform == 'android' ? Icons.android : Icons.phone_iphone,
      ),
      title: Row(
        children: [
          Flexible(
            child: Text(device.displayLabel, overflow: TextOverflow.ellipsis),
          ),
          if (isThisPhone) ...[
            const SizedBox(width: 8),
            const Tag('this phone'),
          ],
        ],
      ),
      subtitle: Text('${device.platform} · last seen ${formatAgo(device.lastSeenAt)}'),
      trailing: IconButton(
        key: ValueKey('delete_device_${device.id}'),
        tooltip: 'Remove',
        icon: const Icon(Icons.delete_outline),
        onPressed: () => _deleteDevice(device),
      ),
    );
  }
}
