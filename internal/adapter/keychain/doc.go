// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package keychain keeps the device and user tokens of a profile in the
// macOS login Keychain, as generic password items: service
// [DefaultService], account "<profile>/<secret>", readable after the first
// unlock on this device only, never synchronized to iCloud.
//
// The Keychain calls exist in a macOS build with cgo only; every other
// build answers each call with port.ErrKeychainUnsupported, so the rest of
// rmctl still builds and runs everywhere. No value is ever logged or put in
// an error.
package keychain
