// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands"
)

// isolate points the profile and the cache at a temporary directory, so a
// test never reads the token of the machine it runs on.
func isolate(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("RMCTL_CONFIG_DIR", dir+"/config")
	t.Setenv("RMCTL_CACHE_DIR", dir+"/cache")
	t.Setenv("RMCTL_PROFILE", "")
}

func TestRun(t *testing.T) {
	isolate(t)
	tests := []struct {
		args    []string
		code    int
		out     string
		errPart string
	}{
		{[]string{"version"}, 0, "0.0.0\n", ""},
		{[]string{"ls"}, 3, "", "not registered"},
		{[]string{"--profile", "Bad Name", "ls"}, 1, "", "profile"},
		{[]string{"ls", "--bogus"}, 2, "", "Run 'rmctl --help' for usage."},
		{[]string{"bogus"}, 2, "", "unknown command"},
	}
	for _, tc := range tests {
		var out, errOut bytes.Buffer
		code := run(tc.args, nil, &out, &errOut, func(int) { t.Error("exited") })
		if code != tc.code || out.String() != tc.out || !strings.Contains(errOut.String(), tc.errPart) {
			t.Errorf("%v: code %d out %q err %q", tc.args, code, out.String(), errOut.String())
		}
	}
}

func TestBuild(t *testing.T) {
	isolate(t)
	svc, closeFn, err := build(context.Background(), commands.Request{Profile: "work", Verbose: true}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if svc.Account == nil || svc.Browser == nil || svc.Fetcher == nil || svc.Writer == nil || svc.Watcher == nil || svc.Inspector == nil {
		t.Fatalf("%+v", svc)
	}
	if err := closeFn(); err != nil {
		t.Fatal(err)
	}
	info, err := svc.Account.Info(context.Background())
	if err != nil || info.Registered {
		t.Fatalf("a fresh profile is registered: %+v %v", info, err)
	}
	if _, _, err := build(context.Background(), commands.Request{}, nil); err == nil {
		t.Fatal("a nil log writer was accepted")
	}
	if _, _, err := build(context.Background(), commands.Request{Profile: "../escape"}, &bytes.Buffer{}); err == nil {
		t.Fatal("an unsafe profile name was accepted")
	}
}

func TestNewLogger(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	quiet, err := newLogger(&buf, false)
	if err != nil {
		t.Fatal(err)
	}
	quiet.Info("hidden")
	quiet.Warn("shown")
	loud, err := newLogger(&buf, true)
	if err != nil {
		t.Fatal(err)
	}
	loud.Debug("debugging")
	if strings.Contains(buf.String(), "hidden") || !strings.Contains(buf.String(), "shown") || !strings.Contains(buf.String(), "debugging") {
		t.Fatalf("%q", buf.String())
	}
	if !isTerminal(&buf) == false {
		t.Fatal("a buffer is a terminal")
	}
}
