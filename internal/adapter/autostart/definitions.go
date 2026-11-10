// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package autostart

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode/utf16"

	"github.com/altessa-s/rmctl/internal/port"
)

// The names the service goes by.
const (
	// Label is the launchd job's label, and the name of its plist.
	Label = "io.altessa.rmctl.mirror"
	// Unit is the systemd user unit.
	Unit = "rmctl-mirror.service"
	// Task is the Task Scheduler task.
	Task = "rmctl-mirror"
	// description says what the service is, where a manager shows it.
	description = "rmctl mirror: local folders kept in step with the reMarkable cloud"
)

// throttle is how long launchd and systemd wait before starting the
// service again after it exited with a failure, in seconds.
const throttle = 30

// LaunchAgent is the plist of the launchd agent that runs spec: at login,
// again after an exit that is not a success, the output of the process
// itself going to managerLog.
func LaunchAgent(spec port.ServiceSpec, managerLog string) []byte {
	var b bytes.Buffer
	b.WriteString(xml.Header)
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	plistKey(&b, "Label", Label)
	b.WriteString("\t<key>ProgramArguments</key>\n\t<array>\n")
	for _, arg := range append([]string{spec.Program}, spec.Args...) {
		b.WriteString("\t\t<string>" + escape(arg) + "</string>\n")
	}
	b.WriteString("\t</array>\n")
	if len(spec.Env) > 0 {
		b.WriteString("\t<key>EnvironmentVariables</key>\n\t<dict>\n")
		for _, k := range slices.Sorted(maps.Keys(spec.Env)) {
			b.WriteString("\t\t<key>" + escape(k) + "</key>\n\t\t<string>" + escape(spec.Env[k]) + "</string>\n")
		}
		b.WriteString("\t</dict>\n")
	}
	b.WriteString("\t<key>RunAtLoad</key>\n\t<true/>\n")
	b.WriteString("\t<key>KeepAlive</key>\n\t<dict>\n\t\t<key>SuccessfulExit</key>\n\t\t<false/>\n\t</dict>\n")
	fmt.Fprintf(&b, "\t<key>ThrottleInterval</key>\n\t<integer>%d</integer>\n", throttle)
	plistKey(&b, "StandardOutPath", managerLog)
	plistKey(&b, "StandardErrorPath", managerLog)
	b.WriteString("</dict>\n</plist>\n")
	return b.Bytes()
}

func plistKey(b *bytes.Buffer, key, value string) {
	b.WriteString("\t<key>" + key + "</key>\n\t<string>" + escape(value) + "</string>\n")
}

// escape is s as XML character data.
func escape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s)) //nolint:errcheck // a strings.Builder takes every write
	return b.String()
}

// SystemdUnit is the systemd user unit that runs spec: started with the
// user's session, again 30 seconds after a failure.
func SystemdUnit(spec port.ServiceSpec) []byte {
	var b strings.Builder
	b.WriteString("[Unit]\n")
	b.WriteString("Description=" + description + "\n")
	b.WriteString("After=network-online.target\nWants=network-online.target\n\n")
	b.WriteString("[Service]\nType=simple\n")
	args := make([]string, 0, len(spec.Args)+1)
	for _, a := range append([]string{spec.Program}, spec.Args...) {
		args = append(args, systemdQuote(a))
	}
	b.WriteString("ExecStart=" + strings.Join(args, " ") + "\n")
	for _, k := range slices.Sorted(maps.Keys(spec.Env)) {
		b.WriteString("Environment=" + systemdQuote(k+"="+spec.Env[k]) + "\n")
	}
	fmt.Fprintf(&b, "Restart=on-failure\nRestartSec=%d\n\n", throttle)
	b.WriteString("[Install]\nWantedBy=default.target\n")
	return []byte(b.String())
}

// systemdQuote is s as one word of a systemd command line: in double
// quotes, with quotes and backslashes escaped, and % and $ doubled so that
// systemd expands neither a specifier nor a variable in it.
func systemdQuote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%", "$", "$$", "\n", `\n`)
	return `"` + r.Replace(s) + `"`
}

// headless is how the task starts rmctl without a console window: rmctl is
// a console program, and Task Scheduler would open a window for it at
// every logon. conhost.exe --headless (Windows 10 1809 and later) gives it
// a console nobody sees.
const headless = `%SystemRoot%\System32\conhost.exe`

// TaskXML is the Task Scheduler task that runs spec at the logon of user
// (DOMAIN\name), hidden, in UTF-16 with its byte order mark, as schtasks
// /Create /XML reads it. A task carries no environment: spec.Env is left
// out.
func TaskXML(spec port.ServiceSpec, user string) []byte {
	args := make([]string, 0, len(spec.Args)+2)
	args = append(args, "--headless", WindowsQuote(spec.Program))
	for _, a := range spec.Args {
		args = append(args, WindowsQuote(a))
	}
	text := `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>` + escape(description) + `</Description>
    <URI>\` + Task + `</URI>
  </RegistrationInfo>
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
      <UserId>` + escape(user) + `</UserId>
    </LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>` + escape(user) + `</UserId>
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowHardTerminate>true</AllowHardTerminate>
    <StartWhenAvailable>true</StartWhenAvailable>
    <RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable>
    <IdleSettings>
      <StopOnIdleEnd>false</StopOnIdleEnd>
      <RestartOnIdle>false</RestartOnIdle>
    </IdleSettings>
    <AllowStartOnDemand>true</AllowStartOnDemand>
    <Enabled>true</Enabled>
    <Hidden>true</Hidden>
    <RunOnlyIfIdle>false</RunOnlyIfIdle>
    <WakeToRun>false</WakeToRun>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <Priority>7</Priority>
    <RestartOnFailure>
      <Interval>PT1M</Interval>
      <Count>999</Count>
    </RestartOnFailure>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>` + escape(headless) + `</Command>
      <Arguments>` + escape(strings.Join(args, " ")) + `</Arguments>
    </Exec>
  </Actions>
</Task>
`
	return utf16LE(strings.ReplaceAll(text, "\n", "\r\n"))
}

// byteOrderMark starts a file in UTF-16, and says which way round it is.
const byteOrderMark = 0xfeff

// utf16LE is s in UTF-16, little-endian, after a byte order mark.
func utf16LE(s string) []byte {
	units := utf16.Encode([]rune(s))
	out := make([]byte, 2, 2+2*len(units))
	binary.LittleEndian.PutUint16(out, byteOrderMark)
	for _, u := range units {
		out = binary.LittleEndian.AppendUint16(out, u)
	}
	return out
}

// WindowsQuote is s as one argument of a Windows command line, as
// CommandLineToArgvW reads it back: quoted when it holds a space, a tab or
// a quote, or is empty, with the backslashes before a quote doubled.
func WindowsQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"") {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	slashes := 0
	for _, r := range s {
		switch r {
		case '\\':
			slashes++
		case '"':
			// The backslashes before it are written already: as many again,
			// and one for the quote.
			b.WriteString(strings.Repeat(`\`, slashes+1))
			slashes = 0
		default:
			slashes = 0
		}
		b.WriteRune(r)
	}
	b.WriteString(strings.Repeat(`\`, slashes))
	b.WriteByte('"')
	return b.String()
}
