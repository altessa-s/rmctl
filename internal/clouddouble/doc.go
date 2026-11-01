// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package clouddouble is an in-memory reMarkable cloud for tests: a loopback
// TLS server that the real rmcloud-go client talks to. It serves the token
// calls, a content-addressed blob store, a root with generations and its
// 412 conflict, check-files and the notification socket, and it builds a
// library from [Item] values the way the tablet lays one out.
//
// It is test support. Only test files may import it; depguard keeps it out
// of production code.
package clouddouble
