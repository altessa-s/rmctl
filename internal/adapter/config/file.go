// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package config

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/altessa-s/rmctl/internal/adapter/flock"
)

// The configuration file and the environment that override it.
const (
	// EnvConfigFile names the configuration file instead of
	// <config dir>/config.toml.
	EnvConfigFile = "RMCTL_CONFIG_FILE"
	// EnvSecrets chooses the token backend over the configuration file.
	EnvSecrets = "RMCTL_SECRETS" // #nosec G101 -- the name of a variable that holds "file" or "keychain", not a credential
	// EnvKeychainService files the Keychain items under another service:
	// a test keeps its items apart from the user's with it.
	EnvKeychainService = "RMCTL_KEYCHAIN_SERVICE"
	// EnvCacheTTL is the cache TTL over the configuration file.
	EnvCacheTTL = "RMCTL_CACHE_TTL"
	// configFile is the file's name in the rmctl configuration directory.
	configFile = "config.toml"
	// fileMode and dirMode keep the configuration to its owner.
	fileMode = 0o600
	dirMode  = 0o700
)

// The token backends.
const (
	BackendFile     = "file"
	BackendKeychain = "keychain"
)

// The sources a backend choice can come from.
const (
	SourceEnv     = "env"
	SourceProfile = "profile"
	SourceConfig  = "config"
	SourceDefault = "default"
)

// ErrConfig means the configuration file is not one rmctl reads.
var ErrConfig = errors.New("the configuration file is invalid")

// ErrConfigChanged means config.toml changed while rmctl was editing it:
// nothing was written, so the other change is kept.
var ErrConfigChanged = errors.New("the configuration file changed while it was being edited; nothing was written, run the command again")

// ErrConfigBusy means another rmctl holds the configuration file's lock.
var ErrConfigBusy = errors.New("another rmctl is editing the configuration file; try again")

// ErrConfigEdit means the configuration file cannot be changed without
// losing what a person wrote in it.
var ErrConfigEdit = errors.New("the configuration file already chooses this profile's backend; edit it by hand")

// ReadFunc reads the configuration file: its bytes, or exists false.
type ReadFunc func(path string) ([]byte, bool, error)

// ReadFile reads a file of the machine; a missing one is not an error.
func ReadFile(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- the user's own configuration file
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", path, err)
	}
	return data, true, nil
}

// fileSecrets is a [secrets] table.
type fileSecrets struct {
	Backend string `toml:"backend"`
}

// fileProfile is a [profiles.<name>] table.
// fileCache is a [cache] table: ttl is a Go duration ("10m"), offline makes
// --offline the default, budget bounds the cache ("4GiB").
type fileCache struct {
	TTL     string `toml:"ttl"`
	Offline *bool  `toml:"offline"`
	Budget  string `toml:"budget"`
}

type fileProfile struct {
	Secrets fileSecrets `toml:"secrets"`
	Cache   fileCache   `toml:"cache"`
}

// File is the configuration file as rmctl reads it.
type File struct {
	Secrets  fileSecrets            `toml:"secrets"`
	Cache    fileCache              `toml:"cache"`
	Profiles map[string]fileProfile `toml:"profiles"`
	// Mirrors are the [[mirror]] tables, in file order.
	Mirrors []Mirror `toml:"mirror"`
}

// ParseFile decodes a configuration file strictly: a key rmctl does not
// know is an error that names it, and so is a value it cannot use.
func ParseFile(data []byte) (File, error) {
	var f File
	dec := toml.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		if strict, ok := errors.AsType[*toml.StrictMissingError](err); ok {
			keys := make([]string, 0, len(strict.Errors))
			for _, e := range strict.Errors {
				keys = append(keys, strings.Join(e.Key(), "."))
			}
			return File{}, fmt.Errorf("%w: unknown keys %s", ErrConfig, strings.Join(keys, ", "))
		}
		return File{}, fmt.Errorf("%w: %v", ErrConfig, err) //nolint:errorlint // the decoder's message, not a sentinel
	}
	if err := checkBackend("secrets.backend", f.Secrets.Backend); err != nil {
		return File{}, err
	}
	if _, err := parseTTL("cache.ttl", f.Cache.TTL); err != nil {
		return File{}, err
	}
	if _, err := parseBudget("cache.budget", f.Cache.Budget); err != nil {
		return File{}, err
	}
	if err := checkMirrors(f.Mirrors); err != nil {
		return File{}, err
	}
	names := make([]string, 0, len(f.Profiles))
	for name := range f.Profiles {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if !profileName.MatchString(name) {
			return File{}, fmt.Errorf("%w: profiles.%s: %w", ErrConfig, name, ErrProfile)
		}
		if err := checkBackend("profiles."+name+".secrets.backend", f.Profiles[name].Secrets.Backend); err != nil {
			return File{}, err
		}
		if _, err := parseTTL("profiles."+name+".cache.ttl", f.Profiles[name].Cache.TTL); err != nil {
			return File{}, err
		}
		if _, err := parseBudget("profiles."+name+".cache.budget", f.Profiles[name].Cache.Budget); err != nil {
			return File{}, err
		}
	}
	return f, nil
}

func checkBackend(key, value string) error {
	switch value {
	case "", BackendFile, BackendKeychain:
		return nil
	default:
		return fmt.Errorf("%w: %s = %q: a backend is %q or %q", ErrConfig, key, value, BackendFile, BackendKeychain)
	}
}

// parseTTL reads a cache TTL: a Go duration of zero or more, "" for none.
func parseTTL(key, value string) (time.Duration, error) {
	if value == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("%w: %s = %q: a TTL is a duration such as \"10m\" or \"1h\", 0 for none", ErrConfig, key, value)
	}
	return d, nil
}

// cacheOf chooses a profile's cache TTL and offline default: RMCTL_CACHE_TTL
// for the TTL, then the profile's [profiles.<name>.cache], then [cache],
// then a TTL of 0 and online.
func cacheOf(env map[string]string, f File, profile string) (ttl time.Duration, offline bool, err error) {
	p := f.Profiles[profile].Cache
	switch {
	case env[EnvCacheTTL] != "":
		if ttl, err = parseTTL(EnvCacheTTL, env[EnvCacheTTL]); err != nil {
			return 0, false, err
		}
	case p.TTL != "":
		ttl, _ = parseTTL("", p.TTL) //nolint:errcheck // ParseFile checked it
	default:
		ttl, _ = parseTTL("", f.Cache.TTL) //nolint:errcheck // ParseFile checked it
	}
	switch {
	case p.Offline != nil:
		offline = *p.Offline
	case f.Cache.Offline != nil:
		offline = *f.Cache.Offline
	}
	return ttl, offline, nil
}

// backendOf chooses a profile's backend: RMCTL_SECRETS, then the profile's
// table, then the file's [secrets], then the file backend.
func backendOf(env map[string]string, f File, profile string) (backend, source string, err error) {
	if v := env[EnvSecrets]; v != "" {
		if err := checkBackend(EnvSecrets, v); err != nil {
			return "", "", err
		}
		return v, SourceEnv, nil
	}
	if p, ok := f.Profiles[profile]; ok && p.Secrets.Backend != "" {
		return p.Secrets.Backend, SourceProfile, nil
	}
	if f.Secrets.Backend != "" {
		return f.Secrets.Backend, SourceConfig, nil
	}
	return BackendFile, SourceDefault, nil
}

// Writer records a profile's backend in the configuration file. It only
// appends a table, so every comment and every other setting stays as a
// person wrote it; a file that already names the profile's backend is left
// alone, since rewriting it could lose what is around that line.
type Writer struct {
	Path string
	Read ReadFunc
	// Env is the environment, whose RMCTL_SECRETS overrides the file.
	Env map[string]string
}

// Override reports RMCTL_SECRETS, which chooses the backend over the file.
func (w Writer) Override() (variable, backend string, ok bool) {
	v := w.Env[EnvSecrets]
	return EnvSecrets, v, v != ""
}

// SecretsLines are the lines that give a profile its own backend.
func (Writer) SecretsLines(profile, backend string) string {
	return fmt.Sprintf("[profiles.%s.secrets]\nbackend = %q\n", profile, backend)
}

// SetSecretsBackend appends the profile's backend to the file, creating it
// when missing, and checks that the result reads back to that backend
// before it replaces the file.
func (w Writer) SetSecretsBackend(profile, backend string) (string, error) {
	if !profileName.MatchString(profile) {
		return "", fmt.Errorf("profile %q: %w", profile, ErrProfile)
	}
	if err := checkBackend("backend", backend); err != nil || backend == "" {
		return "", fmt.Errorf("%w: backend %q", ErrConfig, backend)
	}
	read := w.Read
	if read == nil {
		read = ReadFile
	}
	unlock, err := lockFile(w.Path)
	if err != nil {
		return "", err
	}
	defer unlock()
	data, existed, err := read(w.Path)
	if err != nil {
		return "", err
	}
	current, err := ParseFile(data)
	if err != nil {
		return "", err
	}
	if p, ok := current.Profiles[profile]; ok && p.Secrets.Backend != "" {
		if p.Secrets.Backend == backend {
			return w.Path, nil
		}
		return "", fmt.Errorf("%w: %s says %q", ErrConfigEdit, w.Path, p.Secrets.Backend)
	}
	next := append(bytes.Clone(data), []byte(separator(data)+w.SecretsLines(profile, backend))...)
	parsed, err := ParseFile(next)
	if err != nil {
		return "", fmt.Errorf("%w: %s cannot take the profile's table: %w", ErrConfigEdit, w.Path, err)
	}
	if got, _, _ := backendOf(nil, parsed, profile); got != backend {
		return "", fmt.Errorf("%w: %s does not read back the backend", ErrConfigEdit, w.Path)
	}
	// The lock keeps other rmctl processes out; a hand edit does not take it,
	// so the replacement itself refuses to lose one.
	return w.Path, replaceUnlessChanged(w.Path, data, existed, next)
}

// Lock timing: how long a second rmctl waits for the lock.
const (
	lockWait = 2 * time.Second
	lockPoll = 50 * time.Millisecond
)

// lockFile takes an exclusive advisory lock on <path>.lock, so two rmctl
// processes never edit the configuration at once. The lock is the system's
// (flock, LockFileEx): it is released when the holder exits, however it
// exits, so there is no stale lock to judge or take over, and nobody can
// release another process's lock. The lock file itself stays.
func lockFile(path string) (func(), error) {
	lock := path + ".lock"
	if err := os.MkdirAll(filepath.Dir(lock), dirMode); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(lock, os.O_RDWR|os.O_CREATE, fileMode) // #nosec G304 -- next to the config file rmctl owns
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(lockWait) //nolint:forbidigo // a lock's wait is wall time, not the library's clock
	for {
		busy, err := flock.TryLock(f)
		switch {
		case err != nil:
			_ = f.Close()
			return nil, fmt.Errorf("lock %s: %w", lock, err)
		case !busy:
			return func() { _ = flock.Unlock(f); _ = f.Close() }, nil
		case time.Now().After(deadline): //nolint:forbidigo // see above
			_ = f.Close()
			return nil, fmt.Errorf("%s: %w", path, ErrConfigBusy)
		}
		time.Sleep(lockPoll)
	}
}

// separator keeps the appended table on lines of its own.
func separator(data []byte) string {
	switch {
	case len(data) == 0:
		return ""
	case data[len(data)-1] == '\n':
		return "\n"
	default:
		return "\n\n"
	}
}

// replaceUnlessChanged installs next at path only while path still holds
// was, and never loses a change another writer made meanwhile:
//
//  1. next is written to a staging file;
//  2. path is renamed aside — atomically, so what is set aside is exactly
//     what was on disk at that instant, a hand edit included;
//  3. what was set aside must be was, else it is put back and nothing is
//     written;
//  4. next is linked into place, which fails if path exists again — a writer
//     that recreated it meanwhile keeps its file, and ours is not installed.
//
// When a step after 2 cannot put the set-aside file back, it is left as
// <path>.prev and the error names it: nothing is deleted.
func replaceUnlessChanged(path string, was []byte, existed bool, next []byte) error {
	return replace(path, was, existed, next, nil)
}

// replace is replaceUnlessChanged with meanwhile run just before the
// install: the moment another writer could recreate the file.
func replace(path string, was []byte, existed bool, next []byte, meanwhile func()) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.part")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	staged := tmp.Name()
	defer func() { _ = os.Remove(staged) }()
	_, werr := tmp.Write(next)
	if jerr := errors.Join(tmp.Chmod(fileMode), werr, tmp.Sync(), tmp.Close()); jerr != nil {
		return fmt.Errorf("write %s: %w", path, jerr)
	}
	// A name of its own: a .prev an earlier refusal kept is never
	// overwritten, on any system.
	prev, aside, err := setAside(path)
	if err != nil {
		return err
	}
	switch {
	case aside != existed:
		// Created or deleted since it was read: an empty file is a file.
		if aside {
			return errors.Join(restore(prev, path), fmt.Errorf("%s: %w", path, ErrConfigChanged))
		}
		return fmt.Errorf("%s: %w", path, ErrConfigChanged)
	case aside:
		got, err := os.ReadFile(prev) // #nosec G304 -- the config file rmctl owns, set aside
		if err != nil || !bytes.Equal(got, was) {
			return errors.Join(restore(prev, path), fmt.Errorf("%s: %w", path, ErrConfigChanged))
		}
	}
	if meanwhile != nil {
		meanwhile()
	}
	if err := os.Link(staged, path); err != nil {
		if errors.Is(err, fs.ErrExist) {
			// Recreated meanwhile: theirs stays, the set-aside original is
			// left beside it rather than lost.
			if aside {
				return fmt.Errorf("%s: %w; the version rmctl read is kept at %s", path, ErrConfigChanged, prev)
			}
			return fmt.Errorf("%s: %w", path, ErrConfigChanged)
		}
		if !aside {
			return fmt.Errorf("install %s: %w", path, err)
		}
		return errors.Join(fmt.Errorf("install %s: %w", path, err), restore(prev, path))
	}
	if aside {
		_ = os.Remove(prev)
	}
	return nil
}

// setAside moves path to a name of its own, <path>.<random>.prev, with a
// rename that never replaces: an occupied name is passed over for another.
// aside is false when path does not exist.
func setAside(path string) (prev string, aside bool, err error) {
	const tries = 8
	for range tries {
		prev = fmt.Sprintf("%s.%s.prev", path, rand.Text())
		err = renameNoReplace(path, prev)
		switch {
		case err == nil:
			return prev, true, nil
		case errors.Is(err, fs.ErrNotExist):
			return "", false, nil
		case !errors.Is(err, fs.ErrExist):
			return "", false, fmt.Errorf("set %s aside: %w", path, err)
		}
	}
	return "", false, fmt.Errorf("set %s aside: %w", path, fs.ErrExist)
}

// restore puts the set-aside file back unless path exists again; then it is
// kept as prev and the error says so.
func restore(prev, path string) error {
	if err := os.Link(prev, path); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%s was recreated meanwhile; the version rmctl set aside is kept at %s", path, prev)
		}
		return fmt.Errorf("put %s back: %w", path, err)
	}
	_ = os.Remove(prev)
	return nil
}
