// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package localfs implements port.LocalFiles on the machine's file system.
// Files are produced through a sibling staging file and installed by a
// rename, so a failed or interrupted download never leaves a partial file
// under the name the user asked for. Reads open regular files only, through
// go-atlas-core io/files, so a FIFO cannot block them.
package localfs
