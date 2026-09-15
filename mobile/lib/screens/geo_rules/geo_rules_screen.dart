import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../models/geo_rule.dart';
import '../../services/api_client.dart';
import '../../services/api_exceptions.dart';
import '../../widgets/format.dart';
import '../../widgets/status_views.dart';

/// Flag emoji for an ISO 3166-1 alpha-2 code.
///
/// There is no flag image set to ship: Unicode builds a flag out of two
/// "regional indicator" letters, U+1F1E6 for A up to U+1F1FF for Z, and the
/// phone's font draws them as the country's flag. Returns an empty string for
/// anything that is not two letters.
String flagEmoji(String country) {
  final code = country.toUpperCase();
  if (!RegExp(r'^[A-Z]{2}$').hasMatch(code)) return '';
  const offset = 0x1F1E6 - 0x41; // 0x41 is 'A'
  return String.fromCharCodes(code.codeUnits.map((unit) => unit + offset));
}

const _explanation = 'Logins from these countries are refused without a push. '
    'Whitelisted users are refused too; break-glass still works.';

/// Country blocklist: list, add and remove geo rules (`/geo-rules`).
class GeoRulesScreen extends StatefulWidget {
  const GeoRulesScreen({super.key, required this.api});

  final SentinelApi api;

  @override
  State<GeoRulesScreen> createState() => _GeoRulesScreenState();
}

class _GeoRulesScreenState extends State<GeoRulesScreen> {
  List<GeoRule> _rules = const [];
  bool _loading = true;
  Object? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  /// First load and the retry button: the whole body shows the spinner, then
  /// either the list or the error.
  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final rules = await widget.api.listGeoRules();
      if (!mounted) return;
      setState(() {
        _rules = _sorted(rules);
        _loading = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = e;
        _loading = false;
      });
    }
  }

  /// Pull-to-refresh and after an add or a remove: the list stays on screen
  /// and a failure only shows a snack, so a network blip does not wipe it.
  Future<void> _refresh() async {
    try {
      final rules = await widget.api.listGeoRules();
      if (!mounted) return;
      setState(() => _rules = _sorted(rules));
    } catch (e) {
      if (!mounted) return;
      showSnack(context, describeError(e), error: true);
    }
  }

  /// Alphabetical by code, whatever order the backend used.
  static List<GeoRule> _sorted(List<GeoRule> rules) =>
      [...rules]..sort((a, b) => a.country.compareTo(b.country));

  Future<void> _add() async {
    final rule = await showDialog<GeoRule>(
      context: context,
      builder: (_) => _AddRuleDialog(api: widget.api),
    );
    if (rule == null || !mounted) return;
    showSnack(context, 'Rule for ${rule.country} added');
    await _refresh();
  }

  Future<void> _delete(GeoRule rule) async {
    final confirmed = await confirmDialog(
      context,
      title: 'Remove the ${rule.country} rule?',
      message: 'Logins from ${rule.country} will be pushed for approval again.',
      confirmLabel: 'Remove',
      destructive: true,
    );
    if (!confirmed || !mounted) return;
    try {
      await widget.api.deleteGeoRule(rule.id);
      if (!mounted) return;
      showSnack(context, 'Rule for ${rule.country} removed');
      await _refresh();
    } catch (e) {
      if (!mounted) return;
      showSnack(context, describeError(e), error: true);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Geo rules')),
      floatingActionButton: FloatingActionButton.extended(
        key: const ValueKey('add_rule'),
        onPressed: _add,
        icon: const Icon(Icons.add),
        label: const Text('Add'),
      ),
      body: _body(),
    );
  }

  Widget _body() {
    if (_loading) return const LoadingView();
    final error = _error;
    if (error != null) return ErrorView(error: error, onRetry: _load);
    return RefreshIndicator(
      onRefresh: _refresh,
      child: ListView(
        // Lets the user pull to refresh even when the list is shorter than
        // the screen, and keeps the last row clear of the floating button.
        physics: const AlwaysScrollableScrollPhysics(),
        padding: const EdgeInsets.only(bottom: 88),
        children: [
          const _Explanation(),
          if (_rules.isEmpty)
            const EmptyView(message: 'No country blocks', icon: Icons.public_off)
          else
            for (final rule in _rules)
              _RuleTile(rule: rule, onDelete: () => _delete(rule)),
        ],
      ),
    );
  }
}

/// Short reminder of what a geo rule does, shown above the list.
class _Explanation extends StatelessWidget {
  const _Explanation();

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 12, 16, 4),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(Icons.info_outline, size: 20, color: scheme.onSurfaceVariant),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              _explanation,
              style: Theme.of(context)
                  .textTheme
                  .bodyMedium
                  ?.copyWith(color: scheme.onSurfaceVariant),
            ),
          ),
        ],
      ),
    );
  }
}

/// One country in the list: flag, code, note, when it was added, remove button.
class _RuleTile extends StatelessWidget {
  const _RuleTile({required this.rule, required this.onDelete});

  final GeoRule rule;
  final VoidCallback onDelete;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final note = rule.note;
    final details = <Widget>[
      if (note != null && note.isNotEmpty) Text(note),
      if (rule.createdAt != null)
        Text('added ${formatDateTime(rule.createdAt)}',
            style: theme.textTheme.bodySmall),
    ];
    return ListTile(
      leading: Text(flagEmoji(rule.country), style: const TextStyle(fontSize: 28)),
      title: Text(
        rule.country,
        style: theme.textTheme.headlineSmall
            ?.copyWith(fontWeight: FontWeight.w600, letterSpacing: 1),
      ),
      subtitle: details.isEmpty
          ? null
          : Column(crossAxisAlignment: CrossAxisAlignment.start, children: details),
      trailing: IconButton(
        key: ValueKey('delete_${rule.id}'),
        tooltip: 'Remove',
        icon: const Icon(Icons.delete_outline),
        onPressed: onDelete,
      ),
    );
  }
}

/// "Block a country" form. Calls the backend itself and pops with the new
/// rule on success, so the screen only has to refresh.
class _AddRuleDialog extends StatefulWidget {
  const _AddRuleDialog({required this.api});

  final SentinelApi api;

  @override
  State<_AddRuleDialog> createState() => _AddRuleDialogState();
}

class _AddRuleDialogState extends State<_AddRuleDialog> {
  final _formKey = GlobalKey<FormState>();
  final _country = TextEditingController();
  final _note = TextEditingController();
  bool _submitting = false;

  /// Set when the backend rejected the code (409 or 400); shown under the
  /// country field through the validator, like a client-side error.
  String? _serverError;

  @override
  void dispose() {
    _country.dispose();
    _note.dispose();
    super.dispose();
  }

  String? _validateCountry(String? value) {
    if (_serverError != null) return _serverError;
    final code = (value ?? '').trim();
    if (!RegExp(r'^[A-Z]{2}$').hasMatch(code)) {
      return 'Two-letter country code, for example KP';
    }
    return null;
  }

  Future<void> _submit() async {
    _serverError = null;
    final form = _formKey.currentState;
    if (form == null || !form.validate()) return;
    setState(() => _submitting = true);
    final note = _note.text.trim();
    try {
      final rule = await widget.api.addGeoRule(
        country: _country.text.trim(),
        note: note.isEmpty ? null : note,
      );
      if (!mounted) return;
      Navigator.of(context).pop(rule);
      return;
    } on ConflictException {
      _serverError = 'This country already has a rule';
    } on ApiException catch (e) {
      if (e.statusCode == 400) {
        _serverError = 'Unknown country code';
      } else if (mounted) {
        showSnack(context, describeError(e), error: true);
      }
    } catch (e) {
      if (mounted) showSnack(context, describeError(e), error: true);
    }
    if (!mounted) return;
    setState(() => _submitting = false);
    // Re-runs the validator so a backend rejection appears under the field.
    form.validate();
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('Block a country'),
      content: Form(
        key: _formKey,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextFormField(
              key: const ValueKey('country_field'),
              controller: _country,
              autofocus: true,
              enabled: !_submitting,
              maxLength: 2,
              textCapitalization: TextCapitalization.characters,
              inputFormatters: [
                // The backend accepts any case; showing upper case as the
                // user types matches what the list will display.
                TextInputFormatter.withFunction(
                  (oldValue, newValue) =>
                      newValue.copyWith(text: newValue.text.toUpperCase()),
                ),
              ],
              decoration: const InputDecoration(
                labelText: 'Country code',
                hintText: 'KP',
                helperText: 'ISO 3166-1 alpha-2',
              ),
              validator: _validateCountry,
              onChanged: (_) {
                // A backend rejection applies to the code that was sent,
                // not to what the user is typing now.
                if (_serverError != null) {
                  _serverError = null;
                  _formKey.currentState?.validate();
                }
              },
            ),
            const SizedBox(height: 12),
            TextFormField(
              key: const ValueKey('note_field'),
              controller: _note,
              enabled: !_submitting,
              decoration: const InputDecoration(
                labelText: 'Note (optional)',
                hintText: 'no staff there',
              ),
              onFieldSubmitted: (_) => _submit(),
            ),
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: _submitting ? null : () => Navigator.of(context).pop(),
          child: const Text('Cancel'),
        ),
        FilledButton(
          key: const ValueKey('submit_rule'),
          onPressed: _submitting ? null : _submit,
          child: _submitting
              ? const SizedBox(
                  width: 16,
                  height: 16,
                  child: CircularProgressIndicator(strokeWidth: 2),
                )
              : const Text('Add rule'),
        ),
      ],
    );
  }
}
