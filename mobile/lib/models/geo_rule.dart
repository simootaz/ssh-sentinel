import 'json_helpers.dart';

/// A country block. Requests whose geo country matches are denied without a push.
class GeoRule {
  const GeoRule({required this.id, required this.country, this.note, this.createdAt});

  factory GeoRule.fromJson(Map<String, dynamic> json) => GeoRule(
        id: readString(json['id']) ?? '',
        country: (readString(json['country']) ?? '').toUpperCase(),
        note: readString(json['note']),
        createdAt: readDateTime(json['created_at']),
      );

  final String id;

  /// ISO 3166-1 alpha-2, upper case.
  final String country;
  final String? note;
  final DateTime? createdAt;
}
