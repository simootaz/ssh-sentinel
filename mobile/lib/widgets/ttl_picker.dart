import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'format.dart';

/// How long an always-allow entry lasts. [seconds] null means permanent.
class TtlChoice {
  const TtlChoice(this.seconds);
  const TtlChoice.permanent() : seconds = null;

  static const oneHour = TtlChoice(3600);
  static const oneDay = TtlChoice(86400);

  final int? seconds;

  bool get isPermanent => seconds == null;

  /// "1 h", "24 h", "3 d", "permanent".
  String get label => ttlLabel(seconds);
}

/// Opens the TTL picker as a bottom sheet: 1 hour, 24 hours, custom,
/// permanent. Returns null when dismissed.
Future<TtlChoice?> showTtlPicker(
  BuildContext context, {
  String title = 'Always allow for how long?',
}) =>
    showModalBottomSheet<TtlChoice>(
      context: context,
      showDragHandle: true,
      builder: (_) => TtlPickerSheet(title: title),
    );

/// The content of [showTtlPicker]. Pops with the chosen [TtlChoice].
class TtlPickerSheet extends StatelessWidget {
  const TtlPickerSheet({super.key, this.title = 'Always allow for how long?'});

  final String title;

  @override
  Widget build(BuildContext context) {
    return SafeArea(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(24, 0, 24, 8),
            child: Text(title, style: Theme.of(context).textTheme.titleMedium),
          ),
          ListTile(
            key: const ValueKey('ttl_1h'),
            leading: const Icon(Icons.hourglass_bottom),
            title: const Text('1 hour'),
            onTap: () => Navigator.of(context).pop(TtlChoice.oneHour),
          ),
          ListTile(
            key: const ValueKey('ttl_24h'),
            leading: const Icon(Icons.today),
            title: const Text('24 hours'),
            onTap: () => Navigator.of(context).pop(TtlChoice.oneDay),
          ),
          ListTile(
            key: const ValueKey('ttl_custom'),
            leading: const Icon(Icons.tune),
            title: const Text('Custom duration'),
            onTap: () async {
              final choice = await showCustomTtlDialog(context);
              if (choice != null && context.mounted) {
                Navigator.of(context).pop(choice);
              }
            },
          ),
          ListTile(
            key: const ValueKey('ttl_permanent'),
            leading: const Icon(Icons.all_inclusive),
            title: const Text('Permanent'),
            subtitle: const Text('Until removed from the whitelist'),
            onTap: () => Navigator.of(context).pop(const TtlChoice.permanent()),
          ),
          const SizedBox(height: 8),
        ],
      ),
    );
  }
}

/// Dialog with a number and a unit (minutes, hours, days).
Future<TtlChoice?> showCustomTtlDialog(BuildContext context) =>
    showDialog<TtlChoice>(
      context: context,
      builder: (_) => const CustomTtlDialog(),
    );

/// Content of [showCustomTtlDialog].
class CustomTtlDialog extends StatefulWidget {
  const CustomTtlDialog({super.key});

  @override
  State<CustomTtlDialog> createState() => _CustomTtlDialogState();
}

class _CustomTtlDialogState extends State<CustomTtlDialog> {
  static const _units = <String, int>{'minutes': 60, 'hours': 3600, 'days': 86400};

  final _controller = TextEditingController(text: '8');
  String _unit = 'hours';
  String? _error;

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  void _submit() {
    final amount = int.tryParse(_controller.text.trim());
    if (amount == null || amount <= 0) {
      setState(() => _error = 'Enter a number above zero.');
      return;
    }
    Navigator.of(context).pop(TtlChoice(amount * _units[_unit]!));
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text('Custom duration'),
      content: Row(
        children: [
          Expanded(
            child: TextField(
              key: const ValueKey('ttl_custom_value'),
              controller: _controller,
              autofocus: true,
              keyboardType: TextInputType.number,
              inputFormatters: [FilteringTextInputFormatter.digitsOnly],
              decoration: InputDecoration(labelText: 'Amount', errorText: _error),
              onSubmitted: (_) => _submit(),
            ),
          ),
          const SizedBox(width: 12),
          DropdownButton<String>(
            key: const ValueKey('ttl_custom_unit'),
            value: _unit,
            items: [
              for (final unit in _units.keys)
                DropdownMenuItem(value: unit, child: Text(unit)),
            ],
            onChanged: (value) => setState(() => _unit = value ?? _unit),
          ),
        ],
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          child: const Text('Cancel'),
        ),
        FilledButton(
          key: const ValueKey('ttl_custom_ok'),
          onPressed: _submit,
          child: const Text('OK'),
        ),
      ],
    );
  }
}
