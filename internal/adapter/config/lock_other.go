//go:build !unix && !windows

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package config

import "os"

// tryLock has no advisory lock to take on this system: the re-read before
// the replacement still refuses a file another writer changed.
func tryLock(*os.File) (bool, error) { return false, nil }

func unlock(*os.File) error { return nil }
