import 'package:flutter/foundation.dart';

import '../models/enums.dart';
import '../models/incoming_request.dart';

/// How a request in memory ended: our own verdict, another phone's, a
/// `request_decided` push, or the timeout.
class DecisionOutcome {
  const DecisionOutcome({
    required this.status,
    this.decidedByDevice,
    this.decidedAt,
    this.mine = false,
  });

  final RequestStatus status;
  final String? decidedByDevice;
  final DateTime? decidedAt;

  /// True when this phone sent the winning verdict.
  final bool mine;

  /// "Approved by you", "Denied by Pixel 8", "Expired, nobody answered".
  String get description {
    if (status == RequestStatus.timeout) return 'Expired, nobody answered';
    final what = switch (status) {
      RequestStatus.approved => 'Approved',
      RequestStatus.denied => 'Denied',
      _ => 'Decided',
    };
    if (mine) return '$what by you';
    return '$what by ${decidedByDevice ?? 'another phone'}';
  }
}

/// A request received by push and what happened to it.
class PendingEntry {
  PendingEntry(this.request, {this.outcome});

  final IncomingRequest request;
  DecisionOutcome? outcome;

  bool get isDecided => outcome != null;
}

/// Requests received while the app runs, keyed by request id.
///
/// The incoming request screen listens to this to learn that another phone
/// decided (from a `request_decided` push or a 409), and the push service
/// listens to dismiss notifications.
class PendingRequests extends ChangeNotifier {
  final Map<String, PendingEntry> _entries = {};

  /// Entries are dropped this long after they expired.
  static const keepFor = Duration(minutes: 30);

  List<PendingEntry> get all => List.unmodifiable(_entries.values);

  /// Requests that still wait for a verdict from someone.
  List<PendingEntry> get undecided => _entries.values
      .where((e) => !e.isDecided && !e.request.isExpired())
      .toList();

  PendingEntry? entry(String requestId) => _entries[requestId];

  DecisionOutcome? outcome(String requestId) => _entries[requestId]?.outcome;

  /// Records a request. An outcome already known (a `request_decided` push
  /// that arrived first) is kept.
  PendingEntry add(IncomingRequest request) {
    final existing = _entries[request.id];
    if (existing != null) return existing;
    final entry = PendingEntry(request, outcome: _orphanOutcomes.remove(request.id));
    _entries[request.id] = entry;
    _prune();
    notifyListeners();
    return entry;
  }

  /// Records the outcome. Works for ids the store never saw, so a
  /// `request_decided` push arriving before its request is not lost.
  void markDecided(String requestId, DecisionOutcome outcome) {
    final entry = _entries[requestId];
    if (entry == null) {
      _orphanOutcomes[requestId] = outcome;
      return;
    }
    if (entry.outcome != null && !outcome.mine) return;
    entry.outcome = outcome;
    notifyListeners();
  }

  final Map<String, DecisionOutcome> _orphanOutcomes = {};

  void remove(String requestId) {
    if (_entries.remove(requestId) != null) notifyListeners();
  }

  void _prune() {
    final now = DateTime.now();
    _entries.removeWhere(
        (_, e) => now.difference(e.request.expiresAt) > keepFor);
  }
}
