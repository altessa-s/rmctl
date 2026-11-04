// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package port declares the contracts between the layers.
//
// The driven ports (RootStore, DocumentStore, IndexProber, BlobStore,
// Registry, Notifier, LocalFiles, PDFInspector, PageReader, PageEditor,
// PageWriter, ArchiveReader, ArchiveWriter, Renderer, IDs, Clock, BlobCache)
// are what the use cases ask of the outside world; adapters implement them.
// The input ports (Account, Browser, Fetcher, Writer, Watcher, Inspector,
// Pages, Archives, Rendering) are what the command line asks of the use
// cases. The package holds no implementation and imports nothing from this
// module but domain.
package port
