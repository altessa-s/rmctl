// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cliutil

import (
	"bytes"
	"context"
	"testing"

	"github.com/spf13/cobra"
)

// request runs a command of access with args and returns what it asked the
// factory, and the error its services came with.
func request(t *testing.T, access Access, offline bool, args ...string) (Request, error) {
	t.Helper()
	var got Request
	env := NewEnv(Streams{Getenv: envOf(nil)}, func(_ context.Context, req Request) (Services, func() error, error) {
		got = req
		return Services{Offline: offline || (req.Offline != nil && *req.Offline)}, func() error { return nil }, nil
	})
	var svcErr error
	root := &cobra.Command{Use: "rmctl", SilenceErrors: true, SilenceUsage: true}
	AddPersistentFlags(root, env)
	child := WithAccess(&cobra.Command{Use: "x", RunE: func(cmd *cobra.Command, _ []string) error {
		_, svcErr = env.Services(cmd)
		return nil
	}}, access)
	root.AddCommand(child)
	root.SetOut(&bytes.Buffer{})
	root.SetArgs(append([]string{"x"}, args...))
	if err := root.Execute(); err != nil {
		return got, err
	}
	return got, svcErr
}

func TestAccess(t *testing.T) {
	t.Parallel()
	if req, err := request(t, AccessLive, false); err != nil || req.ReadOnly || req.Offline != nil || req.NoCache {
		t.Fatalf("%+v %v", req, err)
	}
	if req, err := request(t, AccessRead, false, "--no-cache"); err != nil || !req.ReadOnly || !req.NoCache {
		t.Fatalf("%+v %v", req, err)
	}
	if req, err := request(t, AccessOffline, false, "--offline"); err != nil || req.Offline == nil || !*req.Offline {
		t.Fatalf("%+v %v", req, err)
	}
	if req, err := request(t, AccessOffline, true, "--offline=false"); err != nil || req.Offline == nil || *req.Offline {
		// The flag says online; the factory still answered offline, and an
		// offline-capable command takes that.
		t.Fatalf("%+v %v", req, err)
	}
	// What needs the cloud refuses to run offline, whether the flag or the
	// profile made it so.
	for _, access := range []Access{AccessLive, AccessRead} {
		if _, err := request(t, access, false, "--offline"); ExitCode(err) != CodeUsage {
			t.Fatalf("%q with --offline: %v", access, err)
		}
		if _, err := request(t, access, true); ExitCode(err) != CodeUsage {
			t.Fatalf("%q offline by configuration: %v", access, err)
		}
	}
	if _, err := request(t, AccessOffline, false, "--offline", "--no-cache"); ExitCode(err) != CodeUsage {
		t.Fatalf("--offline --no-cache: %v", err)
	}
	if AccessOf(&cobra.Command{}) != AccessLive {
		t.Fatal("an unmarked command is not live")
	}
}
