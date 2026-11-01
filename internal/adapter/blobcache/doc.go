// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package blobcache is rmctl's cache on disk: one directory with a
// subdirectory per space. blobs holds downloaded files by their sha256 hash
// and implements port.BlobCache; indexes holds document and root indexes by
// the hash that addresses them, for the indexcache adapter; roots holds each
// profile's last known root. Blobs and indexes share one byte budget and are
// evicted least recently used first; roots are a few bytes each and stay.
// Only a hash, or a profile name among the roots, ever becomes a file name;
// a blob is checked against its hash when read; every write is a staging
// file renamed into place, so concurrent processes read an entry whole or
// not at all; and no cache error ever fails a command.
package blobcache
