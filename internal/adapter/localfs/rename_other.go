//go:build !linux && !darwin && !windows

// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package localfs

// renameNoReplace looks at to before renaming: this system has no rename
// that refuses to replace a file.
func renameNoReplace(from, to string) error { return renameChecked(from, to) }
