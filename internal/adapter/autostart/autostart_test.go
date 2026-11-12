// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package autostart_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/altessa-s/rmctl/internal/adapter/autostart"
	"github.com/altessa-s/rmctl/internal/port"
)

var errBoom = errors.New("boom")

// runner records the tools run and answers from a table: the output and
// the error of a command line, by its prefix.
type runner struct {
	mu      sync.Mutex
	calls   []string
	answers map[string]answer
}

type answer struct {
	out string
	err error
}

func (r *runner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	line := strings.Join(append([]string{name}, args...), " ")
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, line)
	for prefix, a := range r.answers {
		if strings.HasPrefix(line, prefix) {
			return []byte(a.out), a.err
		}
	}
	return nil, nil
}

func (r *runner) ran() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.calls, "\n")
}

func manager(t *testing.T, goos string, answers map[string]answer) (*autostart.Manager, *runner, autostart.Options) {
	t.Helper()
	r := &runner{answers: answers}
	o := autostart.Options{GOOS: goos, Home: t.TempDir(), UID: "501", User: `PC\me`, LogDir: filepath.Join(t.TempDir(), "logs"), Runner: r}
	return autostart.New(o), r, o
}

var service = port.ServiceSpec{Program: "/bin/rmctl", Args: []string{"mirror", "run", "--service"}, Env: map[string]string{"A": "b"}} //nolint:gochecknoglobals // a fixed test value

func TestLaunchd(t *testing.T) {
	t.Parallel()
	m, r, o := manager(t, "darwin", map[string]answer{"launchctl print": {out: "\tstate = running\n"}})
	info, err := m.Install(t.Context(), service)
	plist := filepath.Join(o.Home, "Library", "LaunchAgents", "io.altessa.rmctl.mirror.plist")
	if err != nil || !info.Installed || !info.Running || info.File != plist || info.Manager != "launchd" || len(info.Notes) == 0 {
		t.Fatalf("%+v %v", info, err)
	}
	if _, err = os.Stat(plist); err != nil {
		t.Fatal(err)
	}
	want := "launchctl bootout gui/501/io.altessa.rmctl.mirror\nlaunchctl bootstrap gui/501 " + plist + "\nlaunchctl print gui/501/io.altessa.rmctl.mirror"
	if got := r.ran(); got != want {
		t.Fatalf("%s", got)
	}
	if info, err = m.Uninstall(t.Context()); err != nil || info.Installed {
		t.Fatalf("%+v %v", info, err)
	}
	if _, err = os.Stat(plist); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the plist stayed")
	}
	// A bootstrap that fails says what launchctl said.
	m, _, _ = manager(t, "darwin", map[string]answer{"launchctl bootstrap": {out: "Bootstrap failed: 5", err: errBoom}})
	if _, err = m.Install(t.Context(), service); !errors.Is(err, errBoom) || !strings.Contains(err.Error(), "Bootstrap failed: 5") {
		t.Fatal(err)
	}
}

func TestSystemd(t *testing.T) {
	t.Parallel()
	m, r, o := manager(t, "linux", map[string]answer{"systemctl --user is-active": {out: "active\n"}})
	info, err := m.Install(t.Context(), service)
	unit := filepath.Join(o.Home, ".config", "systemd", "user", "rmctl-mirror.service")
	if err != nil || !info.Running || info.File != unit || !strings.Contains(strings.Join(info.Notes, " "), "loginctl enable-linger me") {
		t.Fatalf("%+v %v", info, err)
	}
	if !strings.Contains(r.ran(), "systemctl --user enable rmctl-mirror.service\nsystemctl --user restart rmctl-mirror.service") {
		t.Fatal(r.ran())
	}
	if info, err = m.Status(t.Context()); err != nil || !info.Installed || !info.Running {
		t.Fatalf("%+v %v", info, err)
	}
	if _, err = m.Uninstall(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.ran(), "systemctl --user disable --now rmctl-mirror.service") {
		t.Fatal(r.ran())
	}
	// $XDG_CONFIG_HOME holds the unit when set.
	o.ConfigHome = t.TempDir()
	o.Runner = &runner{}
	if info, err = autostart.New(o).Install(t.Context(), service); err != nil || !strings.HasPrefix(info.File, o.ConfigHome) {
		t.Fatalf("%+v %v", info, err)
	}
	// No user session: refused before anything is written.
	m, r, o = manager(t, "linux", map[string]answer{"systemctl --user show-environment": {err: errBoom}})
	if _, err = m.Install(t.Context(), service); !errors.Is(err, autostart.ErrNoUserSession) {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(o.Home, ".config")); !errors.Is(err, os.ErrNotExist) || strings.Contains(r.ran(), "daemon-reload") {
		t.Fatal("something was written")
	}
	m, _, _ = manager(t, "linux", map[string]answer{"systemctl --user enable": {err: errBoom}})
	if _, err = m.Install(t.Context(), service); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
}

func TestTaskScheduler(t *testing.T) {
	t.Parallel()
	m, r, o := manager(t, "windows", map[string]answer{"powershell": {out: "Running\r\n"}})
	info, err := m.Install(t.Context(), service)
	file := filepath.Join(o.LogDir, "rmctl-mirror.xml")
	if err != nil || !info.Running || info.File != file || info.Manager != "Task Scheduler" ||
		!strings.Contains(strings.Join(info.Notes, " "), "set A as user environment") {
		t.Fatalf("%+v %v", info, err)
	}
	if !strings.Contains(r.ran(), "schtasks /Create /TN rmctl-mirror /XML "+file+" /F\nschtasks /Run /TN rmctl-mirror") {
		t.Fatal(r.ran())
	}
	if _, err = m.Uninstall(t.Context()); err != nil || !strings.Contains(r.ran(), "schtasks /Delete /TN rmctl-mirror /F") {
		t.Fatal(r.ran(), err)
	}
	// Not installed: nothing to delete, and the status says so.
	m, r, _ = manager(t, "windows", map[string]answer{"schtasks /Query": {err: errBoom}})
	if _, err = m.Uninstall(t.Context()); err != nil || strings.Contains(r.ran(), "/Delete") {
		t.Fatal(r.ran(), err)
	}
	if info, err = m.Status(t.Context()); err != nil || info.Installed || info.Running {
		t.Fatalf("%+v %v", info, err)
	}
	m, _, _ = manager(t, "windows", map[string]answer{"schtasks /Delete": {err: errBoom}})
	if _, err = m.Install(t.Context(), port.ServiceSpec{Program: "rmctl.exe"}); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Uninstall(t.Context()); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
	m, _, _ = manager(t, "windows", map[string]answer{"schtasks /Create": {err: errBoom}})
	if _, err = m.Install(t.Context(), service); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
}

func TestUnsupported(t *testing.T) {
	t.Parallel()
	m, _, _ := manager(t, "plan9", nil)
	for _, err := range []error{
		func() error { _, err := m.Install(t.Context(), service); return err }(),
		func() error { _, err := m.Uninstall(t.Context()); return err }(),
		func() error { _, err := m.Status(t.Context()); return err }(),
	} {
		if !errors.Is(err, autostart.ErrUnsupported) {
			t.Fatal(err)
		}
	}
	if autostart.New(autostart.Options{GOOS: "linux"}) == nil {
		t.Fatal("no manager with the default runner")
	}
}

// The log the manager opens is the one it reads.
func TestManagerLog(t *testing.T) {
	t.Parallel()
	m, _, _ := manager(t, "linux", nil)
	w, err := m.OpenLog()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write([]byte("one\ntwo\n")); err != nil {
		t.Fatal(err)
	}
	lines, end, err := m.ReadLog(10)
	if err != nil || strings.Join(lines, ",") != "one,two" || end != 8 {
		t.Fatal(lines, end, err)
	}
	if _, err = w.Write([]byte("three\n")); err != nil {
		t.Fatal(err)
	}
	if lines, end, err = m.ReadLogFrom(end); err != nil || strings.Join(lines, ",") != "three" || end != 14 {
		t.Fatal(lines, end, err)
	}
	_ = w.Close()
}

// The real runner runs a program and returns its output.
func TestExecRunner(t *testing.T) {
	t.Parallel()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = (autostart.ExecRunner{}).Run(t.Context(), exe, "-test.run=^$"); err != nil {
		t.Fatal(err)
	}
}
