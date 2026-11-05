// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/altessa-s/rmctl/internal/adapter/config"
)

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestProfileDirs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write(t, filepath.Join(root, "work", "token.json"), "{}")
	// The token file next to a person's file is still a profile's.
	write(t, filepath.Join(root, "mixed", "token.json"), "{}")
	write(t, filepath.Join(root, "mixed", "notes.txt"), "x")
	if err := os.Mkdir(filepath.Join(root, "empty"), 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "staged", ".token.json.123.part"), "{}")
	// Not profiles: a person's directory, a name no profile has, a file.
	write(t, filepath.Join(root, "docs", "notes.txt"), "x")
	write(t, filepath.Join(root, "Bad Name", "token.json"), "{}")
	write(t, filepath.Join(root, "config.toml"), "")
	got, err := config.ProfileDirs(root)
	if err != nil || !slices.Equal(got, []string{"empty", "mixed", "staged", "work"}) {
		t.Fatalf("%v %v", got, err)
	}
	if got, err = config.ProfileDirs(filepath.Join(root, "missing")); err != nil || got != nil {
		t.Fatalf("%v %v", got, err)
	}
	if _, err = config.ProfileDirs(filepath.Join(root, "config.toml")); err == nil {
		t.Fatal("a file read as a directory")
	}
}

func TestProfileDirsUnreadable(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("permissions do not stop this user")
	}
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	if _, err := config.ProfileDirs(root); err == nil {
		t.Fatal("an unreadable directory went unnoticed")
	}
}

func TestDirExists(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	write(t, file, "")
	for path, want := range map[string]bool{dir: true, file: false, filepath.Join(dir, "missing"): false} {
		if got, err := config.DirExists(path); err != nil || got != want {
			t.Errorf("%s: %v %v", path, got, err)
		}
	}
	if _, err := config.DirExists(filepath.Join(file, "below")); err == nil && runtime.GOOS != "windows" {
		t.Fatal("a path below a file")
	}
}

func TestRemoveProfileDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cfg := config.Config{ConfigDir: filepath.Join(root, "work")}
	write(t, filepath.Join(cfg.ConfigDir, "token.json"), "{}")
	write(t, filepath.Join(cfg.ConfigDir, ".token.json.9.part"), "{}")
	existed, kept, err := config.RemoveProfileDir(cfg)
	if err != nil || !existed || kept {
		t.Fatalf("%v %v %v", existed, kept, err)
	}
	if _, err := os.Stat(cfg.ConfigDir); !os.IsNotExist(err) {
		t.Fatalf("the directory is still there: %v", err)
	}
	if existed, kept, err = config.RemoveProfileDir(cfg); err != nil || existed || kept {
		t.Fatalf("again: %v %v %v", existed, kept, err)
	}
	// A person's file stays, and the directory with it.
	write(t, filepath.Join(cfg.ConfigDir, "token.json"), "{}")
	write(t, filepath.Join(cfg.ConfigDir, "notes.txt"), "mine")
	if err := os.Mkdir(filepath.Join(cfg.ConfigDir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if existed, kept, err = config.RemoveProfileDir(cfg); err != nil || !existed || !kept {
		t.Fatalf("%v %v %v", existed, kept, err)
	}
	if data, err := os.ReadFile(filepath.Join(cfg.ConfigDir, "notes.txt")); err != nil || string(data) != "mine" {
		t.Fatalf("%q %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(cfg.ConfigDir, "token.json")); !os.IsNotExist(err) {
		t.Fatal("the token file stayed")
	}
	// A path that is not a directory.
	file := filepath.Join(root, "file")
	write(t, file, "")
	if _, _, err := config.RemoveProfileDir(config.Config{ConfigDir: file}); err == nil {
		t.Fatal("a file removed as a directory")
	}
}

func TestTableProfiles(t *testing.T) {
	t.Parallel()
	m := files(map[string]string{
		"/c.toml":   "[profiles.work.secrets]\nbackend = \"file\"\n[profiles.home.cache]\noffline = true\n",
		"/bad.toml": "[nope]\n",
	})
	if got, err := config.TableProfiles("/c.toml", m); err != nil || !slices.Equal(got, []string{"home", "work"}) {
		t.Fatalf("%v %v", got, err)
	}
	if got, err := config.TableProfiles("/missing.toml", m); err != nil || got != nil {
		t.Fatalf("%v %v", got, err)
	}
	if _, err := config.TableProfiles("/bad.toml", m); !errors.Is(err, config.ErrConfig) {
		t.Fatal(err)
	}
	if got, err := config.TableProfiles(filepath.Join(t.TempDir(), "none.toml"), nil); err != nil || got != nil {
		t.Fatalf("%v %v", got, err)
	}
}

func TestLoadNamesTheProfileTableAndRoot(t *testing.T) {
	t.Parallel()
	env := map[string]string{config.EnvConfigDir: "/cfg", config.EnvCacheDir: "/cache"}
	read := files(map[string]string{filepath.Join("/cfg", "config.toml"): "[profiles.work.cache]\nttl = \"1m\"\n"})
	for profile, want := range map[string]bool{"work": true, "home": false} {
		cfg, err := config.LoadFrom(env, profile, config.BaseDirs{}, read)
		if err != nil || cfg.ProfileTable != want || cfg.ConfigRoot != "/cfg" {
			t.Errorf("%s: %+v %v", profile, cfg, err)
		}
	}
}

func TestRemoveProfile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, in, want string
	}{
		{
			name: "the appended table goes, and the separating line",
			in:   "[secrets]\nbackend = \"file\"\n\n[profiles.work.secrets]\nbackend = \"keychain\"\n",
			want: "[secrets]\nbackend = \"file\"\n",
		},
		{
			name: "only the profile's tables",
			in: "# where tokens are kept\n[secrets]\nbackend = \"file\" # all of them\n\n" +
				"# the work account\n[profiles.work.secrets]\nbackend = \"keychain\"\n# about the cache below\n\n" +
				"[profiles.workshop.cache]\nttl = \"1m\"\n\n" +
				"[ profiles . \"work\" . cache ] # spaced and quoted\noffline = true\n\n" +
				"[profiles.home.cache]\noffline = false\n",
			want: "# where tokens are kept\n[secrets]\nbackend = \"file\" # all of them\n\n" +
				"# the work account\n# about the cache below\n\n" +
				"[profiles.workshop.cache]\nttl = \"1m\"\n\n" +
				"[profiles.home.cache]\noffline = false\n",
		},
		{
			name: "the whole file",
			in:   "[profiles.work]\n[profiles.work.secrets]\nbackend = \"file\"\n\n",
			want: "",
		},
		{
			name: "an inline table under the profile's header",
			in:   "[profiles.work]\nsecrets = { backend = \"file\" }\n\n[cache]\nttl = \"5m\"\n",
			want: "[cache]\nttl = \"5m\"\n",
		},
		{
			name: "CRLF lines",
			in:   "[cache]\r\nttl = \"5m\"\r\n\r\n[profiles.work.cache]\r\noffline = true\r\n",
			want: "[cache]\r\nttl = \"5m\"\r\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "config.toml")
			write(t, path, tt.in)
			removed, err := config.Writer{Path: path}.RemoveProfile("work")
			if err != nil || !removed {
				t.Fatalf("%v %v", removed, err)
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != tt.want {
				t.Fatalf("got\n%q\nwant\n%q (%v)", got, tt.want, err)
			}
		})
	}
}

func TestRemoveProfileLeavesAFileWithoutIt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	const in = "[profiles.home.cache]\noffline = true\n"
	write(t, path, in)
	removed, err := config.Writer{Path: path}.RemoveProfile("work")
	if err != nil || removed {
		t.Fatalf("%v %v", removed, err)
	}
	if got, _ := os.ReadFile(path); string(got) != in {
		t.Fatalf("%q", got)
	}
	if _, err := os.Stat(path + ".lock"); !os.IsNotExist(err) {
		t.Fatal("a lock was taken for nothing")
	}
	// No file at all.
	if removed, err = (config.Writer{Path: filepath.Join(dir, "none.toml")}).RemoveProfile("work"); err != nil || removed {
		t.Fatalf("%v %v", removed, err)
	}
}

func TestRemoveProfileRefuses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, in string
		want     error
	}{
		{"dotted keys under [profiles]", "[profiles]\nwork.secrets.backend = \"file\"\n", config.ErrProfileTables},
		{"an inline table of every profile", "profiles = { work = { secrets = { backend = \"file\" } } }\n", config.ErrProfileTables},
		{"a file rmctl does not read", "[nope]\n", config.ErrConfig},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "config.toml")
			write(t, path, tt.in)
			if _, err := (config.Writer{Path: path}).RemoveProfile("work"); !errors.Is(err, tt.want) {
				t.Fatal(err)
			}
			if got, _ := os.ReadFile(path); string(got) != tt.in {
				t.Fatalf("changed: %q", got)
			}
		})
	}
	if _, err := (config.Writer{Path: filepath.Join(t.TempDir(), "c.toml")}).RemoveProfile("Bad Name"); !errors.Is(err, config.ErrProfile) {
		t.Fatal(err)
	}
}

// What the lock-free look saw is read again under the lock: a profile gone
// meanwhile leaves the file alone, and a read that fails is an error.
func TestRemoveProfileRereadsUnderTheLock(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	write(t, path, "")
	reads := 0
	answers := []string{"[profiles.work.cache]\noffline = true\n", "[profiles.home.cache]\noffline = true\n"}
	read := func(string) ([]byte, bool, error) {
		reads++
		return []byte(answers[min(reads, len(answers))-1]), true, nil
	}
	if removed, err := (config.Writer{Path: path, Read: read}).RemoveProfile("work"); err != nil || removed {
		t.Fatalf("%v %v", removed, err)
	}
	boom := errors.New("boom")
	reads = 0
	failing := func(string) ([]byte, bool, error) {
		reads++
		if reads > 1 {
			return nil, false, boom
		}
		return []byte(answers[0]), true, nil
	}
	if _, err := (config.Writer{Path: path, Read: failing}).RemoveProfile("work"); !errors.Is(err, boom) {
		t.Fatal(err)
	}
	reads = 0
	broken := func(string) ([]byte, bool, error) {
		reads++
		if reads > 1 {
			return []byte("[nope]\n"), true, nil
		}
		return []byte(answers[0]), true, nil
	}
	if _, err := (config.Writer{Path: path, Read: broken}).RemoveProfile("work"); !errors.Is(err, config.ErrConfig) {
		t.Fatal(err)
	}
}

// A file a person changed between the read and the write keeps the
// person's change: nothing is written.
func TestRemoveProfileRefusesAConcurrentEdit(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	write(t, path, "[profiles.home.cache]\noffline = true\n")
	read := func(string) ([]byte, bool, error) {
		return []byte("[profiles.work.cache]\noffline = true\n"), true, nil
	}
	if _, err := (config.Writer{Path: path, Read: read}).RemoveProfile("work"); !errors.Is(err, config.ErrConfigChanged) {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "[profiles.home.cache]\noffline = true\n" {
		t.Fatalf("%q", got)
	}
}

func TestRemoveProfileLockBusy(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	write(t, path, "[profiles.work.cache]\noffline = true\n")
	unlock, err := config.LockFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if _, err := (config.Writer{Path: path}).RemoveProfile("work"); !errors.Is(err, config.ErrConfigBusy) {
		t.Fatal(err)
	}
}
