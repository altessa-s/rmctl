// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package secretfile_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/altessa-s/rmcloud-go"

	"github.com/altessa-s/rmctl/internal/adapter/secretfile"
)

const token = "secret-device-token-value"

func newStore(t *testing.T) (*secretfile.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "profile", "token.json")
	return secretfile.New(path), path
}

func TestSaveReadForget(t *testing.T) {
	t.Parallel()
	s, path := newStore(t)
	ctx := t.Context()
	if _, ok, err := s.Secret(ctx, rmcloud.SecretDeviceToken); ok || err != nil {
		t.Fatalf("empty store: %v %v", ok, err)
	}
	if err := s.Save(ctx, rmcloud.SecretDeviceToken, token); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, rmcloud.SecretUserToken, "user"); err != nil {
		t.Fatal(err)
	}
	if v, ok, err := s.Secret(ctx, rmcloud.SecretDeviceToken); !ok || err != nil || v != token {
		t.Fatalf("read back %v %v", ok, err)
	}
	if s.Path() != path {
		t.Fatalf("path %s", s.Path())
	}
	if runtime.GOOS != "windows" {
		assertMode(t, path, 0o600)
		assertMode(t, filepath.Dir(path), 0o700)
	}
	if err := s.Forget(ctx, rmcloud.SecretDeviceID); err != nil {
		t.Fatalf("forgetting an absent name: %v", err)
	}
	if err := s.Forget(ctx, rmcloud.SecretDeviceToken); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Secret(ctx, rmcloud.SecretDeviceToken); ok {
		t.Fatal("forgotten value is still there")
	}
	if err := s.Forget(ctx, rmcloud.SecretUserToken); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("an empty store left its file: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 0 {
		t.Fatalf("staging files left behind: %v", entries)
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode %o, want %o", path, got, want)
	}
}

func TestRefusesAFileOthersCanRead(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("no permission bits on Windows")
	}
	s, path := newStore(t)
	if err := s.Save(t.Context(), rmcloud.SecretDeviceToken, token); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := s.Secret(t.Context(), rmcloud.SecretDeviceToken)
	if !errors.Is(err, secretfile.ErrExposed) || !strings.Contains(err.Error(), "chmod 600") {
		t.Fatalf("got %v", err)
	}
	if err := s.Save(t.Context(), rmcloud.SecretUserToken, "x"); !errors.Is(err, secretfile.ErrExposed) {
		t.Fatalf("save over an exposed file: %v", err)
	}
	if err := s.Forget(t.Context(), rmcloud.SecretUserToken); !errors.Is(err, secretfile.ErrExposed) {
		t.Fatalf("forget on an exposed file: %v", err)
	}
}

func TestMalformedFileDoesNotLeakTheValue(t *testing.T) {
	t.Parallel()
	s, path := newStore(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"deviceToken": "`+token), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := s.Secret(t.Context(), rmcloud.SecretDeviceToken)
	if err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("got %v", err)
	}
}

func TestRefusesADirectoryAndUnwritablePlaces(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := secretfile.New(dir)
	if _, _, err := s.Secret(t.Context(), rmcloud.SecretDeviceToken); err == nil {
		t.Fatal("read a directory as the token file")
	}
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	under := secretfile.New(filepath.Join(blocker, "token.json"))
	if err := under.Save(t.Context(), rmcloud.SecretDeviceToken, token); err == nil {
		t.Fatal("saved under a file")
	}
}

func TestNewNeedsAPath(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("an empty path did not panic")
		}
	}()
	secretfile.New("")
}

func TestUnreadableDirectory(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions and a non-root user")
	}
	dir := filepath.Join(t.TempDir(), "locked")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	s := secretfile.New(filepath.Join(dir, "token.json"))
	if err := s.Save(t.Context(), rmcloud.SecretDeviceToken, token); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if _, _, err := s.Secret(t.Context(), rmcloud.SecretDeviceToken); err == nil {
		t.Fatal("read through a directory without permissions")
	}
}

func TestNotAnObjectOfStrings(t *testing.T) {
	t.Parallel()
	for _, body := range []string{`["a"]`, `{"deviceToken": 7}`, `null`} {
		s, path := newStore(t)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		v, ok, err := s.Secret(t.Context(), rmcloud.SecretDeviceToken)
		if body == "null" {
			if err != nil || ok || v != "" {
				t.Fatalf("null: %q %v %v", v, ok, err)
			}
			continue
		}
		if err == nil {
			t.Fatalf("%s accepted", body)
		}
	}
}

func TestSaveOverANullFile(t *testing.T) {
	t.Parallel()
	s, path := newStore(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("null"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(t.Context(), rmcloud.SecretDeviceToken, token); err != nil {
		t.Fatal(err)
	}
	if v, ok, err := s.Secret(t.Context(), rmcloud.SecretDeviceToken); err != nil || !ok || v != token {
		t.Fatalf("%v %v", ok, err)
	}
	if err := s.Forget(t.Context(), rmcloud.SecretUserToken); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkSecret(b *testing.B) {
	s := secretfile.New(filepath.Join(b.TempDir(), "token.json"))
	if err := s.Save(b.Context(), rmcloud.SecretUserToken, token); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := s.Secret(b.Context(), rmcloud.SecretUserToken); err != nil {
			b.Fatal(err)
		}
	}
}

func TestReadOnlyDirectory(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions and a non-root user")
	}
	dir := filepath.Join(t.TempDir(), "ro")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	s := secretfile.New(filepath.Join(dir, "token.json"))
	if err := s.Save(t.Context(), rmcloud.SecretDeviceToken, token); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(t.Context(), rmcloud.SecretUserToken, "user"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if err := s.Save(t.Context(), rmcloud.SecretDeviceID, "id"); err == nil {
		t.Fatal("saved into a read-only directory")
	}
	if err := s.Forget(t.Context(), rmcloud.SecretDeviceToken); err == nil {
		t.Fatal("rewrote a file in a read-only directory")
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := s.Forget(t.Context(), rmcloud.SecretDeviceToken); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	if err := s.Forget(t.Context(), rmcloud.SecretUserToken); err == nil {
		t.Fatal("removed the last value's file from a read-only directory")
	}
}
