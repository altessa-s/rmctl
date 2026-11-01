// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package pages works with the .rm pages of documents: it reads them in the
// document's order, erases their highlights, and makes notebooks of typed
// text from text and markdown files. It implements port.Pages.
//
// [Reader] is shared: the browse use case searches typed text through it.
package pages
