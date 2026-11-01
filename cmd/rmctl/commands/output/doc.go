// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

// Package output renders the answers of the rmctl commands. A command
// describes its answer once, as a [Result]: the view struct the json format
// writes, and a [Doc] of blocks — a [Table] of records, a key-value
// [Record], a [Tree] or an opaque [Body] — made of typed [Cell] values. A
// [Renderer] registered under a name turns a Result into text, so a new
// format is one [Register] call and no command changes. A command whose
// rows come one by one (watch, put) writes them through [Stream] instead.
//
// The package knows nothing of the library, the ports or the command line;
// cliutil chooses the renderer from the flags and the terminal.
//
// # Formats
//
// json writes the view struct as indented JSON with sorted keys, the stable
// interface for scripts.
//
// plain writes lines that scripts can cut, grep and awk:
//
//   - no header, no color, nothing aligned;
//   - a table row is one line, its fields separated by one TAB, in the
//     order of the columns, including the columns a table leaves out;
//   - a record is one "key<TAB>value" line per field, the key lowercase
//     snake_case and the JSON field name where there is one;
//   - a tree is one node per line, its fields separated by a TAB, indented
//     two spaces per level below the top;
//   - a body is written as it is, ending with a line break;
//   - blocks are separated by one blank line, and an empty block is left
//     out;
//   - text is verbatim, except that a TAB, a line break, a carriage return
//     and a backslash in it are written as \t, \n, \r and \\; sizes and
//     counts are integers (bytes for a size); times are RFC 3339 in UTC;
//     booleans are true or false; a list is joined by commas; and a missing
//     value is "-".
//
// table writes for a person: a header in capitals over a rule of ─ for
// each column, columns two spaces apart, sizes and counts right-aligned,
// sizes in binary units (4.1 KiB), times to the minute in UTC, yes or no,
// a folder's name ending in "/" and a pinned item's in " *", records as
// aligned "key  value" lines, and trees drawn with ├──, └── and │. Widths
// are terminal columns, so wide (CJK, emoji) and combining runes line up;
// when the terminal's width is known, the name or path column is cut with
// "…" to fit, and nothing else is. With [Style.Color] headers are bold,
// folders bold blue, kinds, identifiers and keys dim, rows of the trash
// dim, and the pin mark yellow.
package output
