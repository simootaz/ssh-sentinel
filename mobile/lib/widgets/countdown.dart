import 'dart:async';

import 'package:flutter/material.dart';

import '../models/incoming_request.dart';

/// Rebuilds [builder] a few times per second until [expiresAt] passes, then
/// calls [onExpired] once. The countdown is computed from the backend's
/// `expires_at`, never from when the push arrived.
class Countdown extends StatefulWidget {
  const Countdown({
    super.key,
    required this.expiresAt,
    required this.builder,
    this.onExpired,
    this.tick = const Duration(milliseconds: 250),
  });

  final DateTime expiresAt;
  final Widget Function(BuildContext context, Duration remaining) builder;
  final VoidCallback? onExpired;
  final Duration tick;

  @override
  State<Countdown> createState() => _CountdownState();
}

class _CountdownState extends State<Countdown> {
  Timer? _timer;
  bool _expiredNotified = false;

  Duration get _remaining {
    final left = widget.expiresAt.difference(DateTime.now());
    return left.isNegative ? Duration.zero : left;
  }

  @override
  void initState() {
    super.initState();
    _start();
  }

  @override
  void didUpdateWidget(covariant Countdown oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.expiresAt != widget.expiresAt) {
      _expiredNotified = false;
      _start();
    }
  }

  void _start() {
    _timer?.cancel();
    if (_remaining == Duration.zero) {
      // Already over: tell the parent after this build, not during it.
      WidgetsBinding.instance.addPostFrameCallback((_) => _notifyExpired());
      return;
    }
    _timer = Timer.periodic(widget.tick, (_) {
      if (!mounted) return;
      setState(() {});
      if (_remaining == Duration.zero) {
        _timer?.cancel();
        _notifyExpired();
      }
    });
  }

  void _notifyExpired() {
    if (_expiredNotified || !mounted) return;
    _expiredNotified = true;
    widget.onExpired?.call();
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => widget.builder(context, _remaining);
}

/// "23 s left" with a shrinking bar, red under ten seconds, "Expired" after.
class CountdownBar extends StatelessWidget {
  const CountdownBar({
    super.key,
    required this.expiresAt,
    this.startedAt,
    this.onExpired,
  });

  final DateTime expiresAt;

  /// Start of the window, for the bar's scale. Defaults to 30 s before expiry.
  final DateTime? startedAt;
  final VoidCallback? onExpired;

  @override
  Widget build(BuildContext context) {
    final total = expiresAt.difference(startedAt ?? expiresAt.subtract(requestWindow));
    return Countdown(
      expiresAt: expiresAt,
      onExpired: onExpired,
      builder: (context, remaining) {
        final scheme = Theme.of(context).colorScheme;
        final expired = remaining == Duration.zero;
        final seconds = (remaining.inMilliseconds / 1000).ceil();
        final fraction = total.inMilliseconds <= 0
            ? 0.0
            : (remaining.inMilliseconds / total.inMilliseconds).clamp(0.0, 1.0);
        final color = expired
            ? scheme.outline
            : seconds <= 10
                ? scheme.error
                : scheme.primary;
        return Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(Icons.timer_outlined, size: 18, color: color),
                const SizedBox(width: 6),
                Text(
                  expired ? 'Expired' : '$seconds s left',
                  style: TextStyle(
                    color: color,
                    fontWeight: FontWeight.w600,
                    fontFeatures: const [FontFeature.tabularFigures()],
                  ),
                ),
              ],
            ),
            const SizedBox(height: 6),
            ClipRRect(
              borderRadius: BorderRadius.circular(4),
              child: LinearProgressIndicator(
                value: fraction,
                color: color,
                backgroundColor: scheme.surfaceContainerHighest,
                minHeight: 6,
              ),
            ),
          ],
        );
      },
    );
  }
}
