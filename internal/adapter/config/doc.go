// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package config resolves a profile: where its token file and the blob
// cache live, and which hosts it talks to. Load is a pure function of the
// environment map, the --profile flag and the base directories, so it is
// tested without touching the process environment.
package config
