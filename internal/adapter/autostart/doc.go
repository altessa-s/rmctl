// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package autostart installs the mirror service with the system's service
// manager, for the user alone: a launchd agent on macOS, a systemd user
// unit on Linux, a Task Scheduler task at logon on Windows. It implements
// port.Autostart, and keeps the service's log, rotated as it grows.
//
// The definitions are pure functions of what the service runs —
// [LaunchAgent], [SystemdUnit], [TaskXML] — tested on every system; the
// manager's tools (launchctl, systemctl, schtasks) are run through a
// [Runner], which a test replaces.
package autostart
