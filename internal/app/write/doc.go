// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package write changes the library: folders, uploads, moves, renames, the
// trash and removal. It implements port.Writer.
//
// Every change follows the tablet's own order: the files first, then the
// item's index, then one new root. An edit rewrites only the file it
// changes, so the item's other files keep their hashes. The root write is
// replayed on a root that moved in between; see the library package.
//
// Restore takes items out of the trash into a folder of the caller's
// choosing, the top level by default; the folder must exist outside the
// trash. Upload (mput) runs a bounded number of uploads at once and, when
// incremental, skips the documents a folder already holds by name.
package write
