//go:build !(darwin && cgo)

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package system

import "github.com/altessa-s/rmctl/internal/port"

// Keychain is no Keychain: this build is not a macOS build with cgo.
type Keychain struct{}

// Get reads the item of service and account; found false when missing.
func (Keychain) Get(string, string) (string, bool, error) {
	return "", false, port.ErrKeychainUnsupported
}

// Put adds the item, or updates it when it is there.
func (Keychain) Put(string, string, string, string) error { return port.ErrKeychainUnsupported }

// Delete removes the item; a missing one is not an error.
func (Keychain) Delete(string, string) error { return port.ErrKeychainUnsupported }
