// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package account

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/altessa-s/rmctl/cmd/rmctl/commands/clitest"
	"github.com/altessa-s/rmctl/cmd/rmctl/commands/cliutil"
	"github.com/altessa-s/rmctl/internal/port"
)

func run(t *testing.T, f *clitest.Fake, args ...string) clitest.Result {
	t.Helper()
	return clitest.Exec(t, f.Services(), clitest.Options{}, Commands, args...)
}

func TestCommands(t *testing.T) {
	t.Parallel()
	tests := []struct {
		args       []string
		call, want string
	}{
		{[]string{"register", "abcdefgh"}, "Register abcdefgh", "registered\n"},
		{[]string{"register", "abcdefgh", "--replace"}, "Register abcdefgh replace", "registered\n"},
		{[]string{"logout"}, "Logout", "logged out\n"},
		{[]string{"logout", "--revoke"}, "Logout revoke", "revoked and logged out\n"},
		{[]string{"account"}, "Info", "registered  yes\nlevel       connect\nsync scope  sync:fox\nsecrets     keychain\n"},
	}
	for _, tc := range tests {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			t.Parallel()
			f := &clitest.Fake{
				AccountInfo: port.AccountInfo{Registered: true, Plan: port.Plan{Level: "connect", SyncScope: "sync:fox"}},
				SecretsAt:   port.SecretsStatus{Backend: port.SecretsKeychain},
			}
			r := run(t, f, tc.args...)
			if r.Code != 0 || r.Out != tc.want || f.Calls()[0] != tc.call {
				t.Fatalf("code %d out %q calls %v err %v", r.Code, r.Out, f.Calls(), r.Error)
			}
		})
	}
}

func TestAccountUnregistered(t *testing.T) {
	t.Parallel()
	r := run(t, &clitest.Fake{}, "account")
	if r.Out != "registered  no\nlevel       -\nsync scope  -\nsecrets     -\n" {
		t.Fatalf("%q", r.Out)
	}
	r = run(t, &clitest.Fake{AccountInfo: port.AccountInfo{Registered: true, Plan: port.Plan{Level: "connect"}}}, "account", "--json")
	clitest.Golden(t, "account", r.Out)
}

func TestErrors(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"register", "x"}, {"logout"}, {"account"}} {
		r := run(t, &clitest.Fake{Err: port.ErrNotRegistered}, args...)
		if r.Code != cliutil.CodeNotRegistered || !strings.HasPrefix(r.Error.Error(), args[0]+": ") {
			t.Fatalf("%v: %d %v", args, r.Code, r.Error)
		}
	}
}

func TestUsage(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"register"}, {"register", "a", "b"}, {"logout", "x"}, {"account", "x"}} {
		if r := run(t, &clitest.Fake{}, args...); r.Code != cliutil.CodeUsage {
			t.Fatalf("%v: %d", args, r.Code)
		}
	}
}

func TestFactoryFailure(t *testing.T) {
	t.Parallel()
	boom := errors.New("no config")
	factory := func(context.Context, string, bool) (cliutil.Services, func() error, error) {
		return cliutil.Services{}, nil, boom
	}
	r := clitest.Exec(t, cliutil.Services{}, clitest.Options{Factory: factory}, Commands, "account")
	if !errors.Is(r.Error, boom) {
		t.Fatal(r.Error)
	}
	r = clitest.Exec(t, cliutil.Services{}, clitest.Options{}, Commands, "account")
	if !errors.Is(r.Error, cliutil.ErrNoService) {
		t.Fatal(r.Error)
	}
}

// A token store that cannot be read fails account rather than print an
// account that looks fine.
func TestAccountFailsWhenTheTokenStoreFails(t *testing.T) {
	t.Parallel()
	f := &clitest.Fake{AccountInfo: port.AccountInfo{Registered: true}, SecretsErr: errors.New("the Keychain is locked")}
	r := run(t, f, "account")
	if r.Code != cliutil.CodeError || !strings.Contains(r.Error.Error(), "token store: the Keychain is locked") || r.Out != "" {
		t.Fatalf("%d %v %q", r.Code, r.Error, r.Out)
	}
}
