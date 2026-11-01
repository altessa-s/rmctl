// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package domain is the library model: items, the tree that links them,
// paths, and the .metadata and .content files that describe them.
//
// It performs no I/O and knows nothing of the transport. The cloud's own
// types are mapped onto [Record] and [File] by the cloud adapter, so this
// package and the use cases above it do not depend on rmcloud-go.
package domain
