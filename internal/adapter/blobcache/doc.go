// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package blobcache implements port.BlobCache: downloaded files kept by
// their sha256 hash in one flat directory, bounded in size by evicting the
// least recently used. Only 64 lowercase hex digits ever become a file
// name, a file is checked against its hash when read, and no cache error
// ever fails a command.
package blobcache
