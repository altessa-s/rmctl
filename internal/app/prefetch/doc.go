// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package prefetch fills the profile's cache ahead of time, so later reads,
// and --offline runs, need no download. It implements port.Syncer.
//
// A sync reads the live root, then keeps every index of the library and
// every .metadata and .content (every read builds the whole tree from
// them), and the page files of the documents below a path: their .pagedata
// and .rm, which the page commands read. With Files it keeps every other
// file of those documents too: their originals, for get, mget, export,
// backup and render. Everything is addressed by its hash, so whatever the
// cache holds is current and is never fetched again. It computes what it
// keeps before it downloads anything, and refuses a sync that would not fit
// the cache's budget rather than evict what it just fetched. A complete
// sync is recorded with its root and scope; the next one at the same root
// whose scope it covers answers at once.
package prefetch
