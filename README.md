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

Requires Go 1.27 or newer, and a C23 compiler for the page and archive commands (see below):

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
| Import | `put file.rmdoc [folder]` adds every document of an archive with its pages, annotations and tags, in one commit; it mixes with PDFs and EPUBs in one `put`. A document whose identifier the library already holds takes a fresh identifier and its files are renamed to it, so the library's document is never touched: rmctl prints `imported <name> (new id <id>, was <old>)`. The `.metadata` is filed under the folder and taken out of the trash; nothing else in it changes. `--dry-run` only opens the archive and lists what it would import. |
| Offline | `rmdoc <file>` lists an archive's documents, kinds and files, names masked unless `--raw`. `text` and `highlights` read a local archive's pages without the cloud: an argument ending with `.rmdoc` that is a local file is read from it, every document in turn (in JSON, always an array). |
| Limits | An entry is read only up to 1 GiB, an archive up to 4 GiB; rmdoc-go's own bounds hold the entry count, the names and the expansion ratio. |

## Uploads

`put` validates before it uploads: a PDF or EPUB by its extension, at most 500 MiB, not empty, a PDF that opens and has pages, an EPUB
that is a ZIP without DRM. The cover is the page last read unless the first page is asked for. A dry run validates the file and resolves
the folder and uploads nothing. Replacing the document of the same name in the folder keeps its identifier, its name and its tags; its
pages are new, so its annotations do not survive, and rmctl says so before it replaces anything.

## Commands

| Group | Commands |
|---|---|
| Account | `register <code> [--replace]`, `logout [--revoke]`, `account`, `secrets status`, `secrets migrate --to file\|keychain [--replace] [--set-default]` |
| Reading | `ls [path] [-l] [--all]`, `tree [path] [--all]`, `stat <path\|id>`, `find [pattern] [--tag t]... [--starred] [--under path] [--text s] [--all]`, `get <path\|id> [-o file]`, `export <path\|id>... -o file.rmdoc`, `rmdoc <file.rmdoc> [--raw]`, `mget <path> -o dir [--incremental] [--delete] [--jobs N]`, `text <path\|id\|file.rmdoc> [--page N]... [--format text\|markdown\|json]`, `highlights <path\|id\|file.rmdoc> [--page N]... [--format …]`, `watch [--raw]` |
| Writing | `mkdir <path> [-p]`, `put <file\|file.rmdoc>... [folder] [--to folder] [--name] [--tag]... [--cover first\|last] [--replace] [--dry-run] [--notebook --device rm2\|pure\|pro\|move]`, `unmark <path\|id> [--page N]...`, `mv <src> <dst>`, `trash <path\|id>...`, `restore <path\|id>... [--to folder]`, `rm <path\|id>...`, `mput <dir> [folder] [--incremental] [--jobs N]` |
| Diagnostics | `debug root`, `debug blob <hash> [--name] [-o file]`, `debug files <path\|id> [--raw]`, `debug page <path\|id> [--page N]... [--raw]`, `debug hashes`, `debug checkfiles`, `debug sweep` |
| Other | `version [--full] [--output json]`, `completion bash\|zsh\|fish\|powershell` |
| Global flags | `--json`, `--yes/-y`, `--profile/-P`, `--verbose/-v`, `--version` |

Items are named by a path from the top (`/Books/Go.pdf`) or by an identifier or a unique prefix of one (`3f2a`). A name that two items
share in one folder is ambiguous: the error lists the candidates, and their identifiers resolve them.

## Behavior

- **Answers go to standard output, everything else to standard error**, so a pipe gets the answer and not the progress. Every reading
  command takes `--json` for a stable machine-readable shape.
- **Writing commands ask first.** On a terminal they ask for confirmation; elsewhere they need `--yes`. `rm` says how many items it
  removes, descendants of a folder included. Each change is one new root: when the library moved meanwhile, the change is replayed on the
  new root, up to five times.
- **Exit codes:** 0 success, 1 failure, 2 usage error or a writing command without `--yes`, 3 not registered, 4 the library kept changing
  under a write, 130 interrupted.
- **Ctrl-C** cancels the running command: a half-written file is removed and the command says it was interrupted. A second Ctrl-C quits
  at once.
- **Tokens are never printed.** The device token is a file with mode 0600 in the profile directory; `rmctl` refuses to read one that
  other users can read. `debug files` masks document names unless `--raw`.

## Configuration

| Setting | Default | Changed by |
|---|---|---|
| Profile | `default` | `--profile/-P`, `RMCTL_PROFILE` |
| Profile directory (holds `token.json`) | `<user config dir>/rmctl/<profile>` | `RMCTL_CONFIG_DIR` |
| Blob cache | `<user cache dir>/rmctl/blobs` | `RMCTL_CACHE_DIR` |
| Hosts | production | `RMCLOUD_HOST`, `RMCLOUD_AUTH_HOST`, `RMCLOUD_SYNC_HOST` |
| Wire log | off | `RMCLOUD_WIRE_LOG=headers\|bodies` (standard error, credentials masked) |
| Token backend | `file` | `config.toml`, `RMCTL_SECRETS=file\|keychain` |
| Configuration file | `<user config dir>/rmctl/config.toml` | `RMCTL_CONFIG_FILE` |
| Log level | warnings | `--verbose/-v` |

Profiles keep several accounts apart: each has its own token.

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
- `localfs`, `blobcache`, `pdf` — the local file system, downloaded files kept by hash, and a PDF's page count through pdfcpu.
- `rmpages`, `rmdocs` — `.rm` pages through rmlines-go and `.rmdoc` archives through rmdoc-go; nothing else imports those two.
- `config`, `ids`, `clock` — profile resolution from config.toml, random v4 UUIDs, and time over go-atlas-core.

**Composition root and command line**

- `internal/wiring` — builds the adapters and hands them to the use cases.
- `cmd/rmctl` — Cobra commands over the input ports only.

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
