// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/internal/adapter/config"
)

// files is a ReadFunc over a map: a path it lacks does not exist.
func files(m map[string]string) config.ReadFunc {
	return func(path string) ([]byte, bool, error) {
		data, ok := m[path]
		return []byte(data), ok, nil
	}
}

const sample = `# where tokens are kept
[secrets]
backend = "file"

[profiles.work.secrets]
backend = "keychain"
`

func TestBackendPrecedence(t *testing.T) {
	t.Parallel()
	dirs := config.BaseDirs{Config: "/cfg", Cache: "/cache"}
	path := filepath.Join("/cfg", "rmctl", "config.toml")
	tests := []struct {
		name, profile, file, env, want, source string
	}{
		{name: "no file", profile: "", want: "file", source: config.SourceDefault},
		{name: "global", profile: "", file: "[secrets]\nbackend = \"keychain\"\n", want: "keychain", source: config.SourceConfig},
		{name: "profile wins over global", profile: "work", file: sample, want: "keychain", source: config.SourceProfile},
		{name: "other profile takes the global", profile: "home", file: sample, want: "file", source: config.SourceConfig},
		{name: "env wins over the file", profile: "work", file: sample, env: "file", want: "file", source: config.SourceEnv},
		{name: "empty file", profile: "", file: "", want: "file", source: config.SourceDefault},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := map[string]string{}
			if tt.name != "no file" {
				m[path] = tt.file
			}
			env := map[string]string{}
			if tt.env != "" {
				env[config.EnvSecrets] = tt.env
			}
			cfg, err := config.LoadFrom(env, tt.profile, dirs, files(m))
			if err != nil || cfg.Secrets != tt.want || cfg.SecretsSource != tt.source || cfg.ConfigFile != path {
				t.Fatalf("%+v %v", cfg, err)
			}
		})
	}
}

func TestConfigFileFromEnvAndConfigDir(t *testing.T) {
	t.Parallel()
	dirs := config.BaseDirs{Config: "/cfg", Cache: "/cache"}
	cfg, err := config.LoadFrom(map[string]string{config.EnvConfigFile: "/elsewhere.toml"}, "", dirs,
		files(map[string]string{"/elsewhere.toml": "[secrets]\nbackend = \"keychain\"\n"}))
	if err != nil || cfg.ConfigFile != "/elsewhere.toml" || cfg.Secrets != "keychain" {
		t.Fatalf("%+v %v", cfg, err)
	}
	// RMCTL_CONFIG_DIR moves the file with the profiles, so a test or a
	// second setup never reads the user's own.
	cfg, err = config.LoadFrom(map[string]string{config.EnvConfigDir: "/iso"}, "", dirs, files(nil))
	if err != nil || cfg.ConfigFile != "/iso/config.toml" {
		t.Fatalf("%+v %v", cfg, err)
	}
	// Load reads no file at all.
	if cfg, err := config.Load(map[string]string{}, "", dirs); err != nil || cfg.Secrets != "file" {
		t.Fatalf("%+v %v", cfg, err)
	}
}

func TestConfigFileRefusals(t *testing.T) {
	t.Parallel()
	dirs := config.BaseDirs{Config: "/cfg", Cache: "/cache"}
	path := filepath.Join("/cfg", "rmctl", "config.toml")
	for name, tc := range map[string]struct{ file, env, part string }{
		"unknown key":       {file: "[secrets]\nbackend = \"file\"\ncolor = \"red\"\n", part: "secrets.color"},
		"unknown table":     {file: "[cache]\nsize = 3\n", part: "cache"},
		"bad backend":       {file: "[secrets]\nbackend = \"vault\"\n", part: `"vault"`},
		"bad profile name":  {file: "[profiles.\"Bad Name\".secrets]\nbackend = \"file\"\n", part: "Bad Name"},
		"bad profile value": {file: "[profiles.work.secrets]\nbackend = 3\n", part: "invalid"},
		"not toml":          {file: "[secrets\n", part: "invalid"},
		"bad env backend":   {env: "vault", part: config.EnvSecrets},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env := map[string]string{}
			if tc.env != "" {
				env[config.EnvSecrets] = tc.env
			}
			_, err := config.LoadFrom(env, "work", dirs, files(map[string]string{path: tc.file}))
			if err == nil || !strings.Contains(err.Error(), tc.part) {
				t.Fatalf("err %v, want it to name %s", err, tc.part)
			}
		})
	}
	failing := func(string) ([]byte, bool, error) { return nil, false, errors.New("disk on fire") }
	if _, err := config.LoadFrom(map[string]string{}, "", dirs, failing); err == nil {
		t.Fatal("a read failure was ignored")
	}
}

func TestWriterOverride(t *testing.T) {
	t.Parallel()
	if v, b, ok := (config.Writer{}).Override(); ok || b != "" || v != config.EnvSecrets {
		t.Fatalf("%s %s %v", v, b, ok)
	}
	w := config.Writer{Env: map[string]string{config.EnvSecrets: "keychain"}}
	if v, b, ok := w.Override(); !ok || b != "keychain" || v != "RMCTL_SECRETS" {
		t.Fatalf("%s %s %v", v, b, ok)
	}
}

func TestWriterAppendsAndKeepsComments(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "rmctl", "config.toml")
	w := config.Writer{Path: path}
	// A missing file is created with the profile's table alone.
	if got, err := w.SetSecretsBackend("work", "keychain"); err != nil || got != path {
		t.Fatalf("%s %v", got, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "[profiles.work.secrets]\nbackend = \"keychain\"\n" {
		t.Fatalf("%q %v", data, err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
	// The same choice again changes nothing; another one is refused.
	if _, err := w.SetSecretsBackend("work", "keychain"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.SetSecretsBackend("work", "file"); !errors.Is(err, config.ErrConfigEdit) {
		t.Fatalf("err %v", err)
	}
	// A file with comments and other settings keeps every byte, and grows.
	commented := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(commented, []byte(sample[:len(sample)-1]), 0o600); err != nil {
		t.Fatal(err)
	}
	cw := config.Writer{Path: commented}
	if _, err := cw.SetSecretsBackend("home", "keychain"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(commented)
	if !strings.HasPrefix(string(after), sample[:len(sample)-1]) || !strings.HasSuffix(string(after), "\n\n[profiles.home.secrets]\nbackend = \"keychain\"\n") {
		t.Fatalf("%q", after)
	}
	cfg, err := config.LoadFrom(map[string]string{config.EnvConfigFile: commented}, "home", config.BaseDirs{Config: "/c", Cache: "/x"}, config.ReadFile)
	if err != nil || cfg.Secrets != "keychain" || cfg.SecretsSource != config.SourceProfile {
		t.Fatalf("%+v %v", cfg, err)
	}
	if w.SecretsLines("work", "file") != "[profiles.work.secrets]\nbackend = \"file\"\n" {
		t.Fatal("lines")
	}
}

func TestWriterRefuses(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	broken := filepath.Join(dir, "broken.toml")
	if err := os.WriteFile(broken, []byte("[secrets\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	inline := filepath.Join(dir, "inline.toml")
	if err := os.WriteFile(inline, []byte("[profiles.work]\nsecrets = {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		path, profile, backend string
	}{
		"broken file":   {broken, "work", "keychain"},
		"inline table":  {inline, "work", "keychain"},
		"bad profile":   {filepath.Join(dir, "x.toml"), "Bad", "file"},
		"bad backend":   {filepath.Join(dir, "x.toml"), "work", "vault"},
		"empty backend": {filepath.Join(dir, "x.toml"), "work", ""},
	} {
		if _, err := (config.Writer{Path: tc.path}).SetSecretsBackend(tc.profile, tc.backend); err == nil {
			t.Errorf("%s: written", name)
		}
	}
	before, _ := os.ReadFile(inline)
	if string(before) != "[profiles.work]\nsecrets = {}\n" {
		t.Fatalf("a refused edit changed the file: %q", before)
	}
	failing := config.Writer{Path: "/x", Read: func(string) ([]byte, bool, error) { return nil, false, errors.New("io") }}
	if _, err := failing.SetSecretsBackend("work", "file"); err == nil {
		t.Fatal("a read failure was ignored")
	}
	if _, _, err := config.ReadFile(dir); err == nil {
		t.Fatal("reading a directory succeeded")
	}
	if data, ok, err := config.ReadFile(filepath.Join(dir, "missing")); err != nil || ok || data != nil {
		t.Fatal("a missing file is not an error")
	}
}

func FuzzParseFile(f *testing.F) {
	for _, seed := range []string{sample, "", "[secrets]\nbackend = \"keychain\"\n", "[profiles.a.secrets]\nbackend = \"file\"\n", "x = 1"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, in string) {
		file, err := config.ParseFile([]byte(in))
		if err != nil {
			return
		}
		// What parses chooses a backend rmctl knows, for every profile.
		for name := range file.Profiles {
			cfg, err := config.LoadFrom(map[string]string{config.EnvConfigFile: "/f"}, name, config.BaseDirs{Config: "/c", Cache: "/x"},
				func(string) ([]byte, bool, error) { return []byte(in), true, nil })
			if err != nil || (cfg.Secrets != config.BackendFile && cfg.Secrets != config.BackendKeychain) {
				t.Fatalf("%q profile %s: %+v %v", in, name, cfg, err)
			}
		}
	})
}

func BenchmarkLoadFrom(b *testing.B) {
	read := files(map[string]string{"/cfg/rmctl/config.toml": sample})
	dirs := config.BaseDirs{Config: "/cfg", Cache: "/cache"}
	env := map[string]string{}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := config.LoadFrom(env, "work", dirs, read); err != nil {
			b.Fatal(err)
		}
	}
}

// asides are the set-aside copies left next to path.
func asides(t *testing.T, path string) []string {
	t.Helper()
	found, err := filepath.Glob(path + ".*.prev")
	if err != nil {
		t.Fatal(err)
	}
	return found
}

// A file edited by someone else while rmctl edits it is left as they made
// it: nothing is written.
func TestSetSecretsBackendRefusesAConcurrentEdit(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("# mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// rmctl reads "# mine"; by the time it replaces the file, a hand edit
	// has made it "# theirs".
	w := config.Writer{Path: path, Read: func(p string) ([]byte, bool, error) {
		data, ok, err := config.ReadFile(p)
		if werr := os.WriteFile(p, []byte("# theirs\n"), 0o600); werr != nil {
			return nil, false, werr
		}
		return data, ok, err
	}}
	if _, err := w.SetSecretsBackend("work", "keychain"); !errors.Is(err, config.ErrConfigChanged) {
		t.Fatalf("err %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "# theirs\n" {
		t.Fatalf("the other change was lost: %q", got)
	}
	if left := asides(t, path); len(left) != 0 {
		t.Fatalf("set-aside copies left behind after the put back: %v", left)
	}
}

// A file that appears while rmctl prepares to create it is theirs: kept.
func TestSetSecretsBackendRefusesAFileCreatedMeanwhile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	w := config.Writer{Path: path, Read: func(p string) ([]byte, bool, error) {
		if err := os.WriteFile(p, []byte("# theirs\n"), 0o600); err != nil {
			return nil, false, err
		}
		return nil, false, nil
	}}
	if _, err := w.SetSecretsBackend("work", "keychain"); !errors.Is(err, config.ErrConfigChanged) {
		t.Fatalf("err %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "# theirs\n" {
		t.Fatalf("%q", got)
	}
}

// Another rmctl holding the lock makes a second one wait, then give up; once
// it is released the edit goes through.
func TestSetSecretsBackendLock(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	release, err := config.LockFile(path)
	if err != nil {
		t.Fatal(err)
	}
	w := config.Writer{Path: path}
	if _, err := w.SetSecretsBackend("work", "keychain"); !errors.Is(err, config.ErrConfigBusy) {
		t.Fatalf("held lock: %v", err)
	}
	release()
	if _, err := w.SetSecretsBackend("work", "keychain"); err != nil {
		t.Fatalf("released lock: %v", err)
	}
	if got, _ := os.ReadFile(path); !strings.Contains(string(got), `backend = "keychain"`) {
		t.Fatalf("%q", got)
	}
}

// Putting a set-aside file back never overwrites a file that appeared in
// its place: the set-aside copy stays and the error names it.
func TestRestoreKeepsBoth(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path, prev := filepath.Join(dir, "config.toml"), filepath.Join(dir, "config.toml.prev")
	for name, body := range map[string]string{path: "theirs", prev: "mine"} {
		if err := os.WriteFile(name, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := config.Restore(prev, path); err == nil || !strings.Contains(err.Error(), prev) {
		t.Fatalf("err %v", err)
	}
	a, _ := os.ReadFile(path)
	b, _ := os.ReadFile(prev)
	if string(a) != "theirs" || string(b) != "mine" {
		t.Fatalf("%q %q", a, b)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := config.Restore(prev, path); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "mine" {
		t.Fatalf("%q", got)
	}
}

// Every way the replacement can meet another writer, or fail, leaves the
// file of the other writer, or the file rmctl read, never a loss.
func TestReplace(t *testing.T) {
	t.Parallel()
	write := func(t *testing.T, path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	read := func(t *testing.T, path string) string {
		t.Helper()
		b, _ := os.ReadFile(path)
		return string(b)
	}
	t.Run("installs", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "config.toml")
		write(t, path, "old")
		if err := config.Replace(path, []byte("old"), true, []byte("new"), nil); err != nil || read(t, path) != "new" {
			t.Fatalf("%v %q", err, read(t, path))
		}
		if left := asides(t, path); len(left) != 0 {
			t.Fatalf("the set-aside copy stayed: %v", left)
		}
	})
	t.Run("recreated after it was set aside", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "config.toml")
		write(t, path, "old")
		err := config.Replace(path, []byte("old"), true, []byte("new"), func() { write(t, path, "theirs") })
		left := asides(t, path)
		if !errors.Is(err, config.ErrConfigChanged) || len(left) != 1 || !strings.Contains(err.Error(), left[0]) ||
			read(t, path) != "theirs" || read(t, left[0]) != "old" {
			t.Fatalf("%v %q %v", err, read(t, path), left)
		}
	})
	t.Run("created while it was missing", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "config.toml")
		err := config.Replace(path, nil, false, []byte("new"), func() { write(t, path, "theirs") })
		if !errors.Is(err, config.ErrConfigChanged) || read(t, path) != "theirs" {
			t.Fatalf("%v %q", err, read(t, path))
		}
	})
	t.Run("deleted meanwhile", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := config.Replace(path, []byte("old"), true, []byte("new"), nil); !errors.Is(err, config.ErrConfigChanged) {
			t.Fatalf("%v", err)
		}
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("a deleted file was recreated")
		}
	})
	t.Run("an earlier copy is never overwritten", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "config.toml")
		write(t, path, "old")
		kept := path + ".earlier.prev"
		write(t, kept, "precious")
		if err := config.Replace(path, []byte("old"), true, []byte("new"), nil); err != nil || read(t, path) != "new" {
			t.Fatalf("%v %q", err, read(t, path))
		}
		if read(t, kept) != "precious" {
			t.Fatal("an earlier set-aside copy was overwritten")
		}
	})
	t.Run("an empty file deleted meanwhile", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "config.toml")
		// Read as an existing empty file; gone by the replacement.
		if err := config.Replace(path, nil, true, []byte("new"), nil); !errors.Is(err, config.ErrConfigChanged) {
			t.Fatalf("%v", err)
		}
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("a deleted file was recreated")
		}
	})
	t.Run("an empty file created before the replacement", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "config.toml")
		write(t, path, "")
		// Read as missing; an empty file is there by the replacement.
		if err := config.Replace(path, nil, false, []byte("new"), nil); !errors.Is(err, config.ErrConfigChanged) || read(t, path) != "" {
			t.Fatalf("%v %q", err, read(t, path))
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatal("their file is gone")
		}
		if left := asides(t, path); len(left) != 0 {
			t.Fatalf("%v", left)
		}
	})
	t.Run("cannot be set aside", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "config.toml")
		write(t, path, "old")
		// A directory rmctl cannot write: nothing is staged or set aside.
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
		if err := config.Replace(path, []byte("old"), true, []byte("new"), nil); err == nil || read(t, path) != "old" {
			t.Fatalf("%v %q", err, read(t, path))
		}
	})
	t.Run("no directory", func(t *testing.T) {
		t.Parallel()
		file := filepath.Join(t.TempDir(), "file")
		write(t, file, "")
		if err := config.Replace(filepath.Join(file, "config.toml"), nil, false, []byte("new"), nil); err == nil {
			t.Fatal("a config under a file was written")
		}
	})
}

// Setting a file aside never replaces what is at the new name, and a
// missing source stays recognizable.
func TestRenameNoReplace(t *testing.T) {
	t.Parallel()
	for name, rename := range map[string]func(string, string) error{
		"system":      config.RenameNoReplace,
		"check first": config.CheckThenRename,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			from, to := filepath.Join(dir, "config.toml"), filepath.Join(dir, "config.toml.x.prev")
			for path, body := range map[string]string{from: "new", to: "kept"} {
				if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := rename(from, to); !errors.Is(err, os.ErrExist) {
				t.Fatalf("occupied: %v", err)
			}
			if a, b := mustRead(t, from), mustRead(t, to); a != "new" || b != "kept" {
				t.Fatalf("%q %q", a, b)
			}
			free := filepath.Join(dir, "config.toml.y.prev")
			if err := rename(from, free); err != nil || mustRead(t, free) != "new" {
				t.Fatalf("free: %v", err)
			}
			if err := rename(from, filepath.Join(dir, "z")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("missing source: %v", err)
			}
		})
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestCacheSettings(t *testing.T) {
	t.Parallel()
	dirs := config.BaseDirs{Config: "/cfg", Cache: "/cache"}
	path := filepath.Join("/cfg", "rmctl", "config.toml")
	file := "[cache]\nttl = \"10m\"\noffline = true\n\n[profiles.work.cache]\nttl = \"1h\"\noffline = false\n\n[profiles.home.secrets]\nbackend = \"file\"\n"
	tests := []struct {
		name, profile, env string
		ttl                time.Duration
		offline            bool
	}{
		{name: "the profile's table", profile: "work", ttl: time.Hour},
		{name: "the global table", profile: "home", ttl: 10 * time.Minute, offline: true},
		{name: "the environment wins", profile: "work", env: "30s", ttl: 30 * time.Second},
		{name: "zero turns it off", profile: "work", env: "0", ttl: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := map[string]string{}
			if tt.env != "" {
				env[config.EnvCacheTTL] = tt.env
			}
			cfg, err := config.LoadFrom(env, tt.profile, dirs, files(map[string]string{path: file}))
			if err != nil || cfg.CacheTTL != tt.ttl || cfg.Offline != tt.offline || cfg.CacheDir != filepath.Join("/cache", "rmctl") {
				t.Fatalf("%+v %v", cfg, err)
			}
		})
	}
	if cfg, err := config.LoadFrom(nil, "", dirs, nil); err != nil || cfg.CacheTTL != 0 || cfg.Offline {
		t.Fatalf("defaults: %+v %v", cfg, err)
	}
	for name, tc := range map[string]struct {
		file string
		env  map[string]string
	}{
		"a bad global ttl":     {file: "[cache]\nttl = \"soon\"\n"},
		"a negative ttl":       {file: "[profiles.work.cache]\nttl = \"-1m\"\n"},
		"an unknown cache key": {file: "[cache]\nsize = 1\n"},
		"a bad environment":    {env: map[string]string{config.EnvCacheTTL: "x"}},
	} {
		if _, err := config.LoadFrom(tc.env, "work", dirs, files(map[string]string{path: tc.file})); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}
