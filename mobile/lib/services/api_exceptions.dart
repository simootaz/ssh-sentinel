import '../models/verdict_result.dart';

/// Base class of every error raised by the API client.
class ApiException implements Exception {
  const ApiException(this.message, {this.statusCode});

  /// Short message, from the backend's `{"error": ...}` body when there is one.
  final String message;
  final int? statusCode;

  @override
  String toString() =>
      statusCode == null ? message : '$message (HTTP $statusCode)';
}

/// Base URL or admin token missing: the Settings screen has not been filled in.
class NotConfiguredException extends ApiException {
  const NotConfiguredException()
      : super('Backend URL and admin token are not set. Open Settings.');
}

/// DNS, connection, TLS or timeout failure: the backend could not be reached.
class NetworkException extends ApiException {
  const NetworkException(super.message);
}

/// 401 or 403: the admin token is missing, wrong or not allowed for this call.
class UnauthorizedException extends ApiException {
  const UnauthorizedException(super.message, {super.statusCode});
}

/// 404.
class NotFoundException extends ApiException {
  const NotFoundException(super.message) : super(statusCode: 404);
}

/// 409 on any route except `/verdict`, for instance a geo rule that exists.
class ConflictException extends ApiException {
  const ConflictException(super.message) : super(statusCode: 409);
}

/// 409 on `POST /verdict`: the request was already decided, by another phone
/// or by the timeout. Carries who decided so the app can show it.
class AlreadyDecidedException extends ConflictException {
  const AlreadyDecidedException(this.decision)
      : super('request already decided');

  final AlreadyDecided decision;

  @override
  String toString() => 'Request ${decision.description}';
}

/// One line for the user, whatever went wrong.
String describeError(Object error) {
  if (error is AlreadyDecidedException) return error.toString();
  if (error is NotConfiguredException) return error.message;
  if (error is NetworkException) return 'Backend unreachable: ${error.message}';
  if (error is UnauthorizedException) {
    return 'Rejected by the backend: ${error.message}. Check the admin token.';
  }
  if (error is ApiException) return error.toString();
  return error.toString();
}
