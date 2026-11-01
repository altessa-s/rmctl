// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package cloud is the adapter over the rmcloud-go client. It
// implements the cloud ports (RootStore, DocumentStore, BlobStore, Registry,
// Notifier) by mapping the transport's entries onto domain records and files
// and its errors onto the ports' sentinels. It holds no library semantics:
// what a document is, and how a change is built, belong to the use cases.
package cloud
