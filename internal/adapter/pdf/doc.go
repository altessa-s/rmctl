// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package pdf implements port.PDFInspector with pdfcpu: the page count an
// upload writes into .content, and a refusal for a PDF the tablet could not
// open, such as one locked with a password. It uses pdfcpu's stateless
// configuration, so reading a PDF never writes under the user's home.
package pdf
