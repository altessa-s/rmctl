// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package blobcache is rmctl's cache on disk: one directory with a
// subdirectory per space. blobs holds downloaded files by their sha256 hash
// and implements port.BlobCache; indexes holds document and root indexes by
// the hash that addresses them, for the indexcache adapter; rendered holds
// the drawings "rmctl open" shows, a directory each named by its key, and
// implements port.Previews; state holds a few small named files, such as
// each profile's last known root. Blobs, indexes and drawings share one byte
// budget, each profile its own, and are evicted least recently used first,
// or never under a budget of no limit; the state stays.
// Only a hash, or a name among the state, ever becomes a file name;
// a blob is checked against its hash when read; every write is a staging
// file renamed into place, so concurrent processes read an entry whole or
// not at all; and no cache error ever fails a command.
package blobcache
