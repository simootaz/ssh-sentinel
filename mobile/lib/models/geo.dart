import 'json_helpers.dart';

/// Geo lookup result attached to a request. Any of the three keys may be
/// missing, and the whole object may be null when the lookup failed.
class Geo {
  const Geo({this.country, this.city, this.asn});

  factory Geo.fromJson(Map<String, dynamic> json) => Geo(
        country: readString(json['country']),
        city: readString(json['city']),
        asn: readString(json['asn']),
      );

  /// ISO 3166-1 alpha-2 country code, upper case.
  final String? country;
  final String? city;
  final String? asn;

  bool get isEmpty => country == null && city == null && asn == null;

  /// "Paris, FR", "FR", "unknown".
  String get label {
    final parts = [city, country].whereType<String>().toList();
    return parts.isEmpty ? 'unknown' : parts.join(', ');
  }

  Map<String, dynamic> toJson() => {
        if (country != null) 'country': country,
        if (city != null) 'city': city,
        if (asn != null) 'asn': asn,
      };
}
