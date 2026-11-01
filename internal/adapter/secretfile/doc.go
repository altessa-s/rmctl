// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package secretfile keeps the device and user tokens of one profile in a
// JSON file of mode 0600 inside a directory of mode 0700. A file that other
// users can read is refused rather than used. Writes go through a sibling
// temporary file and a rename. No value is ever logged or put in an error.
package secretfile
