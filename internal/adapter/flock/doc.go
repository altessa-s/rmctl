// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package flock takes the system's advisory file locks: flock on Unix,
// LockFileEx on Windows. A lock is released when its holder exits,
// however it exits, so there is no stale lock to judge or take over, and
// nobody can release another process's lock. config locks config.toml
// with it, and localfs a mirrored directory.
package flock
