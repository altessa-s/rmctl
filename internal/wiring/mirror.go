// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package wiring

import (
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/altessa-s/rmctl/internal/adapter/autostart"
	"github.com/altessa-s/rmctl/internal/adapter/config"
	"github.com/altessa-s/rmctl/internal/app/mirror"
	"github.com/altessa-s/rmctl/internal/port"
)

// mirrorConfig implements port.MirrorConfig over the [[mirror]] tables of
// config.toml.
type mirrorConfig struct {
	writer config.Writer
}

var _ port.MirrorConfig = mirrorConfig{}

// Mirrors implements port.MirrorConfig.
func (m mirrorConfig) Mirrors() ([]port.MirrorSpec, error) {
	tables, err := config.Mirrors(m.writer.Path, m.writer.Read)
	if err != nil {
		return nil, err
	}
	out := make([]port.MirrorSpec, 0, len(tables))
	for _, t := range tables {
		out = append(out, port.MirrorSpec(t))
	}
	return out, nil
}

// AddMirror implements port.MirrorConfig.
func (m mirrorConfig) AddMirror(spec port.MirrorSpec) error {
	return m.writer.AddMirror(config.Mirror(spec))
}

// RemoveMirror implements port.MirrorConfig.
func (m mirrorConfig) RemoveMirror(name string) (bool, error) {
	return m.writer.RemoveMirror(name)
}

// engines implements port.MirrorEngines: the run's own engine for its
// profile, and for any other the engine of a run of rmctl -P <profile>,
// on a transport of its own.
type engines struct {
	opts    Options
	profile string
	own     port.MirrorEngine
}

var _ port.MirrorEngines = engines{}

// Open implements port.MirrorEngines.
func (e engines) Open(profile string) (port.MirrorEngine, func() error, error) {
	if profile == "" || profile == e.profile {
		return e.own, func() error { return nil }, nil
	}
	opts := e.opts
	opts.Profile, opts.ReadOnly = profile, false
	svc, err := Build(opts)
	if err != nil {
		return nil, nil, err
	}
	return svc.MirrorEngine, svc.Close, nil
}

// serviceEnv are the variables of this run the mirror service keeps: where
// the configuration, the tokens and the cache are, and which hosts it talks
// to.
var serviceEnv = []string{ //nolint:gochecknoglobals // a fixed list
	config.EnvConfigDir, config.EnvCacheDir, config.EnvConfigFile, config.EnvProfile, config.EnvSecrets,
	config.EnvKeychainService, config.EnvCacheTTL, config.EnvCacheBudget,
	"RMCLOUD_HOST", "RMCLOUD_AUTH_HOST", "RMCLOUD_SYNC_HOST",
}

// LogDir is the directory of the mirror service's log, below the
// configuration directory of every profile.
func LogDir(cfg config.Config) string { return filepath.Join(cfg.ConfigRoot, "logs") }

// mirrorService is the autostart service of the mirrors on this system,
// running this very rmctl.
func mirrorService(cfg config.Config, opts Options, timer port.Timer) port.MirrorService {
	env := map[string]string{}
	for _, k := range serviceEnv {
		if v := opts.Env[k]; v != "" {
			env[k] = v
		}
	}
	program, err := os.Executable()
	if err == nil {
		program = autostart.StableExecutable(program, func(p string) bool {
			_, serr := os.Stat(p)
			return serr == nil
		})
	}
	o := autostart.Options{GOOS: runtime.GOOS, LogDir: LogDir(cfg), ConfigHome: opts.Env["XDG_CONFIG_HOME"], Runner: opts.ServiceRunner}
	o.Home, _ = os.UserHomeDir() //nolint:errcheck // a missing home makes the definition's path fail to write, which says so
	if u, uerr := user.Current(); uerr == nil {
		o.UID, o.User = u.Uid, u.Username
	} else {
		o.UID = strconv.Itoa(os.Getuid())
	}
	if opts.ServiceHome != "" {
		o.Home = opts.ServiceHome
	}
	return mirror.NewAutostart(autostart.New(o), program, env, timer)
}
