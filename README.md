# rmctl

`rmctl` works with a reMarkable cloud library from the shell: list, find, download, upload, move, trash and restore documents and
folders, read the typed text and highlights of pages, pack and unpack `.rmdoc` archives, follow live notifications, and look at what
the cloud holds when something is wrong.

It is built on **[rmcloud-go](https://github.com/altessa-s/rmcloud-go)** for the cloud, **[rmlines-go](https://github.com/altessa-s/rmlines-go)**
for the `.rm` page format and **[rmdoc-go](https://github.com/altessa-s/rmdoc-go)** for the `.rmdoc` container. Part of
**[Slate Drive](https://slatedrive.app)** ([repository](https://github.com/altessa-s/slatedrive)), the macOS app that shows a reMarkable library
in Finder.

```sh
rmctl register abcd1234        # one-time code from https://my.remarkable.com/device/desktop/connect
rmctl ls /Books
rmctl get /Books/Go.pdf -o go.pdf
rmctl put paper.pdf /Inbox --yes
rmctl mv /Inbox/paper "/Books/Paper" --yes
rmctl find 'go|rust' --tag reading --json
```

## Install

With [Homebrew](https://brew.sh) on macOS or Linux:

```sh
brew install altessa-s/tap/rmctl
```

Every release also attaches archives for macOS, Linux and Windows.

From source, with Go 1.27 or newer and a C23 compiler for the page and archive commands (see below):

```sh
go install github.com/altessa-s/rmctl/cmd/rmctl@latest
```

`make rmctl` builds `bin/rmctl` from a checkout, with its version.

The page commands read and write the `.rm` page format through [rmlines-go](https://github.com/altessa-s/rmlines-go), which compiles a
C23 library through cgo: they need clang 19+ or GCC 13+ (`CC=clang-19 go install …` when the default compiler is older). Built with
`CGO_ENABLED=0`, rmctl keeps every other command and answers the page commands with "built without cgo". The released binaries are built
with cgo on each platform's own runner.

## Pages

| What | Behavior |
|---|---|
| Typed text | The paragraphs of a notebook's pages, in the document's order, with their styles: headings, bold, bullets, numbered items, checkboxes. |
| Highlights | The marker over a PDF or EPUB, with the words the tablet copied out of the text layer, the page, and the color. |
| Page diagnostics | Per page: strokes by tool and color, paragraphs, highlights, images, and the blocks the reader stepped over. |
| Search by text | `find` also looks into the typed text and the highlighted passages of documents that have pages; the other filters run first. |
| Unmark | Erases every highlight of a document, or of the pages named, the way the tablet's eraser does: each mark becomes a tombstone, every other byte stays, and the edited page is read back before it is stored. One commit. |
| Notebook from text | A `.md`, `.markdown` or `.txt` file becomes a notebook of typed text the tablet opens as its own: `# heading` is a heading, deeper headings and wholly bold lines are bold, bullets and numbered items are list paragraphs, a rule is a dash line; tables and pictures are refused, naming their line. Pages are laid out for the device (rm2, pure, pro, move) and paginated by estimate, at most 2000. |

## Archives

The `.rmdoc` container goes through [rmdoc-go](https://github.com/altessa-s/rmdoc-go), cgo like the page format.

| What | Behavior |
|---|---|
| Export | `export` packs documents into one archive stored the way the tablet writes one: each document's files under their record names, in index order. A folder, or `/`, stands for every document below it that is not in the trash on its own; a document named twice is packed once. Every name is checked before anything is downloaded. The tablet imports one document per archive. |
| Import | `put file.rmdoc [folder]` adds every document of an archive with its pages, annotations and tags, in one commit; it mixes with PDFs and EPUBs in one `put`. A document whose identifier the library already holds takes a fresh identifier and its files are renamed to it, so the library's document is never touched: its row of the answer names the identifier it had under `renamed_from`. The `.metadata` is filed under the folder and taken out of the trash; nothing else in it changes. `--dry-run` only opens the archive and lists what it would import. |
| Offline | `rmdoc <file>` lists an archive's documents, kinds and files, names masked unless `--raw`. `text` and `highlights` read a local archive's pages without the cloud: an argument ending with `.rmdoc` that is a local file is read from it, every document in turn (in JSON, always an array). |
| Limits | An entry is read only up to 1 GiB, an archive up to 4 GiB; rmdoc-go's own bounds hold the entry count, the names and the expansion ratio. |

## Uploads

`put` validates before it uploads: a PDF or EPUB by its extension, at most 500 MiB, not empty, a PDF that opens and has pages, an EPUB
that is a ZIP without DRM. The cover is the page last read unless the first page is asked for. A dry run validates the file and resolves
the folder and uploads nothing. Replacing the document of the same name in the folder keeps its identifier, its name and its tags; its
pages are new, so its annotations do not survive, and rmctl says so before it replaces anything.

**`--current-page N`** (a PDF, counted from 1) is the page the document opens on: it is recorded as the `.metadata`'s `lastOpenedPage`
(N-1), and with `--content-only` on a document whose `.content` keeps its pages in `cPages`, as `cPages.lastOpened` too. A page beyond
the PDF is refused. EPUBs are paginated by the tablet, so they take no page number.

**`--cover first|last`** maps to the `.content`'s `coverPageNumber`: `first` is 0, the first page; `last` is -1, the page last read, the
tablet's default. The tablet knows no third state, so there is no `--cover none`.

**`--content-only`** replaces only the PDF of the PDF document of the same name in the folder (the one `--replace` would pick) and keeps
everything else: its identifier, name, folder, tags, pin, cover and reading settings, every page's annotations (`<id>/<page>.rm`, per-page
metadata, old-style highlights), and every `.content` and `.metadata` field rmctl does not have to change. It changes the PDF, the
`.content`'s page list (`pages` and `redirectionPageMap`, or `cPages.pages` and `cPages.original`), `pageCount`, `originalPageCount` and
`sizeInBytes`, the `.pagedata` (one template line per page), and the `.metadata`'s `lastModified` and `version`; cached thumbnails are
dropped for the tablet to draw again. Every kept page also takes a new page identifier, with its annotation files filed under it
unchanged, because the tablet keeps page previews by identifier and would otherwise go on showing the old PDF; the plan's `id_map` lists
old → new. Annotations hang on page identifiers, which map to the PDF's pages:

- **The same number of pages, or more:** every page keeps its place and its annotations (under its new identifier); each extra page of
  the new PDF gets a new page, appended. Pages added on the tablet with no PDF page under them stay where they are.
- **Fewer pages:** the pages whose PDF page is gone are dropped. If any of them carries annotations, the upload is refused (exit 1),
  naming each such page and its annotation files; `--drop-annotations` drops them deliberately. Without annotations they simply go.
- **No document of that name, or one that is not a PDF:** refused. With nothing to keep, `--content-only` is most likely a mistake; upload
  without it.

It takes one PDF, asks first ("Replace the PDF of /Books/Paper, keeping its annotations on N pages?"), and is one commit like any write.
`--dry-run` prints the plan — the old and new page counts, the pages kept, appended and dropped, and the annotated pages affected — and the
JSON answer carries it as `content_plan`. It excludes `--replace`, `--notebook`, `--tag` and `--cover`.

## Space and duplicates

- **`du [path] [--depth N] [--all]`** adds up what the documents below a folder (the top level by default) take — the bytes of every file
  their indexes list, which is what the cloud keeps — and reports the total, each folder down to `--depth` levels (1; 0 is the total
  alone) with its documents and folders, largest first, and the documents by kind. `--all` counts the trash.
- **`dupes [path] [--all]`** groups the documents whose original — the PDF or EPUB they were made from — is the same file by its hash,
  one row per document with its path and id, the groups that waste the most first, then the number of groups and the bytes the extra copies
  take. Annotations are not compared, and notebooks, which have no original, are never listed.

Both read only the library's indexes, so they answer from the cache, `--offline` too.

## Backups

`rmctl backup <dir> [path]` keeps a local copy of the documents below a path (the whole library by default) that the library itself can
take back:

- **One archive per document.** Each document is written as `<dir>/<id>.rmdoc`, packed as `export` packs one — pages, annotations, tags
  and all. Documents in the trash stay out unless `--all`. `--jobs N` packs N documents at a time.
- **A manifest.** `<dir>/manifest.json` records the format version, the profile, the time and the path backed up, and per document its
  id, name, path, kind, record hash, archive and size.
- **Incremental.** A document whose record hash the manifest already holds, with its archive there at the recorded size, is skipped
  without a download; a changed one is written again, and each archive appears only when whole. The manifest is written last.
- **Documents gone.** The archive of a document deleted from the library, or put in the trash (unless `--all`), is moved into
  `<dir>/removed/`, or deleted with `--prune`. The archive of a document still in the library but moved out of the path is always moved
  into `removed/`, never deleted, `--prune` or not. The answer counts the documents, and the archives written, skipped, moved and pruned,
  and `outside`: those moved for lying outside the path. The manifest no longer lists what is in `removed/`; an archive there still
  imports with `rmctl put <file.rmdoc>`.
- **One directory, one profile, one path.** A directory is bound to the profile and the path of its first backup. A directory holding
  another profile's backup is refused unless `--force`; one holding the backup of another path — a renamed folder included — is refused
  unless `--rescope`, which binds it to the new path and moves every archive outside it into `removed/`. A re-scope deletes nothing,
  whatever `--prune` says. The two overrides are separate, so neither lets the other's mistake through.

`rmctl backup restore <dir> [--to folder] [--id id]... [--dry-run]` puts a backup back, in one commit: every archive (or those `--id`
names, by id or unique prefix) is imported into the folder path its manifest records, below `--to` when given, and missing folders are
created. A document whose id the library still holds comes back as a copy under a new id, as `put` imports an archive; the library's own
document is not touched. The archives are read before anything is uploaded; it asks first unless `--yes`, and `--dry-run` shows the folders
and documents it would create. `restore` is the subcommand, so a backup directory that is itself named `restore` is written `./restore`.

## Running a program on changes

`rmctl watch --exec PROGRAM [-- ARG...]` runs a program whenever the library changes, while it still prints the events:

- **Batches.** Events within `--debounce` (2s) of the first one of a batch make one run. Runs happen one at a time, in order; while one
  runs, up to 16 batches wait, and past that a batch joins the last one waiting. A batch holds at most 1000 events, however it grows —
  within one debounce window or by joining the last one waiting: past that the oldest are dropped and the newest 1000 kept. The run is
  told how many went, in `RMCTL_DROPPED` and `"dropped"`, and `watch` notes it on standard error once per batch; a program that sees a
  non-zero count missed events and should rescan the library (`rmctl ls -l`, `rmctl find`) rather than trust the batch. A batch that
  lost events always runs, even when none of the events kept passes `--only` or `--under`, then with an empty event list.
- **No shell, no interpolation.** The program runs as given, with the arguments after `--`. `--shell` hands `--exec` to `sh -c` instead
  (the arguments after `--` become `$0`, `$1`, …). Names are never put into the command line: the events reach the program only through its
  environment — `RMCTL_EVENT`, `RMCTL_ID`, `RMCTL_NAME`, `RMCTL_PATH`, `RMCTL_KIND`, `RMCTL_THIS_DEVICE` and `RMCTL_TIME` of the batch's
  last event, `RMCTL_COUNT`, `RMCTL_DROPPED` and `RMCTL_PROFILE` — and as JSON on its standard input: `{"profile": ..., "dropped": N,
  "events": [{"time", "event", "id", "name", "path", "kind", "this_device"}, ...]}`. Each of these variables replaces one of the same name
  that rmctl's own environment holds (by name without case on Windows), so the program sees it once. Names and paths come from the library,
  unmasked; an item no longer in it has none. `RMCTL_COUNT` counts the events after `--only` and `--under`; `RMCTL_DROPPED` counts the
  library events dropped before them.
- **Filters.** `--only document|folder` and `--under /Folder` keep the events of documents or folders, or of items below a folder; an item
  no longer in the library passes only when neither is given. Paths are read once per batch, which the cache makes one request.
- **Failures.** A run may take `--exec-timeout` (5m) and is then stopped. Its standard output and error go to rmctl's standard error. A run
  that fails or times out is noted with its exit status, and `watch` goes on.
- **Ctrl-C** gives a run under way 5 seconds to finish, then stops it; batches still waiting are dropped. `watch` then exits 130.

## Editing in place

`rename`, `pin`, `unpin` and `tag add|rm` change a document or folder without uploading it again: they write only the `.metadata`
(`visibleName`, `pinned`, and `version` and `lastModified` marked edited) and, for tags, the `.content`'s `tags`; every other field,
known or not, stays as it is, and every other file keeps its hash. `rename` keeps the item in its folder (`mv` moves it); a name cannot hold
a slash. Tags are given with `--tag`, repeated, after the items, so a tag never reads as a path; folders take no tags, since the tablet tags
documents and their pages and a folder's `.content` holds none. Asking for what already is — a tag that is there, a star on a starred
item — is reported as `unchanged` and commits nothing. Each command is one commit, replayed on a conflict, and asks first unless `--yes`.

## Commands

| Group | Commands |
|---|---|
| Account | `register <code> [--replace]`, `logout [--revoke]`, `account`, `secrets status`, `secrets migrate --to file\|keychain [--replace] [--set-default]` |
| Reading | `ls [path] [-l] [--all]`, `tree [path] [--all]`, `stat <path\|id>`, `find [pattern] [--tag t]... [--starred] [--under path] [--text s] [--all]`, `du [path] [--depth N] [--all]`, `dupes [path] [--all]`, `get <path\|id> [-o file]`, `export <path\|id>... -o file.rmdoc`, `rmdoc <file.rmdoc> [--raw]`, `mget <path> -o dir [--incremental] [--delete] [--jobs N]`, `backup <dir> [path] [--all] [--prune] [--force] [--rescope] [--jobs N]`, `text <path\|id\|file.rmdoc> [--page N]... [--format text\|markdown\|json]`, `highlights <path\|id\|file.rmdoc> [--page N]... [--format …]`, `watch [--raw] [--exec program [--shell] [--debounce d] [--exec-timeout d] [--only document\|folder] [--under path] [-- arg...]]` |
| Writing | `mkdir <path> [-p]`, `put <file\|file.rmdoc>... [folder] [--to folder] [--name] [--tag]... [--cover first\|last] [--current-page N] [--replace \| --content-only [--drop-annotations]] [--dry-run] [--notebook --device rm2\|pure\|pro\|move]`, `unmark <path\|id> [--page N]...`, `mv <src> <dst>`, `rename <path\|id> <name>`, `pin <path\|id>...`, `unpin <path\|id>...`, `tag add\|rm <path\|id>... --tag t...`, `trash <path\|id>...`, `restore <path\|id>... [--to folder]`, `rm <path\|id>...`, `mput <dir> [folder] [--incremental] [--jobs N]`, `backup restore <dir> [--to folder] [--id id]... [--dry-run]` |
| Diagnostics | `debug root`, `debug blob <hash> [--name] [-o file]`, `debug files <path\|id> [--raw]`, `debug page <path\|id> [--page N]... [--raw]`, `debug hashes`, `debug checkfiles`, `debug sweep`, `debug cache status [--all]`, `debug cache clear [--all]` |
| Other | `version [--full] [--output text\|json]`, `completion bash\|zsh\|fish\|powershell` |
| Global flags | `--format table\|plain\|json`, `--json`, `--color auto\|always\|never`, `--offline`, `--no-cache`, `--yes/-y`, `--profile/-P`, `--verbose/-v`, `--version` |

Items are named by a path from the top (`/Books/Go.pdf`) or by an identifier or a unique prefix of one (`3f2a`). A name that two items
share in one folder is ambiguous: the error lists the candidates, and their identifiers resolve them.

## Behavior

- **Answers go to standard output, everything else to standard error**, so a pipe gets the answer and not the progress. See
  [Output](#output) for their formats.
- **Writing commands ask first.** On a terminal they ask for confirmation; elsewhere they need `--yes`. `rm` says how many items it
  removes, descendants of a folder included. Each change is one new root: when the library moved meanwhile, the change is replayed on the
  new root, up to five times.
- **Exit codes:** 0 success, 1 failure, 2 usage error or a writing command without `--yes`, 3 not registered, 4 the library kept changing
  under a write, 130 interrupted.
- **Ctrl-C** cancels the running command: a half-written file is removed and the command says it was interrupted. A second Ctrl-C quits
  at once.
- **Tokens are never printed.** The device token is a file with mode 0600 in the profile directory; `rmctl` refuses to read one that
  other users can read. `debug files` masks document names unless `--raw`.

## Output

Every answer comes in one of three formats:

| Format | For | Shape |
|---|---|---|
| `table` | a person; the default when standard output is a terminal | Headers over a `─` rule, aligned columns, sizes like `4.1 KiB`, folders as `Name/`, pinned items marked `*`, trees drawn with `├──` and `└──`, records as aligned `key  value` lines. Names are cut with `…` to fit the terminal; nothing else is. |
| `plain` | `grep`, `cut`, `awk`; the default anywhere else | One line per record, fields separated by one TAB, no header and no color. Raw values: sizes in bytes, times in RFC 3339 UTC, identifiers whole, `true`/`false`, lists joined by commas, `-` for none. A single record is `key<TAB>value` lines, keys as in the JSON; a tree indents two spaces per level; blocks are separated by a blank line. A TAB, line break or backslash inside a value is written `\t`, `\n` or `\\`. |
| `json` | scripts | The same answers as indented JSON with sorted keys. `watch` writes one object per event, `put` one array at the end. |

`--format` chooses, then `--json` (the same as `--format json`), then `RMCTL_FORMAT`, then the terminal test. `--json` together with
another `--format` is a usage error. For scripts, `--json` is the stable interface; plain is stable too, but carries less.

A table is colored on a terminal that shows color: headers bold, folders blue, kinds and identifiers dim, items in the trash dim, the pin
mark yellow. `--color never` or `NO_COLOR` (any value, see [no-color.org](https://no-color.org)) or `TERM=dumb` turns this off,
`--color always` forces it, also into a pipe. On Windows, color is on only when the console accepts ANSI sequences.

`text` and `highlights` print a document's own text, and keep their own `--format text|markdown|json`, which stands in for the global
flag on those two commands; `version --output text|json` does the same. Progress and notes stay on standard error in every format.

## Cache

Every profile keeps its own cache under `<user cache dir>/rmctl/<profile>/` (`RMCTL_CACHE_DIR` replaces `<user cache dir>/rmctl`):
directories of mode 0700, files of mode 0600. It holds the indexes of the library — the root index by its hash, each document's index by
its record hash — the `.metadata` and `.content` files, what writes upload, and the profile's last known root with the time the cloud
confirmed it. Profiles share nothing, not even files with the same hash: they are different accounts' documents. Each profile's cache is
bounded by 512 MiB of indexes and files together, least recently used first; the root pointer is never evicted.

Everything kept is addressed by its content, so nothing goes stale and nothing needs invalidating:

- **A read asks the cloud one question when the library has not changed:** the root's hash and generation. With the same root as last time,
  the tree is built from disk; with another, only the indexes and files whose hash the cache lacks are fetched.
- **A write** (`put`, `mkdir`, `mv`, `trash`, `restore`, `rm`, import, `unmark`) always reads the live root and commits with the usual
  conflict replay; the indexes and files it writes go into the cache.
- **A TTL** (`ttl` in config.toml, or `RMCTL_CACHE_TTL`, a Go duration; 0, the default, is off) lets a read trust a root the cloud
  confirmed less than that long ago, without asking. Writes ignore it.
- **`--offline`** makes no request at all. `ls`, `tree`, `stat`, `find`, `du`, `dupes`, `text`, `highlights`, `debug page`, `debug sweep`, `rmdoc`,
  `secrets status` and `debug cache` read the last known root and its indexes from the cache; anything missing is an error that names it.
  A root older than a day draws a warning. Every other command refuses to run offline.
- **`--no-cache`** neither reads nor writes the cache, for debugging.
- **`debug cache status`** reports the profile's cache: its directory, size and budget, entries by kind, and the last known root with
  when it was confirmed; `--all` lists every profile. **`debug cache clear`** empties it, or every profile's with `--all`, after a question
  that names them.
- **`register` and `logout`** clear the profile's cache, so whoever registers next under the profile never sees the last account's files.

Older versions kept one blob directory for every profile; it is removed on first use, since nothing says whose its files were.

## Configuration

| Setting | Default | Changed by |
|---|---|---|
| Profile | `default` | `--profile/-P`, `RMCTL_PROFILE` |
| Profile directory (holds `token.json`) | `<user config dir>/rmctl/<profile>` | `RMCTL_CONFIG_DIR` |
| Cache directory (one subdirectory per profile) | `<user cache dir>/rmctl` | `RMCTL_CACHE_DIR` |
| Cache TTL | `0` (off) | `config.toml` `ttl`, `RMCTL_CACHE_TTL` |
| Offline | off | `--offline`, `config.toml` `offline` |
| Hosts | production | `RMCLOUD_HOST`, `RMCLOUD_AUTH_HOST`, `RMCLOUD_SYNC_HOST` |
| Wire log | off | `RMCLOUD_WIRE_LOG=headers\|bodies` (standard error, credentials masked) |
| Token backend | `file` | `config.toml`, `RMCTL_SECRETS=file\|keychain` |
| Configuration file | `<user config dir>/rmctl/config.toml` | `RMCTL_CONFIG_FILE` |
| Log level | warnings | `--verbose/-v` |
| Output format | `table` on a terminal, `plain` elsewhere | `--format`, `--json`, `RMCTL_FORMAT=table\|plain\|json` |
| Color | on a color terminal | `--color`, `NO_COLOR`, `TERM=dumb` |

Profiles keep several accounts apart: each has its own token and its own cache.

### config.toml

`<user config dir>/rmctl/config.toml` (or `RMCTL_CONFIG_FILE`; with `RMCTL_CONFIG_DIR` it moves along with the profiles) chooses where
tokens are kept. It is read strictly: a key rmctl does not know is an error that names it. A missing file is the defaults.

```toml
# Every profile keeps its tokens in a file of mode 0600 ...
[secrets]
backend = "file"

# ... except this one, which keeps them in the macOS Keychain.
[profiles.work.secrets]
backend = "keychain"
```

`RMCTL_SECRETS=file|keychain` overrides the file. `rmctl secrets status` says which backend is in use, what chose it and which tokens
are there — never their values.

It also sets the [cache](#cache), for every profile and per profile:

```toml
[cache]
ttl = "5m"          # reads trust a root confirmed less than 5 minutes ago
offline = false

[profiles.travel.cache]
offline = true      # this profile reads from the cache unless --offline=false
```

`RMCTL_CACHE_TTL` overrides `ttl`; `--offline` and `--offline=false` override `offline`.

### The macOS Keychain

With `backend = "keychain"` the tokens are generic password items of the login Keychain: service `io.altessa.rmctl`,
account `<profile>/<token>`, readable after the first unlock on this Mac only and never synchronized to iCloud. It needs a
macOS build with cgo, which the released macOS binaries are; any other build says so instead of falling back.

`rmctl secrets migrate --to keychain --set-default --yes` moves a profile's tokens: each is written to the Keychain and read back before
anything is removed from the file, and a failure on the way leaves them where they were. `--set-default` appends the profile's table to
config.toml (only appends, so comments and other settings stay); without it, the lines to add are printed. A target that already holds
a token of the profile is refused unless `--replace`.

## Architecture

The transport is [rmcloud-go](https://github.com/altessa-s/rmcloud-go): tokens, blobs, indexes, the root, notifications. Everything
above it — what a folder or a document is, paths, the trash, how an upload or a move becomes a new root — lives here, in layers that
`architecture_test.go` and `depguard` hold apart.

The domain, the ports, the use cases and the command packages under `cmd/rmctl/commands` never import rmcloud-go. The adapters that
stand on it may: `cloud` implements the cloud ports over its client, `secretfile` and `keychain` implement its `SecretStore`, `vault`
adapts a `SecretStore` to the secrets port, and `config` reads its endpoints. So may the composition root, `internal/wiring` and
`cmd/rmctl`, which build the client.

**Entities and ports**

- `internal/domain` — items, the tree, paths, `.metadata` and `.content`. No I/O.
- `internal/port` — the driven ports the use cases need and the input ports the command line calls.

**Use cases**

- `internal/app/library` — loads the tree and commits a change to the root, replaying it on a conflict.
- `internal/app/account`, `browse`, `fetch`, `write`, `watch`, `inspect`, `pages`, `archives`, `secrets` — one input port each;
  `pages.Reader` is shared with `browse`.

**Adapters** (`internal/adapter/…`)

- `cloud` — the cloud ports over the rmcloud-go client.
- `secretfile`, `keychain`, `vault` — token storage in a file or the macOS Keychain, and `vault`, which presents either to the
  secrets port.
- `localfs`, `blobcache`, `pdf` — the local file system, the cache on disk (one directory per profile, one budget each), and a PDF's
  page count through pdfcpu.
- `indexcache` — decorators of the root, document, blob and registry ports that keep the library's indexes in the profile's cache, serve
  `--offline` and the TTL, and report and clear the cache.
- `rmpages`, `rmdocs` — `.rm` pages through rmlines-go and `.rmdoc` archives through rmdoc-go; nothing else imports those two.
- `config`, `ids`, `clock` — profile resolution from config.toml, random v4 UUIDs, and time over go-atlas-core.

**Composition root and command line**

- `internal/wiring` — builds the adapters and hands them to the use cases.
- `cmd/rmctl` — Cobra commands over the input ports only. Each command describes its answer once, as a JSON view and a document of
  tables, records and trees; `cmd/rmctl/commands/output` renders it in the chosen format.

**Tests**

- `internal/porttest` — doubles of the ports.
- `internal/clouddouble` — an emulated cloud the real transport talks to.

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md). `make ci` runs lint, vet, the coverage gates, the integration tests (`-tags integration`, the
whole tool against the emulated cloud), the security scanners and the duplicate-code check; `make bench` runs the benchmarks.

## See also

- **[rmcloud-go](https://github.com/altessa-s/rmcloud-go)** — the reMarkable cloud sync protocol for Go, the transport this tool stands on.
- **[rmlines-go](https://github.com/altessa-s/rmlines-go)** — the `.rm` v6 page format for Go.
- **[rmdoc-go](https://github.com/altessa-s/rmdoc-go)** — the `.rmdoc` container a document is exported and imported as, for Go.
- **[rmlines-lib](https://github.com/altessa-s/rmlines-lib)** — the `.rm` v6 page format as a C23 library.
- **[rmlines-swift](https://github.com/altessa-s/rmlines-swift)** — the same page format as a Swift package.
- **[rmdoc-swift](https://github.com/altessa-s/rmdoc-swift)** — the `.rmdoc` container as a Swift package.
- **[rmdoc-lib](https://github.com/altessa-s/rmdoc-lib)** — the `.rmdoc` container as a portable C23 library.
- **[rmmarks-lib](https://github.com/altessa-s/rmmarks-lib)**, **[rmmarks-go](https://github.com/altessa-s/rmmarks-go)** and
  **[rmmarks-swift](https://github.com/altessa-s/rmmarks-swift)** — geometry for marked text: where a highlight lies on a page and which words it covers.
- **[rmrender-swift](https://github.com/altessa-s/rmrender-swift)** — drawing reMarkable notebooks into PDF or images, from Swift.
- **[Slate Drive](https://slatedrive.app)** ([repository](https://github.com/altessa-s/slatedrive)) — the app these packages are part of.

## Acknowledgements

reMarkable does not publish its cloud protocol or file formats. This tool is built on openly available information and on the research of
third-party developers who documented them.

## License

MIT — see [LICENSE](LICENSE).

reMarkable is a trademark of reMarkable AS. This project is not affiliated with, endorsed by or sponsored by reMarkable AS.
