// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

//go:build unix

package opener_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/internal/adapter/opener"
)

func TestCommandOpens(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out := filepath.Join(dir, "opened")
	// The program gets the file after its own arguments.
	c := opener.Command{Name: "sh", Args: []string{"-c", `printf %s "$1" > "$0"`, out}, Grace: 10 * time.Second}
	if err := c.Open(t.Context(), "/tmp/a file & more.pdf"); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(out); err != nil || string(data) != "/tmp/a file & more.pdf" {
		t.Fatalf("%q %v", data, err)
	}
}

func TestCommandFails(t *testing.T) {
	t.Parallel()
	if err := (opener.Command{Name: "false", Grace: 10 * time.Second}).Open(t.Context(), "x.pdf"); err == nil ||
		!strings.Contains(err.Error(), "open x.pdf with false") {
		t.Fatal(err)
	}
	if err := (opener.Command{Name: filepath.Join(t.TempDir(), "no-such-opener"), Grace: time.Second}).Open(t.Context(), "x.pdf"); err == nil {
		t.Fatal("a missing opener opened")
	}
}

// An opener that runs the viewer in its place is left running.
func TestCommandDoesNotWaitForTheViewer(t *testing.T) {
	t.Parallel()
	start := time.Now()
	if err := (opener.Command{Name: "sleep", Grace: 50 * time.Millisecond}).Open(t.Context(), "2"); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Fatalf("waited %v", took)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := (opener.Command{Name: "sleep", Grace: time.Minute}).Open(ctx, "2"); err == nil {
		t.Fatal("a canceled wait succeeded")
	}
}
