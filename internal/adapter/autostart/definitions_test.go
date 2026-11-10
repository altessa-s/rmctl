// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package autostart_test

import (
	"encoding/binary"
	"encoding/xml"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/altessa-s/rmctl/internal/adapter/autostart"
	"github.com/altessa-s/rmctl/internal/port"
)

// spec is a service whose program path and environment need escaping.
var spec = port.ServiceSpec{ //nolint:gochecknoglobals // a fixed test value
	Program: "/Users/me/My Apps/rmctl",
	Args:    []string{"mirror", "run", "--service"},
	Env:     map[string]string{"RMCTL_CONFIG_DIR": "/Users/me/<cfg> & 100%", "RMCLOUD_HOST": "https://h"},
}

// The plist is a well-formed agent: every argument its own string, the
// environment, at load and again after a failure.
func TestLaunchAgent(t *testing.T) {
	t.Parallel()
	data := autostart.LaunchAgent(spec, "/logs/out")
	var plist struct {
		Dict struct {
			Inner []string `xml:",any"`
		} `xml:"dict"`
	}
	if err := xml.Unmarshal(data, &plist); err != nil {
		t.Fatalf("%v\n%s", err, data)
	}
	text := string(data)
	for _, want := range []string{
		"<key>Label</key>\n\t<string>io.altessa.rmctl.mirror</string>",
		"<string>/Users/me/My Apps/rmctl</string>\n\t\t<string>mirror</string>\n\t\t<string>run</string>\n\t\t<string>--service</string>",
		"<key>RMCLOUD_HOST</key>\n\t\t<string>https://h</string>\n\t\t<key>RMCTL_CONFIG_DIR</key>\n\t\t<string>/Users/me/&lt;cfg&gt; &amp; 100%</string>",
		"<key>RunAtLoad</key>\n\t<true/>",
		"<key>KeepAlive</key>\n\t<dict>\n\t\t<key>SuccessfulExit</key>\n\t\t<false/>",
		"<key>StandardErrorPath</key>\n\t<string>/logs/out</string>",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in\n%s", want, text)
		}
	}
	if strings.Contains(string(autostart.LaunchAgent(port.ServiceSpec{Program: "/r"}, "/o")), "EnvironmentVariables") {
		t.Error("an empty environment was written")
	}
}

// The unit quotes every word and keeps systemd from expanding anything in
// them.
func TestSystemdUnit(t *testing.T) {
	t.Parallel()
	text := string(autostart.SystemdUnit(spec))
	for _, want := range []string{
		`ExecStart="/Users/me/My Apps/rmctl" "mirror" "run" "--service"`,
		`Environment="RMCLOUD_HOST=https://h"`,
		`Environment="RMCTL_CONFIG_DIR=/Users/me/<cfg> & 100%%"`,
		"Restart=on-failure\nRestartSec=30",
		"[Install]\nWantedBy=default.target",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in\n%s", want, text)
		}
	}
	odd := string(autostart.SystemdUnit(port.ServiceSpec{Program: `/a"b\c$d`, Env: map[string]string{"RMCTL_CONFIG_DIR": `/x$y"z`}}))
	if !strings.Contains(odd, `ExecStart="/a\"b\\c$$d"`) {
		t.Error(odd)
	}
	// Environment= expands no variable: a $ doubled there would stay two.
	if !strings.Contains(odd, `Environment="RMCTL_CONFIG_DIR=/x$y\"z"`) {
		t.Error(odd)
	}
}

// The task is UTF-16 with a byte order mark, runs rmctl through a headless
// console at the user's logon, hidden, and restarts it on failure.
func TestTaskXML(t *testing.T) {
	t.Parallel()
	data := autostart.TaskXML(port.ServiceSpec{Program: `C:\Program Files\rmctl\rmctl.exe`, Args: []string{"mirror", "run", "--service"}}, `PC\me`)
	if len(data)%2 != 0 || binary.LittleEndian.Uint16(data) != 0xfeff {
		t.Fatal("no UTF-16 byte order mark")
	}
	units := make([]uint16, 0, len(data)/2)
	for i := 2; i < len(data); i += 2 {
		units = append(units, binary.LittleEndian.Uint16(data[i:]))
	}
	text := string(utf16.Decode(units))
	for _, want := range []string{
		`<?xml version="1.0" encoding="UTF-16"?>`,
		"<LogonTrigger>\r\n      <Enabled>true</Enabled>\r\n      <UserId>PC\\me</UserId>",
		"<LogonType>InteractiveToken</LogonType>",
		"<RunLevel>LeastPrivilege</RunLevel>",
		"<Hidden>true</Hidden>",
		"<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>",
		"<RestartOnFailure>",
		`<Command>%SystemRoot%\System32\conhost.exe</Command>`,
		`<Arguments>--headless &#34;C:\Program Files\rmctl\rmctl.exe&#34; mirror run --service</Arguments>`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in\n%s", want, text)
		}
	}
	// The declaration says UTF-16, which the decoder reads as UTF-8 here.
	body := text[strings.Index(text, "?>")+2:]
	if err := xml.Unmarshal([]byte(body), new(struct{})); err != nil {
		t.Fatalf("%v\n%s", err, body)
	}
}

func TestWindowsQuote(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"plain":            "plain",
		"":                 `""`,
		`C:\Program Files`: `"C:\Program Files"`,
		`say "hi"`:         `"say \"hi\""`,
		`ends\ in\`:        `"ends\ in\\"`,
		`a\"b c`:           `"a\\\"b c"`,
	} {
		if got := autostart.WindowsQuote(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestStableExecutable(t *testing.T) {
	t.Parallel()
	there := func(p string) bool { return p == "/opt/homebrew/opt/rmctl/bin/rmctl" }
	for in, want := range map[string]string{
		"/opt/homebrew/Cellar/rmctl/1.2.3/bin/rmctl":          "/opt/homebrew/opt/rmctl/bin/rmctl",
		"/opt/homebrew/Cellar/rmctl-lite/1.2.3/bin/rmctl":     "/opt/homebrew/Cellar/rmctl-lite/1.2.3/bin/rmctl",
		"/home/linuxbrew/.linuxbrew/Cellar/rmctl/1/libexec/x": "/home/linuxbrew/.linuxbrew/Cellar/rmctl/1/libexec/x",
		"/usr/local/bin/rmctl":                                "/usr/local/bin/rmctl",
		"/x/Cellar/":                                          "/x/Cellar/",
	} {
		if got := autostart.StableExecutable(in, there); got != want {
			t.Errorf("%s: %s", in, got)
		}
	}
}
