// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package write implements rmctl mkdir, put, mv, trash, restore, rm and
// mput: the commands that change the live library.
//
// Every one of them asks before it changes anything: --yes answers for it,
// a person at a terminal is asked, and anywhere else the command is refused
// as a usage error. rm names how many items go, descendants included.
// restore --to names the folder the items come back into; mput
// --incremental skips the documents the folder already holds by name.
package write
