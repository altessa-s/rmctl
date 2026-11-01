// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package system is the login Keychain itself: generic password items read,
// added or updated, and deleted through Security.framework, in a macOS build
// with cgo. Every other build answers port.ErrKeychainUnsupported. It holds
// no logic of rmctl's; the integration test exercises it against the real
// Keychain.
package system
