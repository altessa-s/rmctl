# rmctl

`rmctl` works with a reMarkable cloud library from the shell: list, find, download, upload, move, trash and restore documents and folders, read the typed text and highlights of pages, draw documents with their annotations as PDF, PNG, JPEG or SVG, pack and unpack `.rmdoc` archives, follow live notifications, and look at what the cloud holds when something is wrong.

It is built on **[rmcloud-go](https://github.com/altessa-s/rmcloud-go)** for the cloud, **[rmlines-go](https://github.com/altessa-s/rmlines-go)** for the `.rm` page format, **[rmdoc-go](https://github.com/altessa-s/rmdoc-go)** for the `.rmdoc` container and **[rmrender-go](https://github.com/altessa-s/rmrender-go)** for drawing.

Part of **[Slate Drive](https://slatedrive.app)** ([repository](https://github.com/altessa-s/slatedrive)), the macOS app that shows a reMarkable library in Finder.

```sh
rmctl register abcd1234        # one-time code from https://my.remarkable.com/device/desktop/connect
rmctl ls /Books
rmctl get /Books/Go.pdf -o go.pdf
rmctl put paper.pdf /Inbox --yes
rmctl mv /Inbox/paper "/Books/Paper" --yes
rmctl find 'go|rust' --tag reading --json
```

## Features

- **[Browse and search](docs/examples.md#browsing):** `ls`, `tree` and `stat`, and `find` by name pattern, tag, star, folder or the typed and highlighted text of pages.
- **[Download](docs/pages-and-archives.md#archives):** a document's PDF or EPUB with `get`, a folder tree with `mget`, documents whole with their ink as one `.rmdoc` with `export`.
- **[Upload](docs/uploads.md):** PDFs, EPUBs and `.rmdoc` archives with `put` and `mput`, checked before anything is sent; `--content-only` replaces a PDF and keeps its annotations.
- **[Notebooks from Markdown](docs/pages-and-archives.md#pages):** `put --notebook` turns a `.md` or `.txt` file into a notebook of typed text laid out for the tablet (`rm2`, `pure`, `pro`, `move`).
- **[Typed text and highlights](docs/pages-and-archives.md#pages):** `text` and `highlights` as plain text, Markdown or JSON, from the cloud or from a local `.rmdoc`.
- **[Rendering](docs/rendering.md):** `render` draws a document with its ink as a PDF, or a page as PNG, JPEG or SVG, and a folder's documents as PDFs mirroring its tree, incrementally and several at a time.
- **[Backups](docs/backups.md):** `backup` keeps an incremental local copy, one `.rmdoc` per document with a manifest, and `backup restore` puts it back.
- **[Watch and exec](docs/watch.md):** `watch` follows the library's notifications live, and `--exec` runs a program on each batch of changes.
- **[Edits in place](docs/library.md#editing-in-place):** `rename`, `pin`, `unpin` and `tag` change only the metadata, `mv` and `mkdir` organize, and `unmark` erases highlights.
- **Trash and restore:** `trash` and `restore` as the tablet does, `rm` for good.
- **[Space and duplicates](docs/library.md#space-and-duplicates):** `du` adds up folders and kinds, `dupes` finds documents made from the same file.
- **[Profiles and the Keychain](docs/configuration.md#profiles):** several accounts side by side, each with its own token and cache; tokens in a 0600 file or the macOS Keychain.
- **[Offline cache](docs/configuration.md#cache):** a read asks the cloud one question when nothing changed, and `--offline` answers from the cache without any request; `sync` fills the cache ahead of time (`--files` with the originals too), so the page commands, `get`, `export`, `mget`, `backup` and `render` run offline, within a budget set in config.toml.
- **[Output and exit codes](docs/output.md):** tables, TAB-separated lines or JSON; distinct exit codes; every write asks first or takes `--yes`, and is one commit.
- **Diagnostics:** `debug` shows the root, blobs, files, pages and the cache; `--verbose` and `RMCLOUD_WIRE_LOG` show what goes over the wire.
- **Platforms:** macOS, Linux and Windows, with Homebrew, Scoop and winget; rendering on macOS and Linux.
- **Automation:** scheduled backups, hot folders, exports to Markdown and more in [docs/automation.md](docs/automation.md).

## Install

With [Homebrew](https://brew.sh) on macOS or Linux:

```sh
brew install altessa-s/tap/rmctl
```

On Windows, with [Scoop](https://scoop.sh) or [winget](https://learn.microsoft.com/windows/package-manager/winget/):

```powershell
scoop bucket add altessa-s https://github.com/altessa-s/scoop-bucket
scoop install rmctl

winget install Altessa.rmctl
```

Every release also attaches archives: a `.zip` for macOS, its binary signed with the Developer ID of Altessa Solutions Inc and notarized by Apple; a `.tar.gz` for Linux; a `.zip` for Windows. `checksums.txt` lists their SHA-256.

From source, with Go 1.27 or newer and a C23 compiler for the page and archive commands (see [Pages and archives](docs/pages-and-archives.md#pages)):

```sh
go install github.com/altessa-s/rmctl/cmd/rmctl@latest
```

`make rmctl` builds `bin/rmctl` from a checkout, with its version.

Rendering is in the macOS and Linux builds; what it needs on each platform and to build from source is in [docs/rendering.md](docs/rendering.md).

## Quick start

The answers are from a test account, in the `table` format a terminal gets.

```console
$ rmctl ls -l "/Projects/Q4 Product Launch"
NAME                        KIND           MODIFIED              SIZE  ID
──────────────────────────  ─────────────  ────────────────  ────────  ────────────────────────────────────
Go-to-Market Plan           pdf            2026-10-07 13:36  48.8 KiB  85b92954-fbc4-417b-a010-ea209769d9fb
Kickoff Notes               annotated-pdf  2026-10-07 13:37  32.4 KiB  8ae19d25-9d5d-4c00-99c0-06c3fb589be3
Kickoff Notes — Transcript  pdf            2026-09-15 15:30  86.3 KiB  bb5939b7-b685-4479-893c-fd1fea45d585
Launch Checklist            pdf            2026-09-15 15:30  25.0 KiB  bdc06cc6-1723-483a-89d7-3a54f776b800
Pricing Strategy v3         pdf            2026-09-15 15:30  64.3 KiB  24fdd1f5-5c13-4b4d-8186-3f56a2b6684b

$ rmctl render "/Clients/Harbor Logistics/Contract — Signed"
path            Contract — Signed-rendered.pdf
pages           2
skipped blocks  0
refused pages   0
links dropped   0
```

```sh
rmctl find --text 'release notes'                           # search the typed text and highlights
rmctl text /Meetings/Retro --format markdown                # a notebook's typed text as Markdown
rmctl highlights ef0f6df3                                   # a PDF's marked passages, named by an id prefix
rmctl put retro.md /Meetings --notebook --device pro --yes  # a notebook from Markdown
rmctl export /Meetings -o meetings.rmdoc                    # documents with their ink, as one archive
rmctl backup ~/rm-backup                                    # an incremental local copy
rmctl du                                                    # what the library takes, by folder and kind
```

More examples: [docs/examples.md](docs/examples.md).

## Commands

| Group | Commands |
|---|---|
| Account | `register <code> [--replace]`, `logout [--revoke]`, `account`, `secrets status`, `secrets migrate --to file\|keychain [--replace] [--set-default]`, `profile ls`, `profile add <name> <code>`, `profile info [name]`, `profile del <name> [--revoke]` |
| Reading | `ls [path] [-l] [--all]`, `tree [path] [--all]`, `stat <path\|id>`, `find [pattern] [--tag t]... [--starred] [--under path] [--text s] [--all]`, `du [path] [--depth N] [--all]`, `dupes [path] [--all]`, `get <path\|id> [-o file]`, `export <path\|id>... -o file.rmdoc`, `rmdoc <file.rmdoc> [--raw]`, `mget <path> -o dir [--incremental] [--delete] [--jobs N]`, `backup <dir> [path] [--all] [--prune] [--force] [--rescope] [--jobs N]`, `sync [path] [--files] [--all] [--force] [--jobs N]`, `text <path\|id\|file.rmdoc> [--page N]... [--format text\|markdown\|json]`, `highlights <path\|id\|file.rmdoc> [--page N]... [--format …]`, `render <path\|id\|file.rmdoc> [-o file] [--mode fresh\|preserving\|recomposed] [--paper a4\|letter\|tablet] [--page N [--format png\|jpg\|svg] [--scale S] [--quality Q]] [--no-template] [--id id]`, `render <folder> [-o dir] [--mode …] [--paper …] [--no-template] [--all] [--incremental] [--jobs N]`, `watch [--raw] [--exec program [--shell] [--debounce d] [--exec-timeout d] [--only document\|folder] [--under path] [-- arg...]]` |
| Writing | `mkdir <path> [-p]`, `put <file\|file.rmdoc>... [folder] [--to folder] [--name] [--tag]... [--cover first\|last] [--current-page N] [--replace \| --content-only [--drop-annotations]] [--dry-run] [--notebook --device rm2\|pure\|pro\|move]`, `unmark <path\|id> [--page N]...`, `mv <src> <dst>`, `rename <path\|id> <name>`, `pin <path\|id>...`, `unpin <path\|id>...`, `tag add\|rm <path\|id>... --tag t...`, `trash <path\|id>...`, `restore <path\|id>... [--to folder]`, `rm <path\|id>...`, `mput <dir> [folder] [--incremental] [--jobs N]`, `backup restore <dir> [--to folder] [--id id]... [--dry-run]` |
| Diagnostics | `debug root`, `debug blob <hash> [--name] [-o file]`, `debug files <path\|id> [--raw]`, `debug page <path\|id> [--page N]... [--raw]`, `debug hashes`, `debug checkfiles`, `debug sweep`, `debug cache status [--all]`, `debug cache clear [--all]` |
| Other | `version [--full] [--output text\|json]`, `completion bash\|zsh\|fish\|powershell` |
| Global flags | `--format table\|plain\|json`, `--json`, `--color auto\|always\|never`, `--offline`, `--no-cache`, `--yes/-y`, `--profile/-P`, `--verbose/-v`, `--version` |

Items are named by a path from the top (`/Books/Go.pdf`) or by an identifier or a unique prefix of one (`3f2a`). A name that two items share in one folder is ambiguous: the error lists the candidates, and their identifiers resolve them.

## Behavior

- **Answers go to standard output, everything else to standard error**, so a pipe gets the answer and not the progress.
- **Writing commands ask first.** On a terminal they ask for confirmation; elsewhere they need `--yes`. Each change is one new root, replayed when the library moved meanwhile.
- **Exit codes:** 0 success, 1 failure, 2 usage error or a writing command without `--yes`, 3 not registered, 4 the library kept changing under a write, 130 interrupted.

Formats, colors, Ctrl-C and tokens: [docs/output.md](docs/output.md).

## Documentation

- [docs/examples.md](docs/examples.md) — real answers of browsing, finding, notebooks, rendering and scripting commands.
- [docs/pages-and-archives.md](docs/pages-and-archives.md) — typed text, highlights, page diagnostics, `unmark`, notebooks from text, and `.rmdoc` export and import.
- [docs/rendering.md](docs/rendering.md) — `render` modes, page images, templates, paper, and the platform and build requirements.
- [docs/uploads.md](docs/uploads.md) — how `put` validates, `--current-page`, `--cover` and `--content-only`.
- [docs/library.md](docs/library.md) — `du`, `dupes`, and `rename`, `pin`, `unpin` and `tag` editing in place.
- [docs/backups.md](docs/backups.md) — incremental backups, the manifest, and `backup restore`.
- [docs/watch.md](docs/watch.md) — `watch --exec`: batches, the environment, filters and failures.
- [docs/output.md](docs/output.md) — standard output and error, confirmations, exit codes, and the `table`, `plain` and `json` formats.
- [docs/configuration.md](docs/configuration.md) — the cache, `--offline`, settings, profiles, config.toml and the macOS Keychain.
- [docs/architecture.md](docs/architecture.md) — the layers, ports, use cases and adapters.
- [docs/automation.md](docs/automation.md) — complete scripts for backups, hot folders, Markdown exports, rendering and reports.
- [docs/applescript.md](docs/applescript.md) — AppleScript for macOS: Finder Quick Actions, Folder Actions, Mail rules, Preview, Notes and the clipboard.
- [docs/powershell.md](docs/powershell.md) — PowerShell for Windows: installing, uploads from Explorer, a hot folder, Task Scheduler backups, `watch --exec` handlers and reports.

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md). `make ci` runs lint, vet, the coverage gates, the integration tests (`-tags integration`, the whole tool against the emulated cloud), the security scanners and the duplicate-code check; `make bench` runs the benchmarks.

## Built on

- **[rmcloud-go](https://github.com/altessa-s/rmcloud-go)** — the reMarkable cloud sync protocol for Go: tokens, blobs, indexes, the root and notifications.
- **[rmlines-go](https://github.com/altessa-s/rmlines-go)** — the `.rm` v6 page format for Go: pages, strokes, typed text and highlights, read and written.
- **[rmdoc-go](https://github.com/altessa-s/rmdoc-go)** — the `.rmdoc` container for Go: the archive a document and its pages travel in.
- **[rmrender-go](https://github.com/altessa-s/rmrender-go)** — reMarkable pages and documents drawn to PDF, PNG, JPEG or SVG for Go, the renderer `render` uses.

## See also

- **[rmmarks-go](https://github.com/altessa-s/rmmarks-go)** — marked text for Go: which words a highlight covers, and how OCR from cropped strips returns to the page.

## Acknowledgements

reMarkable does not publish its cloud protocol or file formats. This tool is built on openly available information and on the research of third-party developers who documented them.

## License

MIT — see [LICENSE](LICENSE). The fonts embedded for rendering carry the SIL Open Font License, and the Linux archives' `libpdfium.so` its own licences; both are under `licenses/` in the release archives.

reMarkable is a trademark of reMarkable AS. This project is not affiliated with, endorsed by or sponsored by reMarkable AS.
