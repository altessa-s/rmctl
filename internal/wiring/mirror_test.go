// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package wiring_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/altessa-s/rmctl/internal/adapter/config"
	"github.com/altessa-s/rmctl/internal/domain"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/wiring"
)

// runner stands for the system's service tools: it answers every command
// with success, and records them.
type runner struct {
	mu    sync.Mutex
	calls []string
}

func (r *runner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	return nil, nil
}

// The mirrors of config.toml are defined, listed and removed through the
// services, and a pass of another profile's mirror runs on that profile.
func TestMirrors(t *testing.T) {
	t.Parallel()
	opts := options(t)
	opts.Env[config.EnvConfigDir] = filepath.Join(t.TempDir(), "rmctl")
	svc, err := wiring.Build(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = svc.Close() }()
	if svc.Mirrors == nil || svc.MirrorEngine == nil || svc.MirrorService == nil {
		t.Fatalf("%+v", svc)
	}
	dir := t.TempDir()
	if _, err = svc.Mirrors.Add(t.Context(), port.MirrorSpec{Name: "papers", Dir: dir, Folder: "/Papers", Profile: "work"}); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(svc.Config.ConfigFile); err != nil {
		t.Fatal("config.toml was not written:", err)
	}
	states, err := svc.Mirrors.List(t.Context())
	if err != nil || len(states) != 1 || states[0].Spec.Profile != "work" {
		t.Fatalf("%+v %v", states, err)
	}
	// The work profile is not registered: its pass fails on its own engine.
	err = svc.Mirrors.Run(t.Context(), port.MirrorSelection{All: true}, port.MirrorRunOptions{}, nil)
	if !errors.Is(err, port.ErrNotRegistered) {
		t.Fatal(err)
	}
	// A profile whose name is no profile's cannot be opened.
	bad := port.MirrorSpec{Dir: dir, Folder: "/X", Profile: "Bad Name"}
	if err = svc.Mirrors.Run(t.Context(), port.MirrorSelection{AdHoc: &bad}, port.MirrorRunOptions{}, nil); !errors.Is(err, config.ErrProfile) {
		t.Fatal(err)
	}
	// The run's own profile runs on its own engine.
	own := port.MirrorSpec{Dir: dir, Folder: "/X"}
	if err = svc.Mirrors.Run(t.Context(), port.MirrorSelection{AdHoc: &own}, port.MirrorRunOptions{}, nil); !errors.Is(err, port.ErrNotRegistered) {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(dir, domain.MirrorLockName)); err != nil {
		t.Fatal("the pass took no lock:", err)
	}
	if _, err = svc.Mirrors.Remove(t.Context(), "papers"); err != nil {
		t.Fatal(err)
	}
	if states, _ = svc.Mirrors.List(t.Context()); len(states) != 0 {
		t.Fatalf("%+v", states)
	}
}

// The service is installed for this system with the variables of the run
// it keeps, and its log is below the configuration directory.
func TestMirrorService(t *testing.T) {
	t.Parallel()
	opts := options(t)
	root := filepath.Join(t.TempDir(), "rmctl")
	opts.Env[config.EnvConfigDir] = root
	opts.Env[config.EnvCacheDir] = filepath.Join(t.TempDir(), "cache")
	opts.Env["UNRELATED"] = "x"
	r := &runner{}
	opts.ServiceRunner, opts.ServiceHome = r, t.TempDir()
	opts.Env["XDG_CONFIG_HOME"] = filepath.Join(opts.ServiceHome, "xdg")
	svc, err := wiring.Build(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = svc.Close() }()
	info, err := svc.MirrorService.Status(t.Context())
	if err != nil {
		t.Skipf("no service manager on this system: %v", err)
	}
	if info.Log != filepath.Join(root, "logs", "mirror.log") || info.Installed {
		t.Fatalf("%+v", info)
	}
	if info, err = svc.MirrorService.Install(t.Context()); err != nil || !info.Installed {
		t.Fatalf("%+v %v", info, err)
	}
	data, err := os.ReadFile(info.File)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.HasPrefix(info.File, opts.ServiceHome) && !strings.HasPrefix(info.File, root) {
		t.Fatalf("the definition is at %s", info.File)
	}
	if strings.Contains(text, "UNRELATED") || !strings.Contains(text, "mirror") {
		t.Fatalf("%s", text)
	}
	if !strings.Contains(text, root) && !strings.HasSuffix(info.File, ".xml") {
		t.Fatalf("the configuration directory is not kept: %s", text)
	}
	w, err := svc.MirrorService.OpenLog()
	if err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	if info, err = svc.MirrorService.Uninstall(t.Context()); err != nil || info.Installed {
		t.Fatalf("%+v %v", info, err)
	}
	if wiring.LogDir(svc.Config) != filepath.Join(root, "logs") {
		t.Fatal(wiring.LogDir(svc.Config))
	}
}
