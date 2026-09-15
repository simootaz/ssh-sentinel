import 'dart:async';

import 'package:flutter/material.dart';

import '../../models/enums.dart';
import '../../models/whitelist_entry.dart';
import '../../services/api_client.dart';
import '../../services/api_exceptions.dart';
import '../../widgets/format.dart';
import '../../widgets/status_views.dart';
import '../../widgets/ttl_picker.dart';

/// Always-allow entries: list, add and remove.
///
/// One entry covers one user for one context (ssh or sudo, docs section 8
/// item 7), on one server or on every server, until it expires or is removed.
/// The backend never returns expired entries, so nothing is filtered here.
class WhitelistScreen extends StatefulWidget {
  const WhitelistScreen({super.key, required this.api});

  final SentinelApi api;

  @override
  State<WhitelistScreen> createState() => _WhitelistScreenState();
}

class _WhitelistScreenState extends State<WhitelistScreen> {
  List<WhitelistEntry>? _entries;
  Object? _error;
  bool _loading = true;
  Timer? _clock;

  @override
  void initState() {
    super.initState();
    _load();
    // "expires in 5 h 10 min" is computed when the row is built. The tab stays
    // alive in the home shell's IndexedStack, so rebuild once a minute to keep
    // those labels honest without hitting the backend.
    _clock = Timer.periodic(const Duration(minutes: 1), (_) {
      if (mounted) setState(() {});
    });
  }

  @override
  void dispose() {
    _clock?.cancel();
    super.dispose();
  }

  /// Fetches the list. With [quiet] the current list stays on screen (pull to
  /// refresh, after an add or a remove) and a failure is only a snack; without
  /// it the whole body switches to the spinner, then to the list or the error.
  Future<void> _load({bool quiet = false}) async {
    if (!quiet) {
      setState(() {
        _loading = true;
        _error = null;
      });
    }
    try {
      final entries = await widget.api.listWhitelist();
      if (!mounted) return;
      setState(() {
        _entries = _sorted(entries);
        _error = null;
        _loading = false;
      });
    } catch (e) {
      if (!mounted) return;
      if (quiet && _entries != null) {
        showSnack(context, describeError(e), error: true);
        return;
      }
      setState(() {
        _error = e;
        _loading = false;
      });
    }
  }

  /// The backend does not promise an order; group by user so the same
  /// account on several servers sits together.
  static List<WhitelistEntry> _sorted(List<WhitelistEntry> entries) {
    final list = List.of(entries);
    list.sort((a, b) {
      final byUser = a.username.compareTo(b.username);
      if (byUser != 0) return byUser;
      final byContext = a.context.wire.compareTo(b.context.wire);
      if (byContext != 0) return byContext;
      return a.serverLabel.compareTo(b.serverLabel);
    });
    return list;
  }

  Future<void> _openAddForm() async {
    final created = await Navigator.of(context).push<WhitelistEntry>(
      MaterialPageRoute(
        fullscreenDialog: true,
        builder: (_) => _AddEntryPage(api: widget.api),
      ),
    );
    if (created == null || !mounted) return;
    showSnack(
      context,
      'Added ${created.username} (${contextLabel(created.context)}) on '
      '${created.serverLabel}, ${formatExpiry(created.expiresAt)}.',
    );
    await _load(quiet: true);
  }

  Future<void> _delete(WhitelistEntry entry) async {
    final what = entry.context == RequestContext.sudo ? 'for sudo' : 'at SSH login';
    final ok = await confirmDialog(
      context,
      title: 'Remove entry?',
      message: '${entry.username} will be asked again $what on ${entry.serverLabel}.',
      confirmLabel: 'Remove',
      destructive: true,
    );
    if (!ok || !mounted) return;
    try {
      await widget.api.deleteWhitelistEntry(entry.id);
      if (!mounted) return;
      showSnack(context, 'Removed ${entry.username} (${contextLabel(entry.context)}).');
    } catch (e) {
      if (!mounted) return;
      showSnack(context, describeError(e), error: true);
    }
    // Reload in both cases: a 404 means the entry was already gone.
    await _load(quiet: true);
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Whitelist')),
      body: _body(),
      floatingActionButton: FloatingActionButton.extended(
        key: const ValueKey('add_entry'),
        onPressed: _openAddForm,
        icon: const Icon(Icons.add),
        label: const Text('Add'),
      ),
    );
  }

  Widget _body() {
    if (_loading) return const LoadingView();
    final error = _error;
    if (error != null) return ErrorView(error: error, onRetry: () => _load());
    final entries = _entries ?? const <WhitelistEntry>[];
    return RefreshIndicator(
      onRefresh: () => _load(quiet: true),
      // AlwaysScrollable so pull to refresh works on a short or empty list.
      child: entries.isEmpty
          ? const CustomScrollView(
              physics: AlwaysScrollableScrollPhysics(),
              slivers: [
                SliverFillRemaining(
                  hasScrollBody: false,
                  child: EmptyView(
                    message: 'No always-allow entries',
                    icon: Icons.verified_user_outlined,
                  ),
                ),
              ],
            )
          : ListView.separated(
              physics: const AlwaysScrollableScrollPhysics(),
              // Room under the last row so the floating button hides nothing.
              padding: const EdgeInsets.only(bottom: 88),
              itemCount: entries.length,
              separatorBuilder: (context, index) => const Divider(height: 1),
              itemBuilder: (context, index) => _EntryTile(
                entry: entries[index],
                onDelete: () => _delete(entries[index]),
              ),
            ),
    );
  }
}

/// One row of the list.
class _EntryTile extends StatelessWidget {
  const _EntryTile({required this.entry, required this.onDelete});

  final WhitelistEntry entry;
  final VoidCallback onDelete;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final isSudo = entry.context == RequestContext.sudo;
    final addedBy = entry.createdByDevice;
    return ListTile(
      title: Row(
        children: [
          Flexible(
            child: Text(
              entry.username,
              style: theme.textTheme.titleMedium,
              overflow: TextOverflow.ellipsis,
            ),
          ),
          const SizedBox(width: 8),
          // sudo gets its own colour: it is the more powerful of the two.
          Tag(contextLabel(entry.context), color: isSudo ? Colors.deepOrange : null),
        ],
      ),
      subtitle: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(entry.serverLabel),
          Text(
            formatExpiry(entry.expiresAt),
            // A permanent bypass deserves a second look, so it stands out.
            style: entry.isPermanent
                ? TextStyle(color: theme.colorScheme.error, fontWeight: FontWeight.w600)
                : null,
          ),
          if (addedBy != null) Text('added by $addedBy', style: theme.textTheme.bodySmall),
        ],
      ),
      isThreeLine: true,
      trailing: IconButton(
        key: ValueKey('delete_${entry.id}'),
        tooltip: 'Remove',
        icon: const Icon(Icons.delete_outline),
        onPressed: onDelete,
      ),
    );
  }
}

/// The four duration chips. `custom` remembers the picked [TtlChoice].
enum _Duration { oneHour, oneDay, custom, permanent }

/// Full-screen form behind the "Add" button. Pops with the created entry, or
/// with nothing when the user backs out.
class _AddEntryPage extends StatefulWidget {
  const _AddEntryPage({required this.api});

  final SentinelApi api;

  @override
  State<_AddEntryPage> createState() => _AddEntryPageState();
}

class _AddEntryPageState extends State<_AddEntryPage> {
  final _username = TextEditingController();
  final _server = TextEditingController();
  RequestContext _requestContext = RequestContext.ssh;
  // Permanent is the risky choice, so the default is one day.
  _Duration _duration = _Duration.oneDay;
  TtlChoice? _customTtl;
  String? _usernameError;
  String? _serverError;
  bool _submitting = false;

  @override
  void initState() {
    super.initState();
    // The summary line under the form repeats what is typed, so rebuild on
    // every keystroke.
    _username.addListener(_rebuild);
    _server.addListener(_rebuild);
  }

  @override
  void dispose() {
    _username.dispose();
    _server.dispose();
    super.dispose();
  }

  void _rebuild() => setState(() {});

  TtlChoice get _ttl => switch (_duration) {
        _Duration.oneHour => TtlChoice.oneHour,
        _Duration.oneDay => TtlChoice.oneDay,
        _Duration.custom => _customTtl ?? TtlChoice.oneDay,
        _Duration.permanent => const TtlChoice.permanent(),
      };

  /// What the entry will do, in one sentence, so a wrong choice is visible
  /// before it is saved.
  String get _summary {
    final username = _username.text.trim();
    final server = _server.text.trim();
    final who = username.isEmpty ? 'This user' : username;
    final what = _requestContext == RequestContext.sudo ? 'run sudo' : 'log in over SSH';
    final where = server.isEmpty ? 'every server' : server;
    final howLong =
        _ttl.isPermanent ? 'until this entry is removed' : 'for the next ${_ttl.label}';
    return '$who may $what on $where without asking, $howLong.';
  }

  Future<void> _pickCustom() async {
    final choice = await showCustomTtlDialog(context);
    // Cancelled: keep whatever was selected before.
    if (choice == null || !mounted) return;
    setState(() {
      _customTtl = choice;
      _duration = _Duration.custom;
    });
  }

  Future<void> _submit() async {
    final username = _username.text.trim();
    final server = _server.text.trim();
    setState(() {
      _usernameError = username.isEmpty ? 'Enter a username.' : null;
      _serverError = null;
    });
    if (username.isEmpty) return;

    setState(() => _submitting = true);
    try {
      final entry = await widget.api.addWhitelistEntry(
        username: username,
        context: _requestContext,
        server: server.isEmpty ? null : server,
        ttlSeconds: _ttl.seconds,
      );
      if (!mounted) return;
      Navigator.of(context).pop(entry);
    } on NotFoundException catch (e) {
      // 404 on POST /whitelist means the server name is not enrolled.
      if (!mounted) return;
      setState(() {
        _submitting = false;
        _serverError = server.isEmpty ? describeError(e) : 'Unknown server name: $server';
      });
    } catch (e) {
      if (!mounted) return;
      setState(() => _submitting = false);
      showSnack(context, describeError(e), error: true);
    }
  }

  Widget _durationChip(_Duration value, {required String label, required String keyName}) {
    return ChoiceChip(
      key: ValueKey(keyName),
      label: Text(label),
      selected: _duration == value,
      onSelected: (_) {
        if (value == _Duration.custom) {
          _pickCustom();
        } else {
          setState(() => _duration = value);
        }
      },
    );
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final custom = _customTtl;
    return Scaffold(
      appBar: AppBar(title: const Text('Add always-allow entry')),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          TextField(
            key: const ValueKey('username_field'),
            controller: _username,
            autofocus: true,
            autocorrect: false,
            enableSuggestions: false,
            textInputAction: TextInputAction.next,
            decoration: InputDecoration(
              labelText: 'Username',
              helperText: 'Unix account on the server',
              errorText: _usernameError,
              border: const OutlineInputBorder(),
            ),
          ),
          const SizedBox(height: 16),
          Text('Context', style: theme.textTheme.labelLarge),
          const SizedBox(height: 8),
          SegmentedButton<RequestContext>(
            key: const ValueKey('context_choice'),
            showSelectedIcon: false,
            segments: const [
              ButtonSegment(
                value: RequestContext.ssh,
                label: Text('ssh'),
                icon: Icon(Icons.terminal),
              ),
              ButtonSegment(
                value: RequestContext.sudo,
                label: Text('sudo'),
                icon: Icon(Icons.admin_panel_settings_outlined),
              ),
            ],
            selected: {_requestContext},
            onSelectionChanged: (choice) => setState(() => _requestContext = choice.first),
          ),
          const SizedBox(height: 16),
          TextField(
            key: const ValueKey('server_field'),
            controller: _server,
            autocorrect: false,
            enableSuggestions: false,
            textInputAction: TextInputAction.done,
            onSubmitted: (_) => _submit(),
            decoration: InputDecoration(
              labelText: 'Server',
              hintText: 'empty = every server',
              helperText: 'Name given at enrollment, for instance web-01',
              errorText: _serverError,
              border: const OutlineInputBorder(),
            ),
          ),
          const SizedBox(height: 16),
          Text('Duration', style: theme.textTheme.labelLarge),
          const SizedBox(height: 8),
          Wrap(
            spacing: 8,
            runSpacing: 4,
            children: [
              _durationChip(_Duration.oneHour, label: '1 hour', keyName: 'ttl_1h'),
              _durationChip(_Duration.oneDay, label: '24 hours', keyName: 'ttl_24h'),
              _durationChip(
                _Duration.custom,
                label: custom == null ? 'Custom' : 'Custom (${custom.label})',
                keyName: 'ttl_custom',
              ),
              _durationChip(_Duration.permanent, label: 'Permanent', keyName: 'ttl_permanent'),
            ],
          ),
          const SizedBox(height: 16),
          Text(
            _summary,
            style: theme.textTheme.bodyMedium?.copyWith(
              color: _ttl.isPermanent ? theme.colorScheme.error : theme.colorScheme.onSurfaceVariant,
            ),
          ),
          const SizedBox(height: 24),
          FilledButton.icon(
            key: const ValueKey('submit_entry'),
            onPressed: _submitting ? null : _submit,
            icon: _submitting
                ? const SizedBox(
                    width: 18,
                    height: 18,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Icon(Icons.check),
            label: const Text('Add entry'),
          ),
        ],
      ),
    );
  }
}
