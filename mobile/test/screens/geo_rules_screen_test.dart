// Geo rules screen against the fake API: list, empty and error states, the
// add form with its validation, and the remove flow.
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:ssh_sentinel/models/geo_rule.dart';
import 'package:ssh_sentinel/screens/geo_rules/geo_rules_screen.dart';
import 'package:ssh_sentinel/services/api_exceptions.dart';
import 'package:ssh_sentinel/widgets/format.dart';
import 'package:ssh_sentinel/widgets/status_views.dart';

import '../fakes/fake_api.dart';

final kpCreatedAt = DateTime.utc(2026, 9, 14, 18, 0);

GeoRule kpRule() =>
    GeoRule(id: 'geo-kp', country: 'KP', note: 'no staff there', createdAt: kpCreatedAt);

Future<FakeSentinelApi> pumpScreen(WidgetTester tester, {List<GeoRule>? rules}) async {
  final api = FakeSentinelApi();
  if (rules != null) api.geoRules = rules;
  await tester.pumpWidget(MaterialApp(home: GeoRulesScreen(api: api)));
  await tester.pumpAndSettle();
  return api;
}

Future<void> openAddDialog(WidgetTester tester) async {
  await tester.tap(find.byKey(const ValueKey('add_rule')));
  await tester.pumpAndSettle();
  expect(find.byKey(const ValueKey('submit_rule')), findsOneWidget);
}

Future<void> submitRule(WidgetTester tester, String code, {String? note}) async {
  await tester.enterText(find.byKey(const ValueKey('country_field')), code);
  if (note != null) {
    await tester.enterText(find.byKey(const ValueKey('note_field')), note);
  }
  await tester.tap(find.byKey(const ValueKey('submit_rule')));
  await tester.pumpAndSettle();
}

void main() {
  group('flagEmoji', () {
    test('two regional indicator letters', () {
      expect(flagEmoji('KP'), '\u{1F1F0}\u{1F1F5}');
      expect(flagEmoji('fr'), '\u{1F1EB}\u{1F1F7}');
    });

    test('empty for anything else', () {
      expect(flagEmoji(''), '');
      expect(flagEmoji('K'), '');
      expect(flagEmoji('K1'), '');
      expect(flagEmoji('FRA'), '');
    });
  });

  group('GeoRulesScreen', () {
    testWidgets('renders the rules with flag, code, note and date', (tester) async {
      await pumpScreen(tester, rules: [
        kpRule(),
        const GeoRule(id: 'geo-ru', country: 'RU'),
      ]);

      expect(find.text('Geo rules'), findsOneWidget);
      expect(find.text('KP'), findsOneWidget);
      expect(find.text('RU'), findsOneWidget);
      expect(find.text(flagEmoji('KP')), findsOneWidget);
      expect(find.text(flagEmoji('RU')), findsOneWidget);
      expect(find.text('no staff there'), findsOneWidget);
      expect(find.text('added ${formatDateTime(kpCreatedAt)}'), findsOneWidget);
      expect(find.byKey(const ValueKey('delete_geo-kp')), findsOneWidget);
      expect(find.byKey(const ValueKey('delete_geo-ru')), findsOneWidget);
      expect(find.textContaining('refused without a push'), findsOneWidget);
      expect(find.byType(EmptyView), findsNothing);
    });

    testWidgets('sorts the rules by code', (tester) async {
      await pumpScreen(tester, rules: const [
        GeoRule(id: 'geo-ru', country: 'RU'),
        GeoRule(id: 'geo-cn', country: 'CN'),
        GeoRule(id: 'geo-kp', country: 'KP'),
      ]);

      final cn = tester.getTopLeft(find.text('CN')).dy;
      final kp = tester.getTopLeft(find.text('KP')).dy;
      final ru = tester.getTopLeft(find.text('RU')).dy;
      expect(cn, lessThan(kp));
      expect(kp, lessThan(ru));
    });

    testWidgets('shows the empty state with the explanation', (tester) async {
      final api = await pumpScreen(tester);

      expect(api.calls, ['listGeoRules']);
      expect(find.byType(EmptyView), findsOneWidget);
      expect(find.text('No country blocks'), findsOneWidget);
      expect(find.textContaining('break-glass still works'), findsOneWidget);
      expect(find.byKey(const ValueKey('add_rule')), findsOneWidget);
    });

    testWidgets('shows the error and retries', (tester) async {
      final api = FakeSentinelApi()
        ..failWith = const NetworkException('connection refused');
      await tester.pumpWidget(MaterialApp(home: GeoRulesScreen(api: api)));
      await tester.pumpAndSettle();

      expect(find.byType(ErrorView), findsOneWidget);
      expect(find.text('Backend unreachable: connection refused'), findsOneWidget);
      expect(find.byType(EmptyView), findsNothing);

      api.failWith = null;
      api.geoRules = [kpRule()];
      await tester.tap(find.text('Retry'));
      await tester.pumpAndSettle();

      expect(api.calls, ['listGeoRules', 'listGeoRules']);
      expect(find.byType(ErrorView), findsNothing);
      expect(find.text('KP'), findsOneWidget);
    });

    testWidgets('pull to refresh reloads the list', (tester) async {
      final api = await pumpScreen(tester, rules: [kpRule()]);
      api.geoRules.add(const GeoRule(id: 'geo-ru', country: 'RU'));
      expect(find.text('RU'), findsNothing);

      await tester.fling(find.byType(ListView), const Offset(0, 300), 1000);
      await tester.pumpAndSettle();

      expect(api.calls, ['listGeoRules', 'listGeoRules']);
      expect(find.text('RU'), findsOneWidget);
    });

    testWidgets('adds a rule: the code is upper-cased and the list refreshes',
        (tester) async {
      final api = await pumpScreen(tester);

      await openAddDialog(tester);
      await submitRule(tester, 'kp', note: 'no staff there');

      expect(api.calls, ['listGeoRules', 'addGeoRule', 'listGeoRules']);
      expect(api.geoRules, hasLength(1));
      expect(api.geoRules.single.country, 'KP');
      expect(api.geoRules.single.note, 'no staff there');
      // The dialog is closed, the new row is on screen with its snack.
      expect(find.byKey(const ValueKey('submit_rule')), findsNothing);
      expect(find.text('KP'), findsOneWidget);
      expect(find.text('no staff there'), findsOneWidget);
      expect(find.text('Rule for KP added'), findsOneWidget);
      expect(find.byType(EmptyView), findsNothing);
    });

    testWidgets('upper-cases the code as it is typed', (tester) async {
      await pumpScreen(tester);
      await openAddDialog(tester);

      await tester.enterText(find.byKey(const ValueKey('country_field')), 'kp');
      await tester.pump();

      final field = tester.widget<TextField>(find.descendant(
        of: find.byKey(const ValueKey('country_field')),
        matching: find.byType(TextField),
      ));
      expect(field.controller?.text, 'KP');
    });

    testWidgets('rejects an invalid code without calling the backend', (tester) async {
      final api = await pumpScreen(tester);

      await openAddDialog(tester);
      await submitRule(tester, 'K1');

      expect(find.text('Two-letter country code, for example KP'), findsOneWidget);
      expect(api.calls, ['listGeoRules']);
      expect(find.byKey(const ValueKey('submit_rule')), findsOneWidget);
    });

    testWidgets('rejects an empty code without calling the backend', (tester) async {
      final api = await pumpScreen(tester);

      await openAddDialog(tester);
      await submitRule(tester, '');

      expect(find.text('Two-letter country code, for example KP'), findsOneWidget);
      expect(api.calls, ['listGeoRules']);
    });

    testWidgets('shows the conflict inline for a duplicate country', (tester) async {
      final api = await pumpScreen(tester, rules: [kpRule()]);

      await openAddDialog(tester);
      await submitRule(tester, 'kp');

      expect(api.calls, ['listGeoRules', 'addGeoRule']);
      expect(find.text('This country already has a rule'), findsOneWidget);
      // Still open, so the user can correct the code.
      expect(find.byKey(const ValueKey('submit_rule')), findsOneWidget);
      expect(api.geoRules, hasLength(1));

      // Typing again clears the backend error (the field fades it out, so
      // wait for the animation).
      await tester.enterText(find.byKey(const ValueKey('country_field')), 'RU');
      await tester.pumpAndSettle();
      expect(find.text('This country already has a rule'), findsNothing);
    });

    testWidgets('shows an unknown code inline on a 400', (tester) async {
      final api = await pumpScreen(tester);

      await openAddDialog(tester);
      // The fake answers 400 for anything that is not two letters once the
      // client-side check is bypassed; the real backend does the same for a
      // code that is not a country, so the message is worded for that case.
      api.failWith = const ApiException('unknown country code', statusCode: 400);
      await submitRule(tester, 'XX');

      expect(find.text('Unknown country code'), findsOneWidget);
      expect(find.byKey(const ValueKey('submit_rule')), findsOneWidget);
    });

    testWidgets('shows other errors in a snack and keeps the dialog', (tester) async {
      final api = await pumpScreen(tester);

      await openAddDialog(tester);
      api.failWith = const NetworkException('timeout');
      await submitRule(tester, 'KP');

      expect(find.text('Backend unreachable: timeout'), findsOneWidget);
      expect(find.byKey(const ValueKey('submit_rule')), findsOneWidget);
      expect(find.text('Unknown country code'), findsNothing);
    });

    testWidgets('cancel closes the dialog without a call', (tester) async {
      final api = await pumpScreen(tester);

      await openAddDialog(tester);
      await tester.tap(find.text('Cancel'));
      await tester.pumpAndSettle();

      expect(find.byKey(const ValueKey('submit_rule')), findsNothing);
      expect(api.calls, ['listGeoRules']);
    });

    testWidgets('removes a rule after confirmation', (tester) async {
      final api = await pumpScreen(tester, rules: [kpRule()]);

      await tester.tap(find.byKey(const ValueKey('delete_geo-kp')));
      await tester.pumpAndSettle();
      expect(find.text('Remove the KP rule?'), findsOneWidget);
      expect(api.calls, ['listGeoRules']);

      await tester.tap(find.widgetWithText(FilledButton, 'Remove'));
      await tester.pumpAndSettle();

      expect(api.calls, ['listGeoRules', 'deleteGeoRule', 'listGeoRules']);
      expect(api.geoRules, isEmpty);
      expect(find.text('KP'), findsNothing);
      expect(find.text('Rule for KP removed'), findsOneWidget);
      expect(find.text('No country blocks'), findsOneWidget);
    });

    testWidgets('cancelling the confirmation keeps the rule', (tester) async {
      final api = await pumpScreen(tester, rules: [kpRule()]);

      await tester.tap(find.byKey(const ValueKey('delete_geo-kp')));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Cancel'));
      await tester.pumpAndSettle();

      expect(api.calls, ['listGeoRules']);
      expect(find.text('KP'), findsOneWidget);
    });

    testWidgets('a failed delete shows a snack and keeps the row', (tester) async {
      final api = await pumpScreen(tester, rules: [kpRule()]);

      api.failWith = const UnauthorizedException('bad token', statusCode: 401);
      await tester.tap(find.byKey(const ValueKey('delete_geo-kp')));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(FilledButton, 'Remove'));
      await tester.pumpAndSettle();

      expect(find.textContaining('Check the admin token'), findsOneWidget);
      expect(find.text('KP'), findsOneWidget);
    });
  });
}
