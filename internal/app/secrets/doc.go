// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package secrets reports where a profile keeps its tokens and moves them
// between the token file and the macOS Keychain.
//
// A migration never loses a token: every token is written to the target and
// read back before anything is removed from the source, the configuration
// file is changed (when asked) before the source is cleared, and a failure
// before that point undoes the target's writes and leaves the source as it
// was.
package secrets
