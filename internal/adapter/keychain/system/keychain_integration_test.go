//go:build integration && darwin && cgo

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// The real login Keychain. Skipped unless RMCTL_KEYCHAIN_TESTS=1: a CI
// runner's Keychain may be locked or absent. Every item goes under a
// service of its own and is deleted when the test ends.

package system_test

import (
	"crypto/rand"
	"fmt"
	"os"
	"testing"

	"github.com/altessa-s/rmcloud-go"

	"github.com/altessa-s/rmctl/internal/adapter/keychain"
	"github.com/altessa-s/rmctl/internal/adapter/keychain/system"

	gokeychain "github.com/keybase/go-keychain"
)

func service(t *testing.T) string {
	t.Helper()
	if os.Getenv("RMCTL_KEYCHAIN_TESTS") != "1" {
		t.Skip("set RMCTL_KEYCHAIN_TESTS=1 to use the login Keychain")
	}
	return fmt.Sprintf("io.altessa.rmctl.test.%s", rand.Text())
}

func TestIntegration_KeychainItems(t *testing.T) {
	svc := service(t)
	var k system.Keychain
	t.Cleanup(func() { _ = k.Delete(svc, "work/deviceToken") })
	if _, ok, err := k.Get(svc, "work/deviceToken"); ok || err != nil {
		t.Fatalf("%v %v", ok, err)
	}
	if err := k.Put(svc, "work/deviceToken", "rmctl test", "first"); err != nil {
		t.Fatal(err)
	}
	if err := k.Put(svc, "work/deviceToken", "rmctl test", "second"); err != nil {
		t.Fatal("an update failed:", err)
	}
	if v, ok, err := k.Get(svc, "work/deviceToken"); v != "second" || !ok || err != nil {
		t.Fatalf("%q %v %v", v, ok, err)
	}
	if err := k.Delete(svc, "work/deviceToken"); err != nil {
		t.Fatal(err)
	}
	if err := k.Delete(svc, "work/deviceToken"); err != nil {
		t.Fatal("deleting a missing item failed:", err)
	}
}

func TestIntegration_KeychainStore(t *testing.T) {
	svc := service(t)
	s := keychain.New(svc, "work")
	t.Cleanup(func() {
		for _, n := range []rmcloud.SecretName{rmcloud.SecretDeviceToken, rmcloud.SecretUserToken} {
			_ = s.Forget(t.Context(), n)
		}
	})
	if err := s.Save(t.Context(), rmcloud.SecretUserToken, "token"); err != nil {
		t.Fatal(err)
	}
	if v, ok, err := s.Secret(t.Context(), rmcloud.SecretUserToken); v != "token" || !ok || err != nil {
		t.Fatalf("%q %v %v", v, ok, err)
	}
}

// An item someone left with weaker attributes — readable whenever the Mac is
// unlocked — is replaced whole by Put, not updated in place: one item, the
// new value, created with rmctl's own attributes.
func TestIntegration_KeychainReplacesForeignAttributes(t *testing.T) {
	svc := service(t)
	var k system.Keychain
	t.Cleanup(func() { _ = k.Delete(svc, "work/userToken") })
	foreign := gokeychain.NewGenericPassword(svc, "work/userToken", "someone else", []byte("old"), "")
	foreign.SetAccessible(gokeychain.AccessibleWhenUnlocked)
	if err := gokeychain.AddItem(foreign); err != nil {
		t.Fatal(err)
	}
	if err := k.Put(svc, "work/userToken", "rmctl work userToken", "new"); err != nil {
		t.Fatal(err)
	}
	q := gokeychain.NewItem()
	q.SetSecClass(gokeychain.SecClassGenericPassword)
	q.SetService(svc)
	q.SetAccount("work/userToken")
	q.SetMatchLimit(gokeychain.MatchLimitAll)
	q.SetReturnAttributes(true)
	got, err := gokeychain.QueryItem(q)
	if err != nil || len(got) != 1 || got[0].Label != "rmctl work userToken" {
		t.Fatalf("%d items, %v", len(got), err)
	}
	if value, found, err := k.Get(svc, "work/userToken"); err != nil || !found || value != "new" {
		t.Fatalf("value %v %v", found, err)
	}
}
