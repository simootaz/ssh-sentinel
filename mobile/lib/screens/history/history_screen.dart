import 'package:flutter/material.dart';

import '../../models/access_request.dart';
import '../../models/enums.dart';
import '../../services/api_client.dart';
import '../../services/api_exceptions.dart';
import '../../widgets/format.dart';
import '../../widgets/status_views.dart';

/// Colour of the "sudo" tag, so a sudo row stands out from an ssh row.
const Color _sudoColor = Colors.deepOrange;

/// Past requests (`GET /history`), newest first, with filters and paging.
///
/// Lives in the home shell's IndexedStack, so it brings its own Scaffold and
/// AppBar. One row per request; a tap opens the full record.
class HistoryScreen extends StatefulWidget {
  const HistoryScreen({super.key, required this.api});

  final SentinelApi api;

  @override
  State<HistoryScreen> createState() => _HistoryScreenState();
}

class _HistoryScreenState extends State<HistoryScreen> {
  /// Rows per call: the contract's default (the backend caps at 200).
  static const _pageSize = 50;

  // Loaded data.
  List<AccessRequest> _items = [];
  String? _nextBefore;
  bool _loading = true;
  bool _loadingMore = false;
  Object? _error;

  /// Incremented on every first-page load. A reply whose number is no longer
  /// current is dropped, so a slow answer to an old filter never overwrites
  /// the newer list.
  int _loadSeq = 0;

  // Applied filters. Null or empty means "no filter". The text fields keep
  // their own draft in the controllers until Apply (or the keyboard's search
  // key) copies it here; the segmented button and the dropdown apply at once.
  bool _filtersOpen = false;
  RequestContext? _context;
  RequestStatus? _status;
  String _server = '';
  String _username = '';
  final _serverCtl = TextEditingController();
  final _usernameCtl = TextEditingController();

  int get _activeFilterCount => [
        _context != null,
        _status != null,
        _server.isNotEmpty,
        _username.isNotEmpty,
      ].where((active) => active).length;

  @override
  void initState() {
    super.initState();
    _loadFirstPage();
  }

  @override
  void dispose() {
    _serverCtl.dispose();
    _usernameCtl.dispose();
    super.dispose();
  }

  Future<HistoryPage> _fetch({String? before}) => widget.api.history(
        limit: _pageSize,
        before: before,
        server: _server.isEmpty ? null : _server,
        username: _username.isEmpty ? null : _username,
        context: _context,
        status: _status,
      );

  /// First page with the current filters: on start, pull-to-refresh, Retry
  /// and every filter change.
  Future<void> _loadFirstPage() async {
    final seq = ++_loadSeq;
    setState(() {
      _loading = true;
      _loadingMore = false;
      _error = null;
    });
    try {
      final page = await _fetch();
      if (!mounted) return;
      if (seq != _loadSeq) return;
      setState(() {
        _items = page.items;
        _nextBefore = page.nextBefore;
        _loading = false;
      });
    } catch (e) {
      if (!mounted) return;
      if (seq != _loadSeq) return;
      setState(() {
        _items = [];
        _nextBefore = null;
        _loading = false;
        _error = e;
      });
    }
  }

  /// Next page, appended to the list. An error goes to a snack so the rows
  /// already loaded stay usable.
  Future<void> _loadMore() async {
    final before = _nextBefore;
    if (before == null || _loadingMore || _loading) return;
    final seq = _loadSeq;
    setState(() => _loadingMore = true);
    try {
      final page = await _fetch(before: before);
      if (!mounted) return;
      if (seq != _loadSeq) return;
      setState(() {
        _items = [..._items, ...page.items];
        _nextBefore = page.nextBefore;
        _loadingMore = false;
      });
    } catch (e) {
      if (!mounted) return;
      if (seq != _loadSeq) return;
      setState(() => _loadingMore = false);
      showSnack(context, describeError(e), error: true);
    }
  }

  /// Copies the text fields into the applied filters and reloads from the
  /// first page. The other controls call this too, so whatever is typed is
  /// applied at the same time.
  void _applyFilters() {
    setState(() {
      _server = _serverCtl.text.trim();
      _username = _usernameCtl.text.trim();
    });
    _loadFirstPage();
  }

  void _clearFilters() {
    _serverCtl.clear();
    _usernameCtl.clear();
    setState(() {
      _context = null;
      _status = null;
      _server = '';
      _username = '';
    });
    _loadFirstPage();
  }

  void _showDetails(AccessRequest request) {
    showModalBottomSheet<void>(
      context: context,
      showDragHandle: true,
      builder: (_) => _RequestDetailsSheet(request: request),
    );
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('History'),
        actions: [
          IconButton(
            key: const ValueKey('filter_toggle'),
            tooltip: 'Filters',
            onPressed: () => setState(() => _filtersOpen = !_filtersOpen),
            icon: Badge(
              key: const ValueKey('filter_count'),
              isLabelVisible: _activeFilterCount > 0,
              label: Text('$_activeFilterCount'),
              child: const Icon(Icons.filter_list),
            ),
          ),
        ],
      ),
      body: Column(
        children: [
          if (_filtersOpen) _buildFilterPanel(context),
          Expanded(child: _buildBody()),
        ],
      ),
    );
  }

  Widget _buildFilterPanel(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Material(
      color: scheme.surfaceContainerLow,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 12, 16, 12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            SegmentedButton<RequestContext?>(
              key: const ValueKey('context_filter'),
              showSelectedIcon: false,
              segments: const [
                ButtonSegment(value: null, label: Text('All')),
                ButtonSegment(value: RequestContext.ssh, label: Text('ssh')),
                ButtonSegment(value: RequestContext.sudo, label: Text('sudo')),
              ],
              selected: {_context},
              onSelectionChanged: (selection) {
                setState(() => _context = selection.first);
                _applyFilters();
              },
            ),
            const SizedBox(height: 12),
            DropdownButtonFormField<RequestStatus?>(
              key: const ValueKey('status_filter'),
              value: _status,
              isExpanded: true,
              hint: const Text('All statuses'),
              decoration: const InputDecoration(
                labelText: 'Status',
                isDense: true,
                border: OutlineInputBorder(),
              ),
              items: [
                const DropdownMenuItem(value: null, child: Text('All statuses')),
                // `unknown` is the parser's fallback, not a value the backend
                // accepts as a filter, so it is left out.
                for (final status in RequestStatus.values)
                  if (status != RequestStatus.unknown)
                    DropdownMenuItem(value: status, child: Text(statusLabel(status))),
              ],
              onChanged: (status) {
                setState(() => _status = status);
                _applyFilters();
              },
            ),
            const SizedBox(height: 12),
            Row(
              children: [
                Expanded(
                  child: TextField(
                    key: const ValueKey('server_filter'),
                    controller: _serverCtl,
                    textInputAction: TextInputAction.search,
                    onSubmitted: (_) => _applyFilters(),
                    decoration: const InputDecoration(
                      labelText: 'Server',
                      isDense: true,
                      border: OutlineInputBorder(),
                    ),
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: TextField(
                    key: const ValueKey('username_filter'),
                    controller: _usernameCtl,
                    textInputAction: TextInputAction.search,
                    onSubmitted: (_) => _applyFilters(),
                    decoration: const InputDecoration(
                      labelText: 'Username',
                      isDense: true,
                      border: OutlineInputBorder(),
                    ),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 8),
            Row(
              mainAxisAlignment: MainAxisAlignment.end,
              children: [
                TextButton(
                  key: const ValueKey('clear_filters'),
                  onPressed: _clearFilters,
                  child: const Text('Clear'),
                ),
                const SizedBox(width: 8),
                FilledButton.tonal(
                  key: const ValueKey('apply_filters'),
                  onPressed: _applyFilters,
                  child: const Text('Apply'),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildBody() {
    final error = _error;
    if (error != null) {
      return ErrorView(error: error, onRetry: _loadFirstPage);
    }
    if (_loading && _items.isEmpty) return const LoadingView();
    if (_items.isEmpty) {
      // Wrapped in a scrollable so pull-to-refresh also works on an empty list.
      return RefreshIndicator(
        onRefresh: _loadFirstPage,
        child: const CustomScrollView(
          physics: AlwaysScrollableScrollPhysics(),
          slivers: [
            SliverFillRemaining(
              hasScrollBody: false,
              child: EmptyView(message: 'No requests yet', icon: Icons.history),
            ),
          ],
        ),
      );
    }
    final hasMore = _nextBefore != null;
    return RefreshIndicator(
      onRefresh: _loadFirstPage,
      child: ListView.builder(
        physics: const AlwaysScrollableScrollPhysics(),
        // One extra slot at the end for the Load more button.
        itemCount: _items.length + (hasMore ? 1 : 0),
        itemBuilder: (context, index) {
          if (index == _items.length) return _buildLoadMore();
          final request = _items[index];
          return _RequestTile(
            request: request,
            onTap: () => _showDetails(request),
          );
        },
      ),
    );
  }

  Widget _buildLoadMore() {
    return Padding(
      padding: const EdgeInsets.all(16),
      child: Center(
        child: _loadingMore
            ? const SizedBox(
                width: 24,
                height: 24,
                child: CircularProgressIndicator(strokeWidth: 2),
              )
            : OutlinedButton.icon(
                key: const ValueKey('load_more'),
                onPressed: _loadMore,
                icon: const Icon(Icons.expand_more),
                label: const Text('Load more'),
              ),
      ),
    );
  }
}

/// Green when the login or command went through, red when it was refused,
/// amber while the request is still waiting for a verdict.
Color _outcomeColor(RequestStatus status, ColorScheme scheme) {
  if (status.isAllowed) return Colors.green.shade600;
  if (status.isRefused) return Colors.red.shade600;
  if (status == RequestStatus.pending) return Colors.amber.shade800;
  return scheme.outline;
}

IconData _outcomeIcon(RequestStatus status) {
  if (status.isAllowed) return Icons.check_circle_outline;
  if (status.isRefused) return Icons.block;
  if (status == RequestStatus.pending) return Icons.hourglass_top;
  return Icons.help_outline;
}

String _decidedByLabel(DecidedBy? by) => switch (by) {
      null => '-',
      DecidedBy.admin => 'admin',
      DecidedBy.whitelist => 'whitelist',
      DecidedBy.timeout => 'timeout',
      DecidedBy.autoblock => 'auto-block',
      DecidedBy.georule => 'geo rule',
      DecidedBy.unknown => 'unknown',
    };

/// One row of the list.
class _RequestTile extends StatelessWidget {
  const _RequestTile({required this.request, required this.onTap});

  final AccessRequest request;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final r = request;
    final outcomeColor = _outcomeColor(r.status, theme.colorScheme);
    final geo = r.geo;
    final source = geo == null ? r.sourceLabel : '${r.sourceLabel} · ${geo.label}';
    final command = r.command;

    return ListTile(
      key: ValueKey('request_${r.id}'),
      onTap: onTap,
      isThreeLine: true,
      leading: Icon(_outcomeIcon(r.status), color: outcomeColor),
      title: Row(
        children: [
          Tag(contextLabel(r.context), color: r.isSudo ? _sudoColor : null),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              '${r.username} on ${r.server}',
              overflow: TextOverflow.ellipsis,
            ),
          ),
        ],
      ),
      subtitle: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(source),
          if (command != null)
            Text(
              command,
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
              style: const TextStyle(fontFamily: 'monospace'),
            ),
          // Wrap instead of Row: a long outcome and the timestamp fold onto
          // two lines on a narrow phone rather than overflowing.
          Wrap(
            spacing: 8,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: [
              Text(
                outcomeLabel(
                  status: r.status,
                  decidedBy: r.decidedBy,
                  decidedByDevice: r.decidedByDevice,
                ),
                style: TextStyle(color: outcomeColor, fontWeight: FontWeight.w600),
              ),
              Text(formatDateTime(r.createdAt), style: theme.textTheme.bodySmall),
            ],
          ),
        ],
      ),
    );
  }
}

/// Bottom sheet with every field of a request. Text is selectable so an id
/// or a command can be copied.
class _RequestDetailsSheet extends StatelessWidget {
  const _RequestDetailsSheet({required this.request});

  final AccessRequest request;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final r = request;
    final geo = r.geo;
    final asn = geo?.asn;
    final geoDetail = geo == null
        ? '-'
        : asn == null
            ? geo.label
            : '${geo.label} · $asn';
    final rows = <(String, String)>[
      ('id', r.id),
      ('server', r.server),
      ('hostname', r.hostname),
      ('context', contextLabel(r.context)),
      ('username', r.username),
      ('source', r.sourceLabel),
      ('tty', r.tty ?? '-'),
      ('command', r.command ?? '-'),
      ('geo', geoDetail),
      ('status', statusLabel(r.status)),
      ('decided by', _decidedByLabel(r.decidedBy)),
      ('device', r.decidedByDevice ?? '-'),
      ('created', formatDateTime(r.createdAt)),
      ('expires', formatDateTime(r.expiresAt)),
      ('decided', formatDateTime(r.decidedAt)),
    ];

    return SafeArea(
      child: SingleChildScrollView(
        padding: const EdgeInsets.fromLTRB(20, 0, 20, 20),
        child: SelectionArea(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Tag(contextLabel(r.context), color: r.isSudo ? _sudoColor : null),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Text('Request details', style: theme.textTheme.titleLarge),
                  ),
                ],
              ),
              const SizedBox(height: 4),
              Text(
                outcomeLabel(
                  status: r.status,
                  decidedBy: r.decidedBy,
                  decidedByDevice: r.decidedByDevice,
                ),
                style: TextStyle(
                  color: _outcomeColor(r.status, theme.colorScheme),
                  fontWeight: FontWeight.w600,
                ),
              ),
              const SizedBox(height: 12),
              for (final (label, value) in rows)
                _DetailRow(
                  label: label,
                  value: value,
                  mono: label == 'id' || label == 'command',
                ),
            ],
          ),
        ),
      ),
    );
  }
}

class _DetailRow extends StatelessWidget {
  const _DetailRow({required this.label, required this.value, this.mono = false});

  final String label;
  final String value;

  /// Monospace for ids and commands, where every character matters.
  final bool mono;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 4),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 96,
            child: Text(
              label,
              style: theme.textTheme.labelLarge
                  ?.copyWith(color: theme.colorScheme.onSurfaceVariant),
            ),
          ),
          Expanded(
            child: Text(
              value,
              style: mono ? const TextStyle(fontFamily: 'monospace') : null,
            ),
          ),
        ],
      ),
    );
  }
}
