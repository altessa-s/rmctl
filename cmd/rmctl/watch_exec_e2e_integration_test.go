//go:build integration && !windows

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/altessa-s/rmctl/internal/clouddouble"
)

// waitFor polls cond until it holds or the test's patience runs out.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("gave up waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// watch --exec runs a real program per batch of events, with the event in
// its environment and on its standard input, and never in its command line;
// Ctrl-C ends watch.
func TestE2E_WatchExec(t *testing.T) {
	e := newE2E(t)
	e.register()
	e.cloud.Seed(t,
		clouddouble.Item{ID: editFolderID, Name: "Books", Kind: clouddouble.Folder},
		clouddouble.Item{ID: editDocID, Name: "Paper; rm -rf $HOME", Parent: editFolderID, Kind: clouddouble.PDF},
	)
	dir := t.TempDir()
	t.Setenv("WATCH_OUT", dir)
	script := `n=$(ls "$WATCH_OUT" | wc -l | tr -d ' '); cat > "$WATCH_OUT/run-$n.json"; ` +
		`printf '%s|%s|%s|%s\n' "$RMCTL_EVENT" "$RMCTL_PATH" "$RMCTL_COUNT" "$1" > "$WATCH_OUT/run-$n.env"`

	var out bytes.Buffer
	var errOut lockedBuffer
	code := make(chan int, 1)
	go func() {
		code <- run([]string{"watch", "--shell", "--exec", script, "--debounce", "200ms", "--only", "document", "--", "arg one"},
			nil, &out, &errOut, func(c int) { t.Errorf("exit(%d)", c) }, e.cloud.Options()...)
	}()
	waitFor(t, "the socket", func() bool { return e.cloud.Broadcast(clouddouble.SyncComplete(editFolderID, "Books")) > 0 })
	// The folder's events are filtered out and their batch passes; the
	// document's two events make one run.
	time.Sleep(400 * time.Millisecond)
	e.cloud.Broadcast(clouddouble.SyncComplete(editDocID, "Paper; rm -rf $HOME"))
	e.cloud.Broadcast(clouddouble.SyncComplete(editDocID, "Paper; rm -rf $HOME"))
	waitFor(t, "a run", func() bool {
		_, err := os.Stat(filepath.Join(dir, "run-0.env"))
		return err == nil
	})
	time.Sleep(400 * time.Millisecond)
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case c := <-code:
		if c != 130 {
			t.Fatalf("exit %d\n%s", c, errOut.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("watch did not end at Ctrl-C")
	}
	env, err := os.ReadFile(filepath.Join(dir, "run-0.env"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(env)); got != "SyncComplete|/Books/Paper; rm -rf $HOME|2|arg one" {
		t.Fatalf("%q", got)
	}
	data, err := os.ReadFile(filepath.Join(dir, "run-0.json"))
	if err != nil {
		t.Fatal(err)
	}
	var input struct {
		Profile string `json:"profile"`
		Events  []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"events"`
	}
	if err := json.Unmarshal(data, &input); err != nil || input.Profile != "default" || len(input.Events) != 2 || input.Events[0].ID != editDocID {
		t.Fatalf("%v %s", err, data)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Fatalf("%d files: the folder's events ran too", len(entries))
	}
}

// lockedBuffer is a buffer several goroutines write.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}
