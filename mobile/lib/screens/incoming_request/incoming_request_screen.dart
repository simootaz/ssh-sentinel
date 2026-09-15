import 'package:flutter/material.dart';

import '../../models/enums.dart';
import '../../models/incoming_request.dart';
import '../../models/verdict_result.dart';
import '../../services/api_client.dart';
import '../../services/api_exceptions.dart';
import '../../services/pending_requests.dart';
import '../../widgets/countdown.dart';
import '../../widgets/format.dart';
import '../../widgets/status_views.dart';
import '../../widgets/ttl_picker.dart';

/// Full-screen page for one request: details, countdown and the three
/// verdict buttons. The push service pushes it on a foreground push or when
/// the notification is tapped.
///
/// The request ends in one of three ways: our verdict was accepted, someone
/// else decided first (a 409 from `POST /verdict`, or a `request_decided`
/// push relayed through [PendingRequests]), or the window ran out. In every
/// case the buttons go away and an outcome panel says what happened.
class IncomingRequestScreen extends StatefulWidget {
  const IncomingRequestScreen({
    super.key,
    required this.request,
    required this.api,
    required this.pending,
    this.deviceId,
    this.openTtlPicker = false,
  });

  final IncomingRequest request;
  final SentinelApi api;
  final PendingRequests pending;

  /// Id from `POST /devices`, sent with the verdict so history names this phone.
  final String? deviceId;

  /// True when opened from the notification's "Always allow" action: the TTL
  /// picker opens as soon as the page is on screen.
  final bool openTtlPicker;

  @override
  State<IncomingRequestScreen> createState() => _IncomingRequestScreenState();
}

class _IncomingRequestScreenState extends State<IncomingRequestScreen> {
  /// True while `POST /verdict` is in flight.
  bool _sending = false;

  /// Set when the countdown ran out, or when the request was already over
  /// when the screen opened (a push delivered late).
  bool _expired = false;

  /// How the request ended. Non-null replaces the buttons with the outcome
  /// panel, whoever decided.
  DecisionOutcome? _outcome;

  /// The 200 answer to our own verdict, for the whitelist entry and the
  /// auto-block details.
  VerdictResult? _result;

  /// The 409 body: another phone was faster, or the request timed out.
  AlreadyDecided? _conflict;

  IncomingRequest get _request => widget.request;

  /// True while a verdict can still be sent.
  bool get _canDecide => !_request.notice && _outcome == null && !_expired;

  @override
  void initState() {
    super.initState();
    _expired = _request.isExpired();
    // The outcome may be known before the screen opens: a request_decided
    // push that arrived first, or a notification tapped after the decision.
    _outcome = widget.pending.outcome(_request.id);
    widget.pending.addListener(_onPendingChanged);
    if (widget.openTtlPicker) {
      // Wait for the first frame: a bottom sheet needs the page to exist.
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && _canDecide) _alwaysAllow();
      });
    }
  }

  @override
  void didUpdateWidget(covariant IncomingRequestScreen oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.pending != widget.pending) {
      oldWidget.pending.removeListener(_onPendingChanged);
      widget.pending.addListener(_onPendingChanged);
    }
  }

  @override
  void dispose() {
    widget.pending.removeListener(_onPendingChanged);
    super.dispose();
  }

  /// A `request_decided` push, or a verdict sent from the notification,
  /// recorded the outcome elsewhere in the app.
  void _onPendingChanged() {
    if (_outcome != null) return;
    final outcome = widget.pending.outcome(_request.id);
    if (outcome == null) return;
    setState(() => _outcome = outcome);
  }

  void _onExpired() {
    if (!mounted || _expired) return;
    setState(() => _expired = true);
  }

  Future<void> _alwaysAllow() async {
    final choice = await showTtlPicker(context);
    // The sheet may have stayed open past the decision or the expiry.
    if (choice == null || !mounted || !_canDecide) return;
    await _send(Verdict.approveAlways, ttlSeconds: choice.seconds);
  }

  Future<void> _send(Verdict verdict, {int? ttlSeconds}) async {
    if (_sending) return;
    setState(() => _sending = true);
    final id = _request.id;
    try {
      final result = await widget.api.sendVerdict(
        requestId: id,
        verdict: verdict,
        ttlSeconds: ttlSeconds,
        deviceId: widget.deviceId,
      );
      final outcome = DecisionOutcome(
        status: result.status,
        decidedByDevice: result.decidedByDevice,
        decidedAt: result.decidedAt,
        mine: true,
      );
      // Tells the push service to dismiss the notification.
      widget.pending.markDecided(id, outcome);
      if (!mounted) return;
      setState(() {
        _result = result;
        _outcome = outcome;
      });
    } on AlreadyDecidedException catch (e) {
      // Another phone was faster, or the window ran out. The backend's
      // answer is final: show it, never retry.
      final outcome = DecisionOutcome(
        status: e.decision.status,
        decidedByDevice: e.decision.decidedByDevice,
        decidedAt: e.decision.decidedAt,
      );
      widget.pending.markDecided(id, outcome);
      if (!mounted) return;
      setState(() {
        _conflict = e.decision;
        _outcome = outcome;
      });
    } catch (e) {
      // Network or backend trouble: say so and leave the buttons enabled,
      // a retry is fine while the countdown runs.
      if (!mounted) return;
      showSnack(context, describeError(e), error: true);
    } finally {
      if (mounted) setState(() => _sending = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final request = _request;
    final scheme = Theme.of(context).colorScheme;
    final expired = _expired || request.isExpired();
    return Scaffold(
      appBar: AppBar(title: Text(request.title)),
      body: SafeArea(
        child: Column(
          children: [
            Expanded(
              child: ListView(
                padding: const EdgeInsets.all(16),
                children: [
                  _Header(request: request),
                  const SizedBox(height: 16),
                  _DetailRow(label: 'Server', value: request.server),
                  _DetailRow(label: 'Source', value: request.sourceLabel),
                  _DetailRow(
                      label: 'Location', value: request.geoLabel ?? 'unknown'),
                  _DetailRow(
                      label: 'Requested', value: formatTime(request.createdAt)),
                  if (request.isSudo) ...[
                    const SizedBox(height: 12),
                    _CommandBox(command: request.command),
                  ],
                  const SizedBox(height: 20),
                  if (request.notice)
                    const _InfoLine(
                      icon: Icons.info_outline,
                      text: 'Notify only, nothing to decide',
                    )
                  else ...[
                    CountdownBar(
                      expiresAt: request.expiresAt,
                      startedAt: request.createdAt,
                      onExpired: _onExpired,
                    ),
                    if (expired && _outcome == null) ...[
                      const SizedBox(height: 12),
                      _InfoLine(
                        icon: Icons.timer_off_outlined,
                        text: 'Expired, nobody answered in time',
                        color: scheme.error,
                      ),
                    ],
                  ],
                ],
              ),
            ),
            if (!request.notice)
              Padding(
                padding: const EdgeInsets.fromLTRB(16, 8, 16, 16),
                child: _outcome != null
                    ? _OutcomePanel(
                        outcome: _outcome!,
                        result: _result,
                        conflict: _conflict,
                        onClose: () => Navigator.of(context).maybePop(),
                      )
                    : _buildButtons(context, enabled: !expired && !_sending),
              ),
          ],
        ),
      ),
    );
  }

  Widget _buildButtons(BuildContext context, {required bool enabled}) {
    final scheme = Theme.of(context).colorScheme;
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        if (_sending) ...[
          const LinearProgressIndicator(key: ValueKey('sending_indicator')),
          const SizedBox(height: 12),
        ],
        Row(
          children: [
            Expanded(
              child: FilledButton.icon(
                key: const ValueKey('deny_button'),
                onPressed: enabled ? () => _send(Verdict.deny) : null,
                style: FilledButton.styleFrom(
                  backgroundColor: scheme.error,
                  foregroundColor: scheme.onError,
                ),
                icon: const Icon(Icons.block),
                label: const Text('Deny'),
              ),
            ),
            const SizedBox(width: 12),
            Expanded(
              child: FilledButton.icon(
                key: const ValueKey('approve_button'),
                onPressed: enabled ? () => _send(Verdict.approve) : null,
                icon: const Icon(Icons.check),
                label: const Text('Approve'),
              ),
            ),
          ],
        ),
        const SizedBox(height: 12),
        FilledButton.tonalIcon(
          key: const ValueKey('always_button'),
          onPressed: enabled ? _alwaysAllow : null,
          icon: const Icon(Icons.verified_user_outlined),
          label: const Text('Always allow'),
        ),
      ],
    );
  }
}

/// Context badge next to the username.
class _Header extends StatelessWidget {
  const _Header({required this.request});

  final IncomingRequest request;

  @override
  Widget build(BuildContext context) {
    return Row(
      children: [
        _ContextBadge(sudo: request.isSudo),
        const SizedBox(width: 12),
        Expanded(
          child: Text(
            request.username,
            style: Theme.of(context).textTheme.headlineSmall,
            overflow: TextOverflow.ellipsis,
          ),
        ),
      ],
    );
  }
}

/// "sudo" or "ssh", large. sudo has its own colour so it is never mistaken
/// for a login.
class _ContextBadge extends StatelessWidget {
  const _ContextBadge({required this.sudo});

  final bool sudo;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final color = sudo ? Colors.deepOrange : theme.colorScheme.primary;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 6),
      decoration: BoxDecoration(
        color: color,
        borderRadius: BorderRadius.circular(16),
      ),
      child: Text(
        sudo ? 'sudo' : 'ssh',
        style: theme.textTheme.titleMedium?.copyWith(
          color: Colors.white,
          fontWeight: FontWeight.bold,
          letterSpacing: 1,
        ),
      ),
    );
  }
}

/// One "label: value" line of the details.
class _DetailRow extends StatelessWidget {
  const _DetailRow({required this.label, required this.value});

  final String label;
  final String value;

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
              style: theme.textTheme.bodyMedium
                  ?.copyWith(color: theme.colorScheme.onSurfaceVariant),
            ),
          ),
          Expanded(child: Text(value, style: theme.textTheme.bodyLarge)),
        ],
      ),
    );
  }
}

/// The sudo command line in a monospace box.
class _CommandBox extends StatelessWidget {
  const _CommandBox({required this.command});

  final String? command;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: theme.colorScheme.surfaceContainerHighest,
        borderRadius: BorderRadius.circular(8),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            'Command',
            style: theme.textTheme.labelMedium
                ?.copyWith(color: theme.colorScheme.onSurfaceVariant),
          ),
          const SizedBox(height: 4),
          Text(
            command ?? 'command not captured',
            style: TextStyle(
              fontFamily: 'monospace',
              fontSize: 15,
              fontStyle: command == null ? FontStyle.italic : FontStyle.normal,
              color: theme.colorScheme.onSurface,
            ),
          ),
        ],
      ),
    );
  }
}

/// Icon and one line of text, for the notice and expiry messages.
class _InfoLine extends StatelessWidget {
  const _InfoLine({required this.icon, required this.text, this.color});

  final IconData icon;
  final String text;
  final Color? color;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final tint = color ?? theme.colorScheme.onSurfaceVariant;
    return Row(
      children: [
        Icon(icon, size: 20, color: tint),
        const SizedBox(width: 8),
        Expanded(
          child: Text(
            text,
            style: theme.textTheme.bodyLarge
                ?.copyWith(color: tint, fontWeight: FontWeight.w600),
          ),
        ),
      ],
    );
  }
}

/// Replaces the buttons once the request is over: who decided, the whitelist
/// entry for an always-allow, the auto-block when a denial reached the
/// threshold, and a Close button.
class _OutcomePanel extends StatelessWidget {
  const _OutcomePanel({
    required this.outcome,
    required this.onClose,
    this.result,
    this.conflict,
  });

  final DecisionOutcome outcome;
  final VerdictResult? result;
  final AlreadyDecided? conflict;
  final VoidCallback onClose;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final timedOut = outcome.status == RequestStatus.timeout;
    final allowed = outcome.status.isAllowed;
    final color = timedOut
        ? scheme.outline
        : allowed
            ? Colors.green
            : scheme.error;
    final icon = timedOut
        ? Icons.timer_off_outlined
        : allowed
            ? Icons.check_circle_outline
            : Icons.block;
    final entry = result?.whitelistEntry;
    final blocked = result?.autoBlocked;
    final lines = <String>[
      // After a 409 the headline says we were late; this line says what the
      // winner decided.
      if (conflict != null && !conflict!.isTimeout) outcome.description,
      if (entry != null) _whitelistSummary(entry),
      if (blocked != null)
        '${blocked.ip} is now blocked after ${blocked.denialCount} denials',
    ];
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: scheme.surfaceContainerHighest,
        borderRadius: BorderRadius.circular(12),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        mainAxisSize: MainAxisSize.min,
        children: [
          Row(
            children: [
              Icon(icon, color: color, size: 28),
              const SizedBox(width: 10),
              Expanded(
                child: Text(
                  conflict != null
                      ? _conflictHeadline(conflict!)
                      : outcome.description,
                  style: theme.textTheme.titleMedium
                      ?.copyWith(color: color, fontWeight: FontWeight.bold),
                ),
              ),
            ],
          ),
          for (final line in lines) ...[
            const SizedBox(height: 8),
            Text(line, style: theme.textTheme.bodyMedium),
          ],
          const SizedBox(height: 16),
          FilledButton.tonal(
            key: const ValueKey('close_button'),
            onPressed: onClose,
            child: const Text('Close'),
          ),
        ],
      ),
    );
  }

  /// "Request already decided by Pixel 8" or "Expired before anyone answered".
  static String _conflictHeadline(AlreadyDecided decision) {
    if (decision.isTimeout) return _capitalise(decision.description);
    return 'Request ${decision.description}';
  }

  /// "deploy whitelisted for ssh on web-01, expires 2026-09-14 22:12:07".
  static String _whitelistSummary(VerdictWhitelistEntry entry) {
    final where = entry.server ?? 'every server';
    final until = entry.expiresAt == null
        ? 'permanently'
        : 'expires ${formatDateTime(entry.expiresAt)}';
    return '${entry.username} whitelisted for ${contextLabel(entry.context)} '
        'on $where, $until';
  }

  static String _capitalise(String text) =>
      text.isEmpty ? text : text[0].toUpperCase() + text.substring(1);
}
