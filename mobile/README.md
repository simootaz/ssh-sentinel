# mobile

Flutter app for the admins' phones. Android first. Several phones can be registered; every request is pushed to all of them and the first answer wins.

## Role

- Receives an FCM high-priority push for each pending request, SSH login or sudo.
- Incoming request screen: context (ssh or sudo, with the command for sudo), user, source IP, geo, host, 30 s countdown from the backend's `expires_at`, three buttons: Deny, Approve, Always allow. Always allow opens a TTL picker: 1 hour, 24 hours, custom, permanent. The same three actions are on the notification itself, so a decision does not require opening the app.
- History screen: past requests, their outcome, which phone decided.
- Whitelist screen: list, add and remove always-allow entries, with context and expiry.
- Blocked IPs screen: IPs the backend auto-blocked, with counts, and an unblock button.
- Geo rules screen: country blocklist, add and remove.
- Settings screen: backend base URL and admin token, stored in Android's encrypted storage; registers the phone with `POST /devices` (label editable) and re-registers when the FCM token rotates.

Notification actions run in a background handler that calls `POST /verdict` directly. That is what makes "approve from the lock screen" work. A `409` answer means another phone was faster: show the outcome, do not retry. A `request_decided` push dismisses the notification when it arrives; the app must not depend on it.

## Layout

| Path | Content |
|---|---|
| `lib/screens/incoming_request/` | request details, countdown, buttons, TTL picker |
| `lib/screens/history/` | history list with filters |
| `lib/screens/whitelist/` | whitelist list and add form |
| `lib/screens/blocked_ips/` | auto-blocked IPs, unblock |
| `lib/screens/geo_rules/` | country blocklist |
| `lib/screens/settings/` | backend URL, admin token, device registration |
| `lib/services/` | backend API client (contract v1), FCM and notification setup, secure storage |
| `lib/models/` | request, whitelist entry, device, blocked IP, geo rule, verdict |
| `lib/widgets/` | shared widgets (countdown, request card, TTL picker) |
| `android/` | Android project. `google-services.json` goes in `android/app/` and is gitignored |
| `test/` | unit and widget tests |

## Build

Flutter stable (3.x), Android SDK, a device or emulator. From `mobile\` (PowerShell):

```powershell
# first time only: generates the Flutter project around this layout, replace the org with your reverse domain
flutter create --project-name ssh_sentinel --platforms android --org com.example .
flutter pub get
flutter run                     # debug build on the connected device
flutter build apk --release     # release APK under build\app\outputs\flutter-apk\
```

Firebase setup, once: create a Firebase project, add an Android app with the same package name, download `google-services.json` into `android\app\`. The FCM service account used by the backend comes from the same Firebase project and goes to the backend's secrets, never into this repo.

## Test

```powershell
flutter analyze
flutter test
```

Widget tests cover each screen against a fake API client; unit tests cover the contract v1 JSON models with the examples from `docs/architecture.md`.

Manual checks on real phones (emulators are unreliable for push delivery):

1. App in foreground: request screen appears, countdown runs, each button ends the request. Always allow asks for a TTL.
2. App in background or phone locked: notification with three action buttons, each one reaches the backend without opening the app.
3. Two phones registered: both get the push; the second one to answer sees "already decided by <label>" and its notification goes away.
4. A sudo request shows the command and "sudo" clearly, and a null source IP shows as "local".
5. Push arriving after the 30 s: the app shows the request as expired, buttons are disabled.
6. Blocked IPs: after three denials from one test client the IP appears; unblock works. Geo rules: a rule for the test client's country blocks the next attempt without a push, and it shows in history.
7. Backend URL wrong or token wrong: clear error in Settings, nothing crashes.
