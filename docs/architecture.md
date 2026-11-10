# Architecture

Back to the [README](../README.md).

The transport is [rmcloud-go](https://github.com/altessa-s/rmcloud-go): tokens, blobs, indexes, the root, notifications. Everything above it — what a folder or a document is, paths, the trash, how an upload or a move becomes a new root — lives here, in layers that `architecture_test.go` and `depguard` hold apart.

The domain, the ports, the use cases and the command packages under `cmd/rmctl/commands` never import rmcloud-go. The adapters that stand on it may: `cloud` implements the cloud ports over its client, `secretfile` and `keychain` implement its `SecretStore`, `vault` adapts a `SecretStore` to the secrets port, and `config` reads its endpoints. So may the composition root, `internal/wiring` and `cmd/rmctl`, which build the client.

**Entities and ports**

- `internal/domain` — items, the tree, paths, `.metadata` and `.content`. No I/O.
- `internal/port` — the driven ports the use cases need and the input ports the command line calls.

**Use cases**

- `internal/app/library` — loads the tree and commits a change to the root, replaying it on a conflict. It tells a `port.LoadObserver` how each load that reads items goes; the command line implements it as the line of a slow load (`cliutil.Loading`), `internal/wiring` passes it on, and the library keeps no time itself.
- `internal/app/account`, `browse`, `fetch`, `write`, `watch`, `inspect`, `pages`, `archives`, `secrets`, `render`, `notes`, `profile`, `prefetch` — one input port each; `pages.Reader` is shared with `browse` and `notes`, `render` downloads a document through `fetch.Pack`, as `export` does, and its `Viewer` (`rmctl open`) draws into `port.Previews` and shows the file through `port.Opener`; `render` and `notes` lay a folder out as a directory tree and record what they wrote through `outdir`, which is no input port itself; `library` keeps the tree of every root it loads or commits in `port.ListingCache`, `profile` registers, revokes and reads the account of any profile through the `account` use case, and `prefetch` (`rmctl sync`) fills the profile's cache through `port.SyncCache`.

**Adapters** (`internal/adapter/…`)

- `cloud` — the cloud ports over the rmcloud-go client.
- `secretfile`, `keychain`, `vault` — token storage in a file or the macOS Keychain, and `vault`, which presents either to the secrets port.
- `localfs`, `blobcache`, `pdf` — the local file system, the cache on disk (one directory per profile, one budget each, the drawings of `open` among what it bounds), and a PDF's page count through pdfcpu.
- `opener` — `port.Opener`: `open` on macOS, `ShellExecute` on Windows, `xdg-open` elsewhere, never waiting for the viewer.
- `indexcache` — decorators of the root, document, blob and registry ports that keep the library's indexes in the profile's cache, serve every file read from the cache first, serve `--offline` and the TTL, keep the listing of the library (`port.ListingCache`) and read it back for shell completion (`port.Completion`, which, like `port.Cache`, has no rule of its own and is implemented here; `wiring.Completion` builds it alone, with no transport and nothing that writes), implement `port.SyncCache` with the record of the last complete sync, and report, clear and remove the caches of every profile.
- `rmpages`, `rmdocs` — `.rm` pages through rmlines-go and `.rmdoc` archives through rmdoc-go; nothing else imports those two.
- `renderer` — drawing through rmrender-go on macOS, Linux and Windows with cgo, and a stub that says rendering is not available everywhere else, and in rmctl-lite (the `norender` build tag), where it names the full build; nothing else imports rmrender-go.
- `config`, `ids`, `clock` — profile resolution from config.toml, the profile directories and the edits of config.toml, random v4 UUIDs, and time over go-atlas-core.

**Composition root and command line**

- `internal/wiring` — builds the adapters and hands them to the use cases.
- `cmd/rmctl` — Cobra commands over the input ports only. Each command describes its answer once, as a JSON view and a document of tables, records and trees; `cmd/rmctl/commands/output` renders it in the chosen format.

**Tests**

- `internal/porttest` — doubles of the ports.
- `internal/clouddouble` — an emulated cloud the real transport talks to.
