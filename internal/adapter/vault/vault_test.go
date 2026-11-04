// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package vault_test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/altessa-s/rmctl/internal/adapter/secretfile"
	"github.com/altessa-s/rmctl/internal/adapter/vault"
	"github.com/altessa-s/rmctl/internal/port"
)

func TestVaultOverAStore(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "token.json")
	v := vault.New(secretfile.New(path), port.SecretsFile, path)
	if v.Backend() != port.SecretsFile || v.Location() != path {
		t.Fatalf("%s %s", v.Backend(), v.Location())
	}
	if !slices.Equal(v.Names(), []string{"deviceToken", "deviceID", "userToken"}) {
		t.Fatalf("%v", v.Names())
	}
	ctx := t.Context()
	if err := v.Put(ctx, "deviceToken", "t"); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := v.Get(ctx, "deviceToken"); got != "t" || !ok || err != nil {
		t.Fatalf("%q %v %v", got, ok, err)
	}
	if err := v.Delete(ctx, "deviceToken"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := v.Get(ctx, "deviceToken"); ok {
		t.Fatal("deleted")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("a nil store did not panic")
		}
	}()
	vault.New(nil, port.SecretsFile, "")
}

// The profile use case knows a registered profile by port.DeviceToken: it
// must be the name the transport keeps the device token under.
func TestDeviceTokenIsTheTransports(t *testing.T) {
	t.Parallel()
	v := vault.New(secretfile.New(filepath.Join(t.TempDir(), "token.json")), port.SecretsFile, "")
	if v.Names()[0] != port.DeviceToken {
		t.Fatalf("%v", v.Names())
	}
}
