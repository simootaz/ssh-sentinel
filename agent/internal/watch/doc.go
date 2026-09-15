// Package watch follows the Windows OpenSSH server log and reports each accepted login to the
// backend in notify mode. Windows is notify-only: nothing here blocks a login.
//
// How events are read: OpenSSH Server for Windows writes to the "OpenSSH/Operational" channel,
// event ID 4, with the sshd message in EventData ("Accepted publickey for deploy from
// 203.0.113.42 port 51234 ssh2: ..."). The watcher polls that channel with wevtutil, which
// ships with every Windows, so the binary keeps zero dependencies. A poll every 2 s is plenty
// for a login watcher. The first poll only records the newest record id, so logins from before
// the watcher started are not replayed.
//
// Install on Windows (PowerShell as administrator):
//
//	# config, same keys as on Linux
//	mkdir C:\ProgramData\ssh-sentinel
//	notepad C:\ProgramData\ssh-sentinel\config.json
//
//	# register the event source once, so the Application log renders the lines cleanly
//	reg add "HKLM\SYSTEM\CurrentControlSet\Services\EventLog\Application\ssh-sentinel" /v EventMessageFile /t REG_EXPAND_SZ /d "%SystemRoot%\System32\EventCreate.exe" /f
//	reg add "HKLM\SYSTEM\CurrentControlSet\Services\EventLog\Application\ssh-sentinel" /v TypesSupported /t REG_DWORD /d 7 /f
//
//	# test: log in over SSH, then report the logins of the last five minutes
//	ssh-sentinel watch --once
//
//	# run at boot as SYSTEM
//	schtasks /Create /TN ssh-sentinel /SC ONSTART /RU SYSTEM /TR "\"C:\Program Files\ssh-sentinel\ssh-sentinel.exe\" watch" /F
package watch
