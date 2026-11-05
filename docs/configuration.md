# Cache and configuration

Back to the [README](../README.md).

## Cache

Every profile keeps its own cache under `<user cache dir>/rmctl/<profile>/` (`RMCTL_CACHE_DIR` replaces `<user cache dir>/rmctl`): directories of mode 0700, files of mode 0600. It holds the indexes of the library — the root index by its hash, each document's index by its record hash — the `.metadata` and `.content` files, what writes upload, and the profile's last known root with the time the cloud confirmed it; `sync` adds the page files and, with `--files`, the originals. Profiles share nothing, not even files with the same hash: they are different accounts' documents. Each profile's cache is bounded by its own budget of indexes and files together, evicted least recently used first; the root pointer, the record of the last sync and the removal count are never evicted. The budget is chosen per profile, the first that is set winning:

1. `RMCTL_CACHE_BUDGET`, which applies to every profile (as `RMCTL_CACHE_TTL` does);
2. `budget` under the profile's `[profiles.<name>.cache]` in config.toml;
3. `budget` under `[cache]`, the default of every profile;
4. 512 MiB.

A budget of `0` is no limit: that profile's cache never evicts, and `sync` never refuses for its size. It does not turn the cache off; `--no-cache` does.

Everything kept is addressed by its content, so nothing goes stale and nothing needs invalidating:

- **A read asks the cloud one question when the library has not changed:** the root's hash and generation. With the same root as last time, the tree is built from disk; with another, only the indexes and files whose hash the cache lacks are fetched.
- **A write** (`put`, `mkdir`, `mv`, `trash`, `restore`, `rm`, import, `unmark`) always reads the live root and commits with the usual conflict replay; the indexes and files it writes go into the cache.
- **A TTL** (`ttl` in config.toml, or `RMCTL_CACHE_TTL`, a Go duration; 0, the default, is off) lets a read trust a root the cloud confirmed less than that long ago, without asking. Writes ignore it.
- **`--offline`** makes no request at all. `ls`, `tree`, `stat`, `find`, `du`, `dupes`, `text`, `highlights`, `debug page`, `debug sweep`, `rmdoc`, `secrets status` and `debug cache` read the last known root and its indexes from the cache, and so do `get`, `export`, `mget`, `backup` and `render` of a cloud document, with the files `sync --files` left there; anything missing is an error that names it (exit 1). A root older than a day draws a warning. `profile ls`, `profile info` and `profile del` (without `--revoke`) run offline too, from what this machine keeps. Every other command, `sync` among them, refuses to run offline (exit 2).
- **Every file is read through the cache first**, online too: a file the cache holds is never downloaded again, since its hash names its bytes. A download is not kept, except by `sync`, so a large original read once does not push out what every read uses.
- **`--no-cache`** neither reads nor writes the cache, for debugging; `sync` refuses it.

### sync

`rmctl sync [path]` fills the cache ahead of time, so later reads download nothing and run under `--offline`. It reads the live root, then fetches what the cache lacks of: every index of the library and every `.metadata` and `.content` (every read builds the whole tree from them), and the `.pagedata` and `.rm` pages of the documents below the path (`/` by default), which `text`, `highlights`, `find --text` and `debug page` read. `--files` also fetches each document's original and every other file of its index, which `get`, `export`, `mget`, `backup` and `render` read. Documents in the trash stay out unless `--all`; `--jobs N` transfers N files at a time (4 by default). It changes nothing in the library.

- **Only what changed costs anything.** Everything is addressed by its hash, so a file the cache holds is never fetched again. A complete sync is recorded in the cache with the root it reached and its scope (the path, `--files`, `--all`); the next sync at the same root whose scope it covers (the same path, or any path after a sync of the whole library, with no more `--files` or `--all`) answers at once that the cache is current: one request, for the root, no walk and no download. A different root walks the library and fetches only the files whose hash is new. A sync that is interrupted, refused or failed records nothing, so the next one walks. `--force` walks anyway.
- **The budget is checked before any document file is downloaded.** `sync` first reads the library as every command does, the root, each document's index and every `.metadata` and `.content`, which on a cold cache are fetched and kept; then it adds up what it keeps: those indexes, every `.metadata` and `.content`, and the files of the scope, those already cached included, since they must all fit at once (an index the reading itself evicted is read again to be counted). When that is more than the profile's budget it downloads no page and no original and exits 1 with a message that names the bytes needed, the budget and the setting to raise — `RMCTL_CACHE_BUDGET` when it set the budget, else `budget` under `[profiles.<name>.cache]` (or under `[cache]` when that set it, which raises every profile's) — rather than evicting what it just fetched; the indexes and metadata read to measure the library may stay in the cache. Under a budget of `0` it never refuses. Whatever the cache holds of the scope counts as just used, so eviction takes other files first.
- **A sync is recorded only when all it kept stayed.** Before recording a complete sync, `sync` checks that every index and file it planned is still in the cache; if the budget evicted any of them meanwhile, it exits 1 with a message that says so and records nothing, so an `--offline` read never trusts a sync the cache no longer holds.
- **An eviction forgets the last complete sync.** Whenever the cache evicts or drops an index or a file, by any command, the record of the last complete sync goes with it, and the profile's removal count (a small file among the state, `removals`) changes, so the next `sync` walks the library and fetches back what went instead of answering that the cache is current. A `sync` that records itself while another rmctl process removes something it kept is not trusted either: the record names the removal count the `sync` read before checking what it kept, and a later `sync` answers at once only while the count is still that one. A missing count is a cache that never removed anything; a damaged one is trusted by no record until the next removal rewrites it.
- **A lowered budget applies at once.** Each command that opens a profile's cache evicts it down to its budget, least recently used first, when it takes more, so `debug cache status` never reports more than the budget. Under no limit nothing is evicted.
- **Progress:** on a terminal, a line on standard error shows a bar, the bytes and files done of their totals, the rate and the time left; anywhere else nothing is printed. Ctrl-C stops it; every file fetched so far stays in the cache, and the next sync fetches only the rest.
- **The answer** (table, plain or JSON) says whether the cache was already current, the path, the documents, the files fetched and those already cached, the bytes fetched and needed, what the cache takes now, its budget (`unlimited` for 0; `0` in JSON) and what set the budget (`budget_source`: `env`, `profile`, `config` or `default`). After an answer of `current`, the counts are those of the sync that covers it. `debug cache status` counts what sync kept.

```sh
rmctl sync /Books --files      # before a flight
rmctl --offline text /Books/Notes
rmctl --offline get /Books/Paper -o paper.pdf
```
- **`debug cache status`** reports the profile's cache: its directory, size, its budget and what set it (`budget_source`), entries by kind, and the last known root with when it was confirmed; `--all` lists every profile, each with its own budget. **`debug cache clear`** empties it, or every profile's with `--all`, after a question that names them.
- **`register` and `logout`** clear the profile's cache, so whoever registers next under the profile never sees the last account's files.

Older versions kept one blob directory for every profile; it is removed on first use, since nothing says whose its files were.

## Configuration

| Setting | Default | Changed by |
|---|---|---|
| Profile | `default` | `--profile/-P`, `RMCTL_PROFILE` |
| Profile directory (holds `token.json`) | `<user config dir>/rmctl/<profile>` | `RMCTL_CONFIG_DIR` |
| Cache directory (one subdirectory per profile) | `<user cache dir>/rmctl` | `RMCTL_CACHE_DIR` |
| Cache TTL | `0` (off) | `config.toml` `ttl`, `RMCTL_CACHE_TTL` |
| Cache budget (per profile; `0` is no limit) | `512MiB` | `RMCTL_CACHE_BUDGET`, `config.toml` `[profiles.<name>.cache]` `budget`, then `[cache]` `budget` |
| Offline | off | `--offline`, `config.toml` `offline` |
| Hosts | production | `RMCLOUD_HOST`, `RMCLOUD_AUTH_HOST`, `RMCLOUD_SYNC_HOST` |
| Wire log | off | `RMCLOUD_WIRE_LOG=headers\|bodies` (standard error, credentials masked) |
| Token backend | `file` | `config.toml`, `RMCTL_SECRETS=file\|keychain` |
| Configuration file | `<user config dir>/rmctl/config.toml` | `RMCTL_CONFIG_FILE` |
| Log level | warnings | `--verbose/-v` |
| Output format | `table` on a terminal, `plain` elsewhere | `--format`, `--json`, `RMCTL_FORMAT=table\|plain\|json` |
| Color | on a color terminal | `--color`, `NO_COLOR`, `TERM=dumb` |

Profiles keep several accounts apart: each has its own token and its own cache.

### Profiles

A profile is one account on this machine: its tokens (`token.json` in its directory, or the Keychain), its directory `<user config dir>/rmctl/<profile>`, its cache `<user cache dir>/rmctl/<profile>/`, and its tables in config.toml (`[profiles.<profile>.…]`). `--profile/-P` or `RMCTL_PROFILE` chooses the one a command uses, `default` when neither does. A name is 1-64 characters of `a-z`, `0-9`, `_` and `-`, starting with a letter or digit.

```sh
rmctl profile add work abcdefgh      # register a new profile, as rmctl -P work register abcdefgh does
rmctl profile ls                     # every profile: current, registered, where its tokens are, its cache and budget
rmctl profile info work              # one profile in detail, with what account shows
rmctl -P work ls /Books              # use it
rmctl profile del work               # remove it entirely, after a question
```

- **`profile ls`** lists every profile this machine keeps something of — a directory, a table in config.toml, a cache — and the one in use once it is registered, and marks the one the command runs as, with what its cache takes and its own budget (`cache_budget` and `cache_budget_source` in JSON). It reads only this machine.
- **`profile add <name> <code>`** registers a new profile with a one-time code. A profile that already holds a device token is refused; `rmctl -P <name> register --replace <code>` registers it again.
- **`profile info [name]`** shows one profile, the one in use unless named: its directory, where its tokens are and which are there (never their values), its cache with its size, its budget and what set it (`cache_budget_source`), and the config.toml settings that apply to it. For a registered profile the cloud is asked what `account` shows; under `--offline`, or when the cloud does not answer, a note says so and the rest is shown.
- **`profile del <name>`** removes the profile in this order: its tokens, from its file and from the Keychain; its directory; its cache; and its tables in config.toml. Only the tables' header and key lines leave config.toml: every other line and comment stays, the result is read back before it replaces the file, and settings written in another form (dotted keys under `[profiles]`, an inline table) are refused with a request to edit the file by hand. Files rmctl did not write stay where they are, with their directory. A step that fails stops the removal there; the profile is still listed until its tables go last, and running `profile del` again finishes it. Like `logout`, it tells the cloud nothing: the device stays on the account until it is removed there; `--revoke` removes it first, and a revoke that fails changes nothing. It asks first, naming the profile and saying when it is the one in use; `--yes` answers.

### config.toml

`<user config dir>/rmctl/config.toml` (or `RMCTL_CONFIG_FILE`; with `RMCTL_CONFIG_DIR` it moves along with the profiles) chooses where tokens are kept. It is read strictly: a key rmctl does not know is an error that names it. A missing file is the defaults.

```toml
# Every profile keeps its tokens in a file of mode 0600 ...
[secrets]
backend = "file"

# ... except this one, which keeps them in the macOS Keychain.
[profiles.work.secrets]
backend = "keychain"
```

`RMCTL_SECRETS=file|keychain` overrides the file. `rmctl secrets status` says which backend is in use, what chose it and which tokens are there — never their values.

It also sets the [cache](#cache), for every profile and per profile:

```toml
[cache]
ttl = "5m"          # reads trust a root confirmed less than 5 minutes ago
offline = false
budget = "2GiB"     # every profile's cache holds up to 2 GiB, unless its table says otherwise

[profiles.work.cache]
budget = "8GiB"     # this profile's cache holds up to 8 GiB

[profiles.archive.cache]
budget = "0"        # no limit: this profile's cache never evicts

[profiles.travel.cache]
offline = true      # this profile reads from the cache unless --offline=false
```

A profile's table overrides `[cache]` key by key: above, `travel` keeps the 2 GiB budget and the 5-minute TTL. `RMCTL_CACHE_TTL` overrides `ttl`, and `RMCTL_CACHE_BUDGET` overrides `budget`, both for every profile; `--offline` and `--offline=false` override `offline`. A budget is a string: a size such as `"512MiB"`, `"4GiB"` or `"1.5GB"` (binary `KiB`, `MiB`, `GiB`, `TiB`, decimal `KB`, `MB`, `GB`, `TB`, or bytes without a unit) of at least `1MiB`, or `"0"` (in any unit) for no limit. A size between 0 and 1 MiB is refused with an error that names the setting: a cache that small keeps nothing worth keeping. `--no-cache`, not a budget of 0, turns the cache off.

### The macOS Keychain

With `backend = "keychain"` the tokens are generic password items of the login Keychain: service `io.altessa.rmctl`, account `<profile>/<token>`, readable after the first unlock on this Mac only and never synchronized to iCloud. It needs a macOS build with cgo, which the released macOS binaries are; any other build says so instead of falling back.

`rmctl secrets migrate --to keychain --set-default --yes` moves a profile's tokens: each is written to the Keychain and read back before anything is removed from the file, and a failure on the way leaves them where they were. `--set-default` appends the profile's table to config.toml (only appends, so comments and other settings stay); without it, the lines to add are printed. A target that already holds a token of the profile is refused unless `--replace`.
