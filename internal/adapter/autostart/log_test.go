// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package autostart_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/internal/adapter/autostart"
)

// The log rotates when a write would take it past its size, keeps the
// newest rotated ones, and never splits a write.
func TestRotatingLog(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "logs", "mirror.log")
	l, err := autostart.OpenLog(path, 10, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"aaaa\n", "bbbb\n", "cccc\n", "dddd\n", "eeee\n", "ffff\n", "gggg\n"} {
		if _, err = l.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	if err = l.Close(); err != nil {
		t.Fatal(err)
	}
	for file, want := range map[string]string{path: "gggg\n", path + ".1": "eeee\nffff\n", path + ".2": "cccc\ndddd\n"} {
		if got, rerr := os.ReadFile(file); rerr != nil || string(got) != want {
			t.Errorf("%s: %q %v", file, got, rerr)
		}
	}
	if _, err = os.Stat(path + ".3"); !errors.Is(err, fs.ErrNotExist) {
		t.Error("a third rotated log was kept")
	}
	if info, serr := os.Stat(path); serr == nil && info.Mode().Perm()&0o077 != 0 && runtime.GOOS != "windows" {
		t.Errorf("mode %v", info.Mode().Perm())
	}
	if _, err = l.Write([]byte("x")); !errors.Is(err, fs.ErrClosed) {
		t.Fatal(err)
	}
	if err = l.Close(); err != nil {
		t.Fatal(err)
	}
	// Opened again, it appends.
	if l, err = autostart.OpenLog(path, 100, 2); err != nil {
		t.Fatal(err)
	}
	if _, err = l.Write([]byte("hhhh\n")); err != nil {
		t.Fatal(err)
	}
	_ = l.Close()
	if got, _ := os.ReadFile(path); string(got) != "gggg\nhhhh\n" {
		t.Fatalf("%q", got)
	}
}

// A rotation that fails keeps the log open: the lines go on into it.
func TestRotatingLogRotationFails(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "mirror.log")
	// A directory that is not empty stands where the rotated log goes.
	if err := os.MkdirAll(filepath.Join(path+".1", "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	l, err := autostart.OpenLog(path, 6, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"aaaa\n", "bbbb\n", "cccc\n"} {
		if _, err = l.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	_ = l.Close()
	if got, _ := os.ReadFile(path); string(got) != "aaaa\nbbbb\ncccc\n" {
		t.Fatalf("%q", got)
	}
}

func TestOpenLogRefused(t *testing.T) {
	t.Parallel()
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := autostart.OpenLog(filepath.Join(file, "mirror.log"), 10, 1); err == nil {
		t.Fatal("a log below a file")
	}
	dir := t.TempDir()
	if _, err := autostart.OpenLog(dir, 10, 1); err == nil {
		t.Fatal("a directory opened as a log")
	}
}

// Tail reaches into the newest rotated log for the lines the current one
// lacks; From reads the whole lines written past an offset, from the start
// of a log rotated meanwhile.
func TestTailAndFrom(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "mirror.log")
	if lines, end, err := autostart.Tail(path, 5); err != nil || lines != nil || end != 0 {
		t.Fatal(lines, end, err)
	}
	if err := os.WriteFile(path+".1", []byte("one\r\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("three\nfour\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lines, end, err := autostart.Tail(path, 3)
	if err != nil || strings.Join(lines, ",") != "two,three,four" || end != 11 {
		t.Fatal(lines, end, err)
	}
	if lines, _, _ = autostart.Tail(path, 1); strings.Join(lines, ",") != "four" {
		t.Fatal(lines)
	}
	if err = os.WriteFile(path, []byte("three\nfour\nfive\nsix (half"), 0o600); err != nil {
		t.Fatal(err)
	}
	if lines, end, err = autostart.From(path, 11); err != nil || strings.Join(lines, ",") != "five" || end != 16 {
		t.Fatal(lines, end, err)
	}
	// A line half written is not tailed: it comes whole from the offset.
	if lines, end, err = autostart.Tail(path, 2); err != nil || strings.Join(lines, ",") != "four,five" || end != 16 {
		t.Fatal(lines, end, err)
	}
	if lines, end, err = autostart.From(path, end); err != nil || lines != nil || end != 16 {
		t.Fatal(lines, end, err)
	}
	if err = os.WriteFile(path, []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if lines, end, err = autostart.From(path, 16); err != nil || strings.Join(lines, ",") != "new" || end != 4 {
		t.Fatal(lines, end, err)
	}
	dir := t.TempDir()
	if _, _, err = autostart.Tail(dir, 1); err == nil {
		t.Fatal("a directory read as a log")
	}
	if _, _, err = autostart.From(dir, 0); err == nil {
		t.Fatal("a directory read as a log")
	}
	if err = os.WriteFile(filepath.Join(dir, "x"), []byte("a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(filepath.Join(dir, "x.1"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err = autostart.Tail(filepath.Join(dir, "x"), 5); err == nil {
		t.Fatal("a directory read as a rotated log")
	}
}
