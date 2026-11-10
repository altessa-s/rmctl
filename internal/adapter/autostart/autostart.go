// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package autostart

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/altessa-s/rmctl/internal/port"
)

// The failures of the service manager.
var (
	// ErrUnsupported means this system has no service manager rmctl knows.
	ErrUnsupported = errors.New("the mirror service runs on macOS (launchd), Linux (systemd) and Windows (Task Scheduler) only")
	// ErrNoUserSession means systemd runs no manager for this user, as in a
	// container or a session without logind.
	ErrNoUserSession = errors.New("there is no systemd user session (systemctl --user does not answer): " +
		"log in through a session that has one, or start rmctl mirror run --all --watch from your own startup instead")
)

// Runner runs the system's service tools: launchctl, systemctl, schtasks
// and PowerShell. out is what the tool wrote, both streams.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (out []byte, err error)
}

// ExecRunner runs the tools as processes.
type ExecRunner struct{}

// Run implements Runner.
func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput() // #nosec G204 -- the system's own service tools, with arguments rmctl builds
}

// The systems the service runs on, and systemctl's flag for the user's
// own manager.
const (
	goosDarwin  = "darwin"
	goosLinux   = "linux"
	goosWindows = "windows"
	userUnits   = "--user"
)

// Options say where the service lives on this machine.
type Options struct {
	// GOOS is the system: darwin, linux or windows.
	GOOS string
	// Home is the user's home directory; ConfigHome is $XDG_CONFIG_HOME,
	// or "" for ~/.config.
	Home, ConfigHome string
	// UID is the user's numeric identifier, for launchd's GUI domain, and
	// User the DOMAIN\name a Windows task runs as.
	UID, User string
	// LogDir holds the service's log.
	LogDir string
	Runner Runner
}

// Manager implements port.Autostart over the system's service manager, for
// the user alone: a launchd agent on macOS, a systemd user unit on Linux, a
// Task Scheduler task at logon on Windows.
type Manager struct {
	o Options
}

var _ port.Autostart = (*Manager)(nil)

// New returns the service manager of the system o names. A nil Runner runs
// the tools as processes.
func New(o Options) *Manager {
	if o.Runner == nil {
		o.Runner = ExecRunner{}
	}
	return &Manager{o: o}
}

// LogPath is the service's log.
func (m *Manager) LogPath() string { return filepath.Join(m.o.LogDir, LogName) }

// managerLog is what the manager itself captures of the process: its
// output before the log is open, and a crash.
func (m *Manager) managerLog() string { return filepath.Join(m.o.LogDir, "mirror-service.out") }

// definition is the file the service is defined by.
func (m *Manager) definition() (string, error) {
	switch m.o.GOOS {
	case goosDarwin:
		return filepath.Join(m.o.Home, "Library", "LaunchAgents", Label+".plist"), nil
	case goosLinux:
		base := m.o.ConfigHome
		if base == "" {
			base = filepath.Join(m.o.Home, ".config")
		}
		return filepath.Join(base, "systemd", "user", Unit), nil
	case goosWindows:
		return filepath.Join(m.o.LogDir, Task+".xml"), nil
	default:
		return "", ErrUnsupported
	}
}

// info is the service as installed or not, without asking the manager.
func (m *Manager) info() (port.ServiceInfo, error) {
	file, err := m.definition()
	if err != nil {
		return port.ServiceInfo{}, err
	}
	i := port.ServiceInfo{File: file, Log: m.LogPath()}
	switch m.o.GOOS {
	case goosDarwin:
		i.Manager, i.Name = "launchd", Label
	case goosLinux:
		i.Manager, i.Name = "systemd", Unit
	default:
		i.Manager, i.Name = "Task Scheduler", Task
	}
	_, err = os.Stat(file)
	i.Installed = err == nil
	return i, nil
}

// Install implements port.Autostart: it writes the definition and has the
// manager load it and start the service, replacing one installed before.
func (m *Manager) Install(ctx context.Context, spec port.ServiceSpec) (port.ServiceInfo, error) {
	i, err := m.info()
	if err != nil {
		return i, err
	}
	if err = os.MkdirAll(m.o.LogDir, logDirMode); err != nil {
		return i, err
	}
	switch m.o.GOOS {
	case goosDarwin:
		err = m.installLaunchd(ctx, i.File, spec)
		i.Notes = []string{"launchd starts it at every login and again after a failure; the process's own output goes to " + m.managerLog()}
	case goosLinux:
		err = m.installSystemd(ctx, i.File, spec)
		i.Notes = []string{
			"systemd starts it with your session and again after a failure; journalctl --user -u " + Unit + " shows what it wrote before its log opened",
			"it stops when you log out: loginctl enable-linger " + m.userName() + " keeps it running, and starts it at boot",
		}
	default:
		err = m.installTask(ctx, i.File, spec)
		i.Notes = []string{"Task Scheduler starts it at every logon, without a window, and again after a failure"}
		if len(spec.Env) > 0 {
			i.Notes = append(i.Notes, "a task carries no environment: set "+strings.Join(sortedKeys(spec.Env), ", ")+
				" as user environment variables for the service to see them")
		}
	}
	if err != nil {
		return i, err
	}
	i.Installed = true
	if s, serr := m.Status(ctx); serr == nil {
		i.Running = s.Running
	}
	return i, nil
}

func (m *Manager) installLaunchd(ctx context.Context, file string, spec port.ServiceSpec) error {
	if err := writeDefinition(file, LaunchAgent(spec, m.managerLog())); err != nil {
		return err
	}
	// A job loaded before is booted out first, so the new definition takes.
	_, _ = m.o.Runner.Run(ctx, "launchctl", "bootout", m.domain()+"/"+Label) //nolint:errcheck // not loaded is fine
	return m.run(ctx, "launchctl", "bootstrap", m.domain(), file)
}

func (m *Manager) installSystemd(ctx context.Context, file string, spec port.ServiceSpec) error {
	if _, err := m.o.Runner.Run(ctx, "systemctl", userUnits, "show-environment"); err != nil {
		return ErrNoUserSession
	}
	if err := writeDefinition(file, SystemdUnit(spec)); err != nil {
		return err
	}
	for _, args := range [][]string{{userUnits, "daemon-reload"}, {userUnits, "enable", Unit}, {userUnits, "restart", Unit}} {
		if err := m.run(ctx, "systemctl", args...); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) installTask(ctx context.Context, file string, spec port.ServiceSpec) error {
	if err := writeDefinition(file, TaskXML(spec, m.o.User)); err != nil {
		return err
	}
	if err := m.run(ctx, "schtasks", "/Create", "/TN", Task, "/XML", file, "/F"); err != nil {
		return err
	}
	return m.run(ctx, "schtasks", "/Run", "/TN", Task)
}

// Uninstall implements port.Autostart: the manager stops the service and
// forgets it, and the definition is removed. Neither the log nor any
// mirrored file is touched.
func (m *Manager) Uninstall(ctx context.Context) (port.ServiceInfo, error) {
	i, err := m.info()
	if err != nil {
		return i, err
	}
	switch m.o.GOOS {
	case goosDarwin:
		_, _ = m.o.Runner.Run(ctx, "launchctl", "bootout", m.domain()+"/"+Label) //nolint:errcheck // not loaded is fine
	case goosLinux:
		_, _ = m.o.Runner.Run(ctx, "systemctl", userUnits, "disable", "--now", Unit) //nolint:errcheck // not enabled is fine
	default:
		_, _ = m.o.Runner.Run(ctx, "schtasks", "/End", "/TN", Task) //nolint:errcheck // not running is fine
		if i.Installed || m.taskExists(ctx) {
			if err = m.run(ctx, "schtasks", "/Delete", "/TN", Task, "/F"); err != nil {
				return i, err
			}
		}
	}
	if err = os.Remove(i.File); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return i, err
	}
	if m.o.GOOS == goosLinux {
		_, _ = m.o.Runner.Run(ctx, "systemctl", userUnits, "daemon-reload") //nolint:errcheck // the unit is gone either way
	}
	i.Installed, i.Running = false, false
	return i, nil
}

// Status implements port.Autostart.
func (m *Manager) Status(ctx context.Context) (port.ServiceInfo, error) {
	i, err := m.info()
	if err != nil {
		return i, err
	}
	switch m.o.GOOS {
	case goosDarwin:
		out, rerr := m.o.Runner.Run(ctx, "launchctl", "print", m.domain()+"/"+Label)
		i.Running = rerr == nil && strings.Contains(string(out), "state = running")
	case goosLinux:
		out, _ := m.o.Runner.Run(ctx, "systemctl", userUnits, "is-active", Unit) //nolint:errcheck // inactive exits non-zero
		i.Running = strings.TrimSpace(string(out)) == "active"
	default:
		i.Installed = m.taskExists(ctx)
		out, rerr := m.o.Runner.Run(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command",
			"(Get-ScheduledTask -TaskName '"+Task+"').State")
		i.Running = rerr == nil && strings.TrimSpace(string(out)) == "Running"
	}
	return i, nil
}

// taskExists reports whether Task Scheduler knows the task.
func (m *Manager) taskExists(ctx context.Context) bool {
	_, err := m.o.Runner.Run(ctx, "schtasks", "/Query", "/TN", Task)
	return err == nil
}

// OpenLog implements port.Autostart.
func (m *Manager) OpenLog() (io.WriteCloser, error) {
	return OpenLog(m.LogPath(), MaxLogBytes, KeptLogs)
}

// ReadLog implements port.Autostart.
func (m *Manager) ReadLog(lines int) ([]string, int64, error) { return Tail(m.LogPath(), lines) }

// ReadLogFrom implements port.Autostart.
func (m *Manager) ReadLogFrom(offset int64) ([]string, int64, error) {
	return From(m.LogPath(), offset)
}

// domain is the launchd domain of the user's GUI session.
func (m *Manager) domain() string { return "gui/" + m.o.UID }

// userName is the user's name without a domain, for loginctl.
func (m *Manager) userName() string {
	if _, name, ok := strings.Cut(m.o.User, `\`); ok {
		return name
	}
	if m.o.User == "" {
		return "$USER"
	}
	return m.o.User
}

// run runs a tool and names it, and what it wrote, when it fails.
func (m *Manager) run(ctx context.Context, name string, args ...string) error {
	out, err := m.o.Runner.Run(ctx, name, args...)
	if err != nil {
		if text := strings.TrimSpace(string(out)); text != "" {
			return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, text)
		}
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

// definitionMode keeps a definition to its owner: a unit or a plist the
// user's manager reads.
const definitionMode fs.FileMode = 0o600

// writeDefinition writes a definition, creating its directory.
func writeDefinition(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), logDirMode); err != nil {
		return err
	}
	return os.WriteFile(path, data, definitionMode)
}

func sortedKeys(m map[string]string) []string { return slices.Sorted(maps.Keys(m)) }

// StableExecutable is the path the service should run rmctl by: exe,
// unless it lies in a Homebrew Cellar, whose versioned path an upgrade
// removes; then the formula's opt link, when exists says it is there.
func StableExecutable(exe string, exists func(string) bool) string {
	slash := filepath.ToSlash(exe)
	prefix, rest, ok := strings.Cut(slash, "/Cellar/")
	if !ok {
		return exe
	}
	formula, _, ok := strings.Cut(rest, "/")
	if !ok || formula == "" {
		return exe
	}
	opt := filepath.FromSlash(prefix + "/opt/" + formula + "/bin/" + filepath.Base(slash))
	if exists(opt) {
		return opt
	}
	return exe
}
