/// Small helpers shared by the JSON models.
library;

/// Reads an RFC 3339 timestamp. Returns null when the field is absent, null
/// or not a valid timestamp, so one bad field never breaks a whole list.
DateTime? readDateTime(Object? value) {
  if (value is! String || value.isEmpty) return null;
  return DateTime.tryParse(value)?.toUtc();
}

/// Serialises a timestamp as RFC 3339 in UTC, the way the contract wants it.
String? writeDateTime(DateTime? value) => value?.toUtc().toIso8601String();

/// Reads a string field. Empty strings count as absent because FCM data
/// messages carry "" for unknown values.
String? readString(Object? value) {
  if (value == null) return null;
  final text = value.toString();
  return text.isEmpty ? null : text;
}

/// Reads an integer field that may arrive as a number or a numeric string.
int? readInt(Object? value) {
  if (value is int) return value;
  if (value is num) return value.toInt();
  if (value is String) return int.tryParse(value);
  return null;
}

/// Reads the `items` array of a list response.
List<Map<String, dynamic>> readItems(Map<String, dynamic> body) {
  final items = body['items'];
  if (items is! List) return const [];
  return items.whereType<Map<String, dynamic>>().toList();
}
