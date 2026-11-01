// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package indexcache decorates the cloud's driven ports with the profile's
// cache on disk, so a read costs one request when the library has not
// changed: the root pointer. [Cache] decorates port.RootStore and
// port.DocumentStore: the root index is kept by root hash and each
// document's index by record hash, both addressed by content and never
// stale; the root pointer is kept with the time the cloud confirmed it,
// which a TTL (Fresh mode) and --offline (Offline mode) read instead of
// asking. [Blobs] keeps what a write uploads; [OfflineBlobs] refuses every
// request of an offline run; [Registry] clears the profile's cache when its
// account changes hands; [Admin] implements port.Cache for rmctl debug
// cache. An entry keeps the bytes the cloud sent and reads them back
// through the current parser, checked against their hash, so a change of
// parser never serves what an older one made of them; a damaged entry is
// dropped and read again.
package indexcache
