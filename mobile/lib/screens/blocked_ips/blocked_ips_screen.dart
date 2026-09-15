import 'package:flutter/material.dart';

import '../../models/blocked_ip.dart';
import '../../services/api_client.dart';
import '../../services/api_exceptions.dart';
import '../../widgets/format.dart';
import '../../widgets/status_views.dart';

/// IPs the backend auto-blocked after repeated admin denials
/// (`GET /blocked-ips`), with an unblock button (`DELETE /blocked-ips/{id}`).
///
/// Requests from a blocked IP are refused without a push until an admin
/// unblocks it here. Lives in the home shell's IndexedStack, so it brings its
/// own Scaffold and AppBar.
class BlockedIpsScreen extends StatefulWidget {
  const BlockedIpsScreen({super.key, required this.api});

  final SentinelApi api;

  @override
  State<BlockedIpsScreen> createState() => _BlockedIpsScreenState();
}

class _BlockedIpsScreenState extends State<BlockedIpsScreen> {
  static const _explanation = 'Blocked after repeated admin denials. '
      'Requests from these IPs are refused without a push.';
  static const _emptyMessage =
      'No blocked IPs. An IP is blocked after repeated denials.';

  /// Null until the first load succeeds. Together with [_error] this gives
  /// the three states: loading (both null), error (list null), list.
  List<BlockedIp>? _items;
  Object? _error;

  /// Ids with an unblock call in flight, to disable their button meanwhile.
  final Set<String> _busy = {};

  /// Lets the AppBar refresh button spin the pull-to-refresh indicator.
  final _refreshKey = GlobalKey<RefreshIndicatorState>();

  @override
  void initState() {
    super.initState();
    _load();
  }

  /// Fetches the list. Used by the first load, retry, pull-to-refresh and
  /// the AppBar button.
  Future<void> _load() async {
    final hadData = _items != null;
    // Retry after an error: back to the spinner while the call runs.
    if (!hadData && _error != null) setState(() => _error = null);
    try {
      final items = await widget.api.listBlockedIps();
      if (!mounted) return;
      setState(() => _items = items);
    } catch (e) {
      if (!mounted) return;
      if (hadData) {
        // The list on screen is still useful: keep it and tell the user.
        showSnack(context, describeError(e), error: true);
      } else {
        setState(() => _error = e);
      }
    }
  }

  /// AppBar button: spins the indicator when the list is on screen, plain
  /// reload otherwise (error view, where there is no indicator).
  Future<void> _refresh() => _refreshKey.currentState?.show() ?? _load();

  Future<void> _unblock(BlockedIp item) async {
    final confirmed = await confirmDialog(
      context,
      title: 'Unblock ${item.ip}?',
      message: 'The next login from ${item.ip} will be pushed to the phones '
          'again. Three new denials block it again.',
      confirmLabel: 'Unblock',
    );
    if (!confirmed || !mounted) return;

    setState(() => _busy.add(item.id));
    try {
      await widget.api.unblockIp(item.id);
      if (!mounted) return;
      _removeRow(item.id);
      showSnack(context, '${item.ip} unblocked');
    } on NotFoundException {
      // Already gone: another phone unblocked it or the block expired.
      // Drop the stale row and reload to pick up any other change.
      if (!mounted) return;
      _removeRow(item.id);
      showSnack(context, '${item.ip} was already unblocked');
      await _load();
    } catch (e) {
      if (!mounted) return;
      showSnack(context, describeError(e), error: true);
    } finally {
      if (mounted) setState(() => _busy.remove(item.id));
    }
  }

  void _removeRow(String id) {
    final items = _items;
    if (items == null) return;
    setState(() => _items = items.where((b) => b.id != id).toList());
  }

  @override
  Widget build(BuildContext context) {
    final items = _items;
    final error = _error;
    final loading = items == null && error == null;

    return Scaffold(
      appBar: AppBar(
        title: const Text('Blocked IPs'),
        actions: [
          IconButton(
            icon: const Icon(Icons.refresh),
            tooltip: 'Refresh',
            onPressed: loading ? null : _refresh,
          ),
        ],
      ),
      body: items == null
          ? (error == null
              ? const LoadingView()
              : ErrorView(error: error, onRetry: _load))
          : RefreshIndicator(
              key: _refreshKey,
              onRefresh: _load,
              child: items.isEmpty ? _buildEmpty() : _buildList(items),
            ),
    );
  }

  /// RefreshIndicator needs something scrollable under the finger, so the
  /// empty placeholder is wrapped in a scroll view that fills the screen.
  Widget _buildEmpty() => LayoutBuilder(
        builder: (context, constraints) => SingleChildScrollView(
          physics: const AlwaysScrollableScrollPhysics(),
          child: SizedBox(
            height: constraints.maxHeight,
            child: const EmptyView(message: _emptyMessage, icon: Icons.block),
          ),
        ),
      );

  Widget _buildList(List<BlockedIp> items) {
    final theme = Theme.of(context);
    return ListView(
      // Blocked IP lists are short; plain children keep the code obvious.
      physics: const AlwaysScrollableScrollPhysics(),
      padding: const EdgeInsets.only(bottom: 16),
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 12, 16, 4),
          child: Text(
            _explanation,
            style: theme.textTheme.bodyMedium
                ?.copyWith(color: theme.colorScheme.onSurfaceVariant),
          ),
        ),
        for (final item in items)
          _BlockedIpCard(
            item: item,
            busy: _busy.contains(item.id),
            onUnblock: () => _unblock(item),
          ),
      ],
    );
  }
}

/// One blocked IP: address, reason, counts, dates and the unblock button.
class _BlockedIpCard extends StatelessWidget {
  const _BlockedIpCard({
    required this.item,
    required this.busy,
    required this.onUnblock,
  });

  final BlockedIp item;
  final bool busy;
  final VoidCallback onUnblock;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final lastHit = item.lastHitAt;
    return Card(
      margin: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              crossAxisAlignment: CrossAxisAlignment.center,
              children: [
                Expanded(
                  child: Text(
                    item.ip,
                    // Monospace keeps the octets aligned and easy to compare
                    // with a terminal or a log line.
                    style: theme.textTheme.headlineSmall?.copyWith(
                      fontFamily: 'monospace',
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                ),
                const SizedBox(width: 8),
                Tag(item.reason),
              ],
            ),
            const SizedBox(height: 8),
            Text(_count(item.denialCount, 'denial', 'denials')),
            Text('${_count(item.hitCount, 'attempt', 'attempts')} '
                'refused since the block'),
            const SizedBox(height: 8),
            _DetailLine('First denial', formatDateTime(item.firstDeniedAt)),
            _DetailLine('Last denial', formatDateTime(item.lastDeniedAt)),
            if (lastHit != null) _DetailLine('Last attempt', formatAgo(lastHit)),
            _DetailLine(
              'Blocked',
              item.expiresAt == null
                  ? 'until unblocked'
                  : formatExpiry(item.expiresAt),
            ),
            const SizedBox(height: 8),
            Align(
              alignment: Alignment.centerRight,
              child: OutlinedButton.icon(
                key: ValueKey('unblock_${item.id}'),
                onPressed: busy ? null : onUnblock,
                icon: const Icon(Icons.lock_open),
                label: const Text('Unblock'),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// "3 denials", "1 attempt".
String _count(int n, String singular, String plural) =>
    '$n ${n == 1 ? singular : plural}';

/// Label on the left, value on the right, so the dates line up.
class _DetailLine extends StatelessWidget {
  const _DetailLine(this.label, this.value);

  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.only(bottom: 2),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 104,
            child: Text(
              label,
              style: theme.textTheme.bodySmall
                  ?.copyWith(color: theme.colorScheme.onSurfaceVariant),
            ),
          ),
          Expanded(child: Text(value, style: theme.textTheme.bodyMedium)),
        ],
      ),
    );
  }
}
