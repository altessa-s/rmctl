//go:build !(darwin && cgo)

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package system_test

import (
	"errors"
	"testing"

	"github.com/altessa-s/rmctl/internal/adapter/keychain/system"
	"github.com/altessa-s/rmctl/internal/port"
)

func TestUnsupported(t *testing.T) {
	t.Parallel()
	var k system.Keychain
	if _, _, err := k.Get("s", "a"); !errors.Is(err, port.ErrKeychainUnsupported) {
		t.Fatal(err)
	}
	if err := k.Put("s", "a", "l", "v"); !errors.Is(err, port.ErrKeychainUnsupported) {
		t.Fatal(err)
	}
	if err := k.Delete("s", "a"); !errors.Is(err, port.ErrKeychainUnsupported) {
		t.Fatal(err)
	}
}
