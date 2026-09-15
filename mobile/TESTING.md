# Testing the mobile app

How to set up Firebase, build the app, run the automated tests and check it on real phones. Companion to `README.md` in this folder. The contract and the failure modes it refers to are in `docs/architecture.md`, sections 5 and 7.

All commands are PowerShell, run from `mobile\` unless said otherwise. Emulators are unreliable for push delivery: the manual checks need real phones, Android 6.0 or newer (`minSdk` 23).

## 1. Firebase setup (once per project)

The app receives pushes through FCM. The backend sends them through the FCM HTTP v1 API. Both sides use the same Firebase project.

| # | Step | Where |
|---|---|---|
| 1 | Create a Firebase project | console.firebase.google.com |
| 2 | Add an Android app. Package name: `com.example.ssh_sentinel`. Nickname free, SHA-1 not needed | Project settings > Your apps |
| 3 | Download `google-services.json` into `mobile\android\app\` | same page |
| 4 | Enable the Firebase Cloud Messaging API (v1) for the project | console.cloud.google.com > APIs & Services, with the Firebase project selected |
| 5 | Generate a service account key, for the backend only | Project settings > Service accounts > Generate new private key |

Step 2: the package name must match `applicationId` in `android\app\build.gradle`. It is `com.example.ssh_sentinel` because the project was generated with `flutter create --org com.example`. To change it, edit `applicationId` and `namespace` in `android\app\build.gradle`, move `android\app\src\main\kotlin\com\example\ssh_sentinel\MainActivity.kt` to the folder of the new package and update its `package` line, then register the new name in Firebase and download a fresh `google-services.json`. A mismatch is the most common reason for "no push arrives".

Step 3: `google-services.json` is gitignored at the repo root, next to `service-account*.json` and `firebase-adminsdk*.json`. It holds the Firebase project ids. `android\app\build.gradle` applies the `google-services` Gradle plugin only when the file exists. Without it the build prints a warning, the app still builds and runs, but `Firebase.initializeApp()` fails at start: push is disabled, the Settings screen shows a warning, and only the lists and Settings work.

Step 5: the JSON key is the backend's credential, `FCM_SERVICE_ACCOUNT_JSON` (`docs/architecture.md`, section 6). It authorizes sending pushes to every phone. It never goes into this repo or onto a phone. Keep it in the backend's secret store, or outside the working tree on the dev machine; section 5 below uses it once from there for a hand-made push.

## 2. Build and install on a phone

Prerequisites: Flutter stable 3.x, the Android SDK with platform tools (`adb`), USB debugging enabled on the phone.

```powershell
flutter doctor                  # Flutter, Android toolchain and connected device all green
flutter devices                 # the phone must be listed
flutter pub get
flutter run                     # debug build: installs, starts, streams the logs
flutter build apk --release     # release APK: build\app\outputs\flutter-apk\app-release.apk
adb install -r build\app\outputs\flutter-apk\app-release.apk
```

Notes:

- The release build is signed with the debug key (`signingConfig = signingConfigs.debug` in `android\app\build.gradle`), so `flutter run --release` and `adb install` work out of the box. For an APK handed to the admins, add a signing config backed by `android\key.properties`, which is gitignored.
- Android 13+ asks for the notification permission at first launch. Accept it. Without it nothing is shown and the buttons do not exist.
- First launch: open Settings, enter the backend base URL (`https://...`, no path; trailing slashes are trimmed) and the admin token, save. The app then calls `POST /devices` with the FCM token and a label (default: manufacturer and model, for instance "Google Pixel 8"; editable). Settings shows the device id returned by the backend and lists the registered phones. The registration is repeated whenever FCM rotates the token.
- Settings has a "Copy push token" button: it copies this phone's FCM token to the clipboard, for section 5.
- Battery: on phones with aggressive battery management (Xiaomi, Huawei, Samsung and others), exempt the app from battery optimisation before testing: Settings > Apps > ssh-sentinel > Battery > Unrestricted, or the vendor's equivalent (Autostart on Xiaomi, "Manage manually" under App launch on Huawei, not in Sleeping apps on Samsung). Doze can otherwise hold a data message for minutes, longer than the 30 s window.

## 3. Automated tests

```powershell
flutter analyze
flutter test
```

| File | Covers |
|---|---|
| `test\models_test.dart` | contract v1 JSON models, with the examples of `docs/architecture.md` section 5: `GET /history` rows (sudo with a null IP, geo, last page), `POST /verdict` 200 (whitelist entry, `auto_blocked`) and 409 (decided by another phone, timeout) |
| `test\push_and_lists_test.dart` | push messages (`access_request` with empty strings, `access_notice` without buttons, round trip through the notification payload, `request_decided`, unknown type ignored), list models (whitelist, device, blocked IP, geo rule), Settings (URL normalisation and validation, config null until both values are set) |
| widget tests | each screen against `test\fakes\fake_api.dart`, an in-memory backend: fill its lists, set `failWith` to make every call throw, `verdictFailWith` for `POST /verdict` only, `delay` for the loading states; sent verdicts are recorded in `verdicts` |

Nothing here needs Firebase, a phone or a backend. Run both commands before every commit; `flutter analyze` must report no issues. `flutter test test\models_test.dart` runs one file, `flutter test --reporter expanded` prints every test name.

## 4. Manual checks on real phones

Common preparation:

- A backend reachable over HTTPS, its admin token, and one enrolled Linux test server running the agent in enforce mode (`agent/README.md`). Never a production server.
- A test client (any machine with an SSH client) whose public IP you know. That IP gets auto-blocked in check 4.6, so it must not be your only way in. Keep a break-glass user on the test server.
- Two phones for check 4.3, one is enough for the others. Each registered: Settings shows a device id.
- One logcat window per phone:

```powershell
adb devices                                                   # serial of each phone
adb -s <serial> logcat -s flutter FLTFireMsgService FirebaseMessaging
```

`flutter` carries the app's own lines (Firebase status, registration errors, Dart exceptions). The other two tags carry the FCM plugin: message received, token, background isolate start.

- Backend history, from any machine that has the admin token. The History screen shows the same rows.

```powershell
$base = "https://<backend>"
$h = @{ Authorization = "Bearer <admin token>" }
(Invoke-RestMethod -Uri "$base/history?limit=5" -Headers $h).items
```

To trigger a request: `ssh <user>@<test server>` from the test client, or `sudo -k; sudo id` on the server (`sudo -k` drops the cached credentials so PAM runs again).

Between checks, undo what the previous one left behind: remove the whitelist entries created by Always allow (Whitelist screen, or the next login of that user is `whitelisted` and no push comes), unblock the test client (Blocked IPs screen), remove test geo rules, and run `sudo -k` on the server.

### 4.1 App in foreground

Preparation: app open on the History screen, phone unlocked.

Steps: trigger an SSH login. The incoming request screen opens on its own. Check the fields: "SSH login on <host>", user, source IP, geo (city, country) when known, the countdown. Tap Approve. Repeat with Deny, then with Always allow: a TTL picker appears with 1 h, 24 h, custom and permanent; pick 1 h.

Expected: the countdown runs from `expires_at` in the push, about 30 s at arrival. Each button ends the request: the screen shows the outcome, and the SSH session opens (Approve, Always allow) or is refused (Deny). After Always allow, the Whitelist screen has an entry for `(user, ssh, server)` expiring in about an hour, and the next login by that user opens without a push.

Where to look: the app; logcat `flutter` for exceptions; history rows `approved` and `denied` with `decided_by_device` set to this phone's label, then a `whitelisted` row for the login after Always allow.

### 4.2 App in background or phone locked

Preparation: press Home, then lock the phone.

Steps: trigger an SSH login. A notification appears on the lock screen: title "SSH login on <host>", body "<user> from <ip> (<city>, <country>)", a countdown clock, and the buttons Deny, Approve, Always allow. Tap Approve without unlocking. Repeat with Deny. Trigger a third login and tap Always allow.

Expected: Deny and Approve run in a background isolate (`notificationActionBackground` in `lib\services\background_handlers.dart`) that calls `POST /verdict` directly. The app does not open; the notification is replaced by an outcome such as "Approved: deploy on web-01 / Sent from this phone" on the low-importance `verdict_outcomes` channel. The session opens or is refused accordingly. Always allow needs the TTL picker, so it opens the app on the incoming request screen with the picker showing; the activity is allowed over the lock screen (`showWhenLocked` in the manifest).

Then kill the app from the recents list and repeat once. Same result: the FCM background handler builds the notification without the app process.

Where to look: logcat `FLTFireMsgService` for the delivery and the isolate start, `flutter` for an exception inside it. A "Verdict not sent" notification means the isolate ran and the backend call failed; its text says why, and the request notification is put back while it is still valid. History: `decided_by_device`.

### 4.3 Two phones

Preparation: both phones registered with different labels, both locked.

Steps: trigger one login. Both phones get the notification. Tap Approve on phone A, then Approve on phone B.

Expected: A shows "Approved: ...". B gets `409` and shows "Request already decided by <label of A>", no retry (failure mode 7). Independently of the tap, B's notification is dismissed when the `request_decided` push arrives; if that push is late or lost, the tap still gets the 409. The app does not depend on `request_decided`. Try the reverse order, and one case where B does not tap: its notification goes away on its own within a few seconds.

Where to look: the outcome text on B; logcat on B shows two messages, `access_request` then `request_decided`; history shows one row with `decided_by_device` = A's label.

### 4.4 sudo, and a null source IP

Preparation: a shell on the test server as a user that is not in the break-glass file, app in background.

Steps: `sudo -k; sudo id`.

Expected: title "sudo on <host>", body "<user> ran sudo id" then "source: local". PAM gives sudo no remote host, so `source_ip` is empty in the push and the app shows "local" instead of an IP. The agent reads the command from `/proc` as a best effort; when it could not, the app shows "sudo (command not captured)". Approve: the command runs. Always allow on a sudo request creates a `sudo` whitelist entry, not an `ssh` one; whitelist entries are per context. Check the Whitelist screen.

Where to look: the incoming request screen shows "sudo" and the command; history row with `context: sudo`, `source_ip: null`, `command` filled.

### 4.5 Push arriving after the 30 s

Preparation: app in background.

Steps: put the phone in airplane mode. Trigger a login and let it time out on the server side (about 30 s, the client is refused). Wait 5 s more, then turn airplane mode off.

Expected: FCM keeps a high-priority message for its TTL, 60 s, so the push is delivered late. `expires_at` is already in the past: the notification has no countdown and no buttons, and opening it shows the request as expired with the buttons disabled. Nothing can be approved. When a button is tapped on a request that expired a moment earlier, the backend answers `409` with `status: timeout` and the app shows "Request expired before anyone answered" (failure mode 6). Past 60 s offline, FCM drops the message and nothing arrives (failure mode 5).

Where to look: history row `timeout`; logcat shows the message arriving right after airplane mode is turned off.

### 4.6 Blocked IPs and geo rules

Preparation: note the test client's public IP and the country the push shows for it. Confirm the break-glass user works on the test server before starting.

Steps, auto-block: trigger three logins from the test client within an hour and tap Deny each time (defaults: 3 denials in 1 hour). On the third Deny the outcome reads "<ip> is now blocked after 3 denials." Trigger a fourth login.

Expected: no push for the fourth login, the client is refused at once. The Blocked IPs screen lists the IP with `denial_count` 3 and `hit_count` 1. Tap Unblock. The next login is pushed normally. Denials keep being counted, so three new denials block the IP again.

Steps, geo rules: on the Geo rules screen add the country shown for the test client. Trigger a login.

Expected: no push, refused at once. Remove the rule; the next login is pushed. A whitelisted user from that country is refused too, because rules run before the whitelist (failure modes 8 and 9). If the backend's geo lookup failed, the country is unknown, the rule does not match and the push goes out with the country empty (failure mode 10).

Where to look: history rows `blocked_ip` (`decided_by: autoblock`) and `blocked_geo` (`decided_by: georule`); the Blocked IPs screen. Failure mode 20 is the same mechanism hitting an admin's own IP: any phone can unblock it.

### 4.7 Wrong URL or wrong token

Preparation: app open on Settings.

Steps: enter a URL with a typo in the host name, save, open History. Then the right URL with a wrong token. Then a URL without `https://`. Finally the right values.

Expected, one line each, nothing crashes:

| Input | Message |
|---|---|
| host does not resolve, or nothing listens | "Backend unreachable: <OS error>" after at most 15 s (client timeout) |
| wrong token | "Rejected by the backend: <error>. Check the admin token." (HTTP 401) |
| empty URL | "Enter the backend URL." |
| no scheme, or unparsable | "The URL must start with https://" or "Not a valid URL." |
| URL and token not both set | "Backend URL and admin token are not set. Open Settings." on every other screen |

Device registration fails the same way with wrong values; Settings shows the error and offers a retry. Once the values are right the registration succeeds and the device id appears.

Where to look: Settings; logcat `flutter` prints "device registration skipped: ..." when the registration at start failed.

## 5. Sending a test push without the backend

Checks FCM and the notification on a phone before the backend exists, or isolates a delivery problem. It calls the FCM HTTP v1 API directly with the service account key of section 1, step 5, and the phone's FCM token from "Copy push token" in Settings.

Prerequisites: the Google Cloud SDK (`gcloud`) on the dev machine, the key file outside the repo.

```powershell
gcloud auth activate-service-account --key-file="C:\secrets\ssh-sentinel-fcm.json"
$accessToken = gcloud auth print-access-token
$project = "<firebase project id>"          # "project_id" in google-services.json
$deviceToken = "<paste from Copy push token>"

$now = (Get-Date).ToUniversalTime()
$body = @{
  message = @{
    token   = $deviceToken
    android = @{ priority = "high"; ttl = "60s" }
    data    = @{
      type        = "access_request"
      request_id  = "5f1c9b2e-2c3a-4f6a-9b1e-0d3a2b7c4e11"
      context     = "sudo"
      server      = "web-01"
      username    = "deploy"
      source_ip   = ""
      geo_country = ""
      geo_city    = ""
      command     = "sudo systemctl restart nginx"
      created_at  = $now.ToString("yyyy-MM-ddTHH:mm:ssZ")
      expires_at  = $now.AddSeconds(30).ToString("yyyy-MM-ddTHH:mm:ssZ")
    }
  }
} | ConvertTo-Json -Depth 5

Invoke-RestMethod -Method Post `
  -Uri "https://fcm.googleapis.com/v1/projects/$project/messages:send" `
  -Headers @{ Authorization = "Bearer $accessToken" } `
  -ContentType "application/json" -Body $body
```

Every value under `data` must be a string; FCM rejects anything else. The `data` block is the `access_request` example of `docs/architecture.md` section 5 with fresh timestamps. The answer is `{"name": "projects/<id>/messages/<message id>"}`, and the notification appears on the phone within a few seconds, with the three buttons and a 30 s countdown.

Expected on a button tap: the request does not exist on the backend, so `POST /verdict` fails and the app shows "Verdict not sent" with the backend's error, then puts the request notification back. With no backend configured the text is "Backend URL and admin token are not set." Both are correct: this test is about delivery, not the verdict.

To check the dismissal, send a second message with `data = @{ type = "request_decided"; request_id = "<same id>"; status = "approved"; decided_by_device = "Pixel 8" }`: the notification goes away. A message with `type = "access_notice"` and the other fields of `access_request` shows a notification without buttons on the `access_notices` channel.

The access token is valid for an hour. When done: `gcloud auth revoke <service account email>`; the email is the `client_email` field of the key file.

## 6. Troubleshooting

No push arrives, in the order to check:

1. Notification permission granted: Android Settings > Apps > ssh-sentinel > Notifications, channel `access_requests` on.
2. `android\app\google-services.json` was present at build time. Logcat `flutter` says "Firebase not initialised, push disabled" when it was not, and Settings shows the warning.
3. Package name of the Android app in Firebase equals `applicationId` in `android\app\build.gradle`.
4. Phone registered: Settings shows a device id and `GET /devices` lists it. If not, save Settings again.
5. Battery optimisation off for the app (section 2).
6. Firebase Cloud Messaging API (v1) enabled in the Google Cloud project. Otherwise the backend logs the FCM error and the request ends in `timeout` (failure mode 14).

Section 5 tells the two halves apart: a hand-made push that arrives means the phone is fine and the backend is not.

Everything else:

| Symptom | Check |
|---|---|
| Push arrives with the app open, not when closed | Battery optimisation, or a vendor autostart restriction. The background handler is registered before `runApp`; a crash in it shows under `flutter` right after `FLTFireMsgService` reports the message. |
| Buttons do nothing from the lock screen | Notification permission first. Then logcat: the tap starts a background isolate, look for it and for a Dart exception under `flutter`. A "Verdict not sent" notification means the isolate ran and the backend call failed; its text says why (unreachable, 401, 404). |
| Always allow does nothing | It opens the app instead of deciding in the background. Check that the app may show over the lock screen and that the vendor does not block it. |
| Countdown already expired on arrival, every time | The phone clock is behind the server clock: the countdown compares `expires_at` from the push with the phone's time. Set the phone to automatic date and time; keep NTP on the backend host and on the servers (failure mode 24). A delay of a few seconds is FCM latency and is normal. |
| "already decided by ..." on every request | Two phones registered and the other one answers first, or a stale registration of the same phone. Expected with two admins (failure mode 7). Remove stale entries with `DELETE /devices/{id}` from Settings. |
| "Request expired before anyone answered" | The tap came after the window; the backend had already marked the row `timeout` (failure mode 6). Check the delivery delay in logcat and the battery settings. |
| Wrong URL or token | Settings shows one of the messages of check 4.7. Nothing crashes; the other screens show the same message until Settings is fixed. A self-signed certificate ends as "Backend unreachable: TLS error ..." because the app trusts the OS certificate store only. |
| Token rotated, phone stopped receiving | The app re-registers on rotation while it runs; a rotation that happened while it was closed is caught at the next launch. `GET /devices` shows `last_seen_at`. |
| Build fails on `google-services` | The plugin version is pinned in `android\settings.gradle` (4.4.2) and only applied when the JSON exists. A JSON from the wrong Firebase app fails the Gradle task: download it again for the app with the right package name. |
