# Architecture

Back to the [README](../README.md).

The transport is [rmcloud-go](https://github.com/altessa-s/rmcloud-go): tokens, blobs, indexes, the root, notifications. Everything above it — what a folder or a document is, paths, the trash, how an upload or a move becomes a new root — lives here, in layers that `architecture_test.go` and `depguard` hold apart.

The domain, the ports, the use cases and the command packages under `cmd/rmctl/commands` never import rmcloud-go. The adapters that stand on it may: `cloud` implements the cloud ports over its client, `secretfile` and `keychain` implement its `SecretStore`, `vault` adapts a `SecretStore` to the secrets port, and `config` reads its endpoints. So may the composition root, `internal/wiring` and `cmd/rmctl`, which build the client.

**Entities and ports**

- `internal/domain` — items, the tree, paths, `.metadata` and `.content`. No I/O.
- `internal/port` — the driven ports the use cases need and the input ports the command line calls.

**Use cases**

- `internal/app/library` — loads the tree and commits a change to the root, replaying it on a conflict.
- `internal/app/account`, `browse`, `fetch`, `write`, `watch`, `inspect`, `pages`, `archives`, `secrets`, `render`, `profile` — one input port each; `pages.Reader` is shared with `browse`, `render` downloads a document through `fetch.Pack`, as `export` does, and `profile` registers, revokes and reads the account of any profile through the `account` use case.

**Adapters** (`internal/adapter/…`)

- `cloud` — the cloud ports over the rmcloud-go client.
- `secretfile`, `keychain`, `vault` — token storage in a file or the macOS Keychain, and `vault`, which presents either to the secrets port.
- `localfs`, `blobcache`, `pdf` — the local file system, the cache on disk (one directory per profile, one budget each), and a PDF's page count through pdfcpu.
- `indexcache` — decorators of the root, document, blob and registry ports that keep the library's indexes in the profile's cache, serve `--offline` and the TTL, and report, clear and remove the caches of every profile.
- `rmpages`, `rmdocs` — `.rm` pages through rmlines-go and `.rmdoc` archives through rmdoc-go; nothing else imports those two.
- `renderer` — drawing through rmrender-go on macOS and Linux with cgo, and a stub that says rendering is not available everywhere else; nothing else imports rmrender-go.
- `config`, `ids`, `clock` — profile resolution from config.toml, the profile directories and the edits of config.toml, random v4 UUIDs, and time over go-atlas-core.

**Composition root and command line**

- `internal/wiring` — builds the adapters and hands them to the use cases.
- `cmd/rmctl` — Cobra commands over the input ports only. Each command describes its answer once, as a JSON view and a document of tables, records and trees; `cmd/rmctl/commands/output` renders it in the chosen format.

**Tests**

- `internal/porttest` — doubles of the ports.
- `internal/clouddouble` — an emulated cloud the real transport talks to.
