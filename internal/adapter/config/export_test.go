// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package config

// LockFile and Restore are exported for the tests of package config_test.
var (
	LockFile = lockFile
	Restore  = restore
	Replace  = replace

	RenameNoReplace = renameNoReplace
	CheckThenRename = checkThenRename
)
