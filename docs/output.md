# Behavior and output

Back to the [README](../README.md).

## Behavior

- **Answers go to standard output, everything else to standard error**, so a pipe gets the answer and not the progress. See [Output](#output) for their formats.
- **Writing commands ask first.** On a terminal they ask for confirmation; elsewhere they need `--yes`. `rm` says how many items it removes, descendants of a folder included. Each change is one new root: when the library moved meanwhile, the change is replayed on the new root, up to five times.
- **Exit codes:** 0 success, 1 failure, 2 usage error or a writing command without `--yes`, 3 not registered, 4 the library kept changing under a write, 130 interrupted.
- **Ctrl-C** cancels the running command: a half-written file is removed and the command says it was interrupted. A second Ctrl-C quits at once. `sync` keeps every file it fetched before the Ctrl-C, and says how many.
- **Progress** is for a person: when standard input and standard error are a terminal, `get` and `put` rewrite a percent line, and `sync` a bar with the bytes and files done of their totals, the rate and the time left (`syncing [=========>          ]  45%  9.0 MiB/20.0 MiB  120/310 files  3.0 MiB/s  0:04 left`). Anywhere else, in a pipe or a log, they print no progress at all. On `TERM=dumb` the line is padded with spaces rather than cleared with an escape sequence.
- **Tokens are never printed.** The device token is a file with mode 0600 in the profile directory; `rmctl` refuses to read one that other users can read. `debug files` masks document names unless `--raw`.

## Output

Every answer comes in one of three formats:

| Format | For | Shape |
|---|---|---|
| `table` | a person; the default when standard output is a terminal | Headers over a `─` rule, aligned columns, sizes like `4.1 KiB`, folders as `Name/`, pinned items marked `*`, trees drawn with `├──` and `└──`, records as aligned `key  value` lines. Names are cut with `…` to fit the terminal; nothing else is. |
| `plain` | `grep`, `cut`, `awk`; the default anywhere else | One line per record, fields separated by one TAB, no header and no color. Raw values: sizes in bytes, times in RFC 3339 UTC, identifiers whole, `true`/`false`, lists joined by commas, `-` for none. A single record is `key<TAB>value` lines, keys as in the JSON; a tree indents two spaces per level; blocks are separated by a blank line. A TAB, line break or backslash inside a value is written `\t`, `\n` or `\\`. |
| `json` | scripts | The same answers as indented JSON with sorted keys. `watch` writes one object per event, `put` one array at the end. |

`--format` chooses, then `--json` (the same as `--format json`), then `RMCTL_FORMAT`, then the terminal test. `--json` together with another `--format` is a usage error. For scripts, `--json` is the stable interface; plain is stable too, but carries less.

A table is colored on a terminal that shows color: headers bold, folders blue, kinds and identifiers dim, items in the trash dim, the pin mark yellow. `--color never` or `NO_COLOR` (any value, see [no-color.org](https://no-color.org)) or `TERM=dumb` turns this off, `--color always` forces it, also into a pipe. On Windows, color is on only when the console accepts ANSI sequences.

`text` and `highlights` print a document's own text, and keep their own `--format`, `text|markdown|json` and `text|markdown|note|json`, which stands in for the global flag on those two commands, while the answer of `highlights <folder>`, a row per note written, takes `--json` or `RMCTL_FORMAT`; `render --format pdf|png|jpg|svg` names the file it draws, so its answer takes `--json` or `RMCTL_FORMAT`; `version --output text|json` keeps its own too. Progress and notes stay on standard error in every format.
