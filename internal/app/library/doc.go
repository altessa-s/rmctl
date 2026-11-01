// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package library is the component the use cases share: it reads the root
// and every item's index and metadata into a domain.Tree, and it commits a
// change to the root, replaying it when the root moves in between.
//
// It is not an input port. It depends on the cloud ports only.
package library
