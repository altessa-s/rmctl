// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package account_test

import (
	"errors"
	"testing"

	"github.com/altessa-s/rmctl/internal/app/account"
	"github.com/altessa-s/rmctl/internal/port"
	"github.com/altessa-s/rmctl/internal/porttest"
)

var errBoom = errors.New("boom")

func cloud(registered bool) *porttest.Cloud {
	c := porttest.NewCloud()
	c.ValidCode = "abcdefgh"
	c.Registered = registered
	c.AccountPlan = port.Plan{Level: "connect", SyncScope: "sync:fox"}
	return c
}

func TestRegister(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		registered bool
		code       string
		replace    bool
		fail       string
		want       error
		forgets    int
	}{
		{name: "fresh", code: "abcdefgh"},
		{name: "trimmed", code: "  abcdefgh\n"},
		{name: "empty", code: "  ", want: account.ErrEmptyCode},
		{name: "refused code", code: "zzzzzzzz", want: porttest.ErrInvalidCode},
		{name: "already registered", registered: true, code: "abcdefgh", want: account.ErrAlreadyRegistered},
		{name: "replace", registered: true, code: "abcdefgh", replace: true, forgets: 1},
		{name: "probe fails", code: "abcdefgh", fail: "IsRegistered", want: errBoom},
		{name: "forget fails", registered: true, code: "abcdefgh", replace: true, fail: "Forget", want: errBoom, forgets: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := cloud(tc.registered)
			if tc.fail != "" {
				c.Fail[tc.fail] = errBoom
			}
			err := account.New(c).Register(t.Context(), tc.code, tc.replace)
			if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if c.Count("Forget") != tc.forgets {
				t.Fatalf("%d forgets", c.Count("Forget"))
			}
		})
	}
}

func TestLogout(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name               string
		registered, revoke bool
		fail               string
		want               error
		revokes            int
	}{
		{name: "not registered"},
		{name: "forget", registered: true},
		{name: "revoke", registered: true, revoke: true, revokes: 1},
		{name: "revoke fails", registered: true, revoke: true, fail: "Revoke", want: errBoom, revokes: 1},
		{name: "probe fails", fail: "IsRegistered", want: errBoom},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := cloud(tc.registered)
			if tc.fail != "" {
				c.Fail[tc.fail] = errBoom
			}
			err := account.New(c).Logout(t.Context(), tc.revoke)
			if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Fatalf("got %v", err)
			}
			if c.Count("Revoke") != tc.revokes {
				t.Fatalf("%d revokes", c.Count("Revoke"))
			}
			if tc.want == nil && c.Registered {
				t.Fatal("still registered")
			}
		})
	}
}

func TestInfo(t *testing.T) {
	t.Parallel()
	info, err := account.New(cloud(true)).Info(t.Context())
	if err != nil || !info.Registered || info.Plan.Level != "connect" {
		t.Fatalf("%v %+v", err, info)
	}
	info, err = account.New(cloud(false)).Info(t.Context())
	if err != nil || info.Registered {
		t.Fatalf("%v %+v", err, info)
	}
	c := cloud(true)
	c.Fail["Plan"] = errBoom
	if _, err := account.New(c).Info(t.Context()); !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
	c = cloud(true)
	c.Fail["IsRegistered"] = errBoom
	if _, err := account.New(c).Info(t.Context()); !errors.Is(err, errBoom) {
		t.Fatalf("got %v", err)
	}
}

func TestNewPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("no panic")
		}
	}()
	account.New(nil)
}
