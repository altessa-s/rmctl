# Cache and configuration

Back to the [README](../README.md).

## Cache

Every profile keeps its own cache under `<user cache dir>/rmctl/<profile>/` (`RMCTL_CACHE_DIR` replaces `<user cache dir>/rmctl`): directories of mode 0700, files of mode 0600. It holds the indexes of the library — the root index by its hash, each document's index by its record hash — the `.metadata` and `.content` files, what writes upload, and the profile's last known root with the time the cloud confirmed it. Profiles share nothing, not even files with the same hash: they are different accounts' documents. Each profile's cache is bounded by 512 MiB of indexes and files together, least recently used first; the root pointer is never evicted.

Everything kept is addressed by its content, so nothing goes stale and nothing needs invalidating:

- **A read asks the cloud one question when the library has not changed:** the root's hash and generation. With the same root as last time, the tree is built from disk; with another, only the indexes and files whose hash the cache lacks are fetched.
- **A write** (`put`, `mkdir`, `mv`, `trash`, `restore`, `rm`, import, `unmark`) always reads the live root and commits with the usual conflict replay; the indexes and files it writes go into the cache.
- **A TTL** (`ttl` in config.toml, or `RMCTL_CACHE_TTL`, a Go duration; 0, the default, is off) lets a read trust a root the cloud confirmed less than that long ago, without asking. Writes ignore it.
- **`--offline`** makes no request at all. `ls`, `tree`, `stat`, `find`, `du`, `dupes`, `text`, `highlights`, `debug page`, `debug sweep`, `rmdoc`, `secrets status` and `debug cache` read the last known root and its indexes from the cache; anything missing is an error that names it. A root older than a day draws a warning. `profile ls`, `profile info` and `profile del` (without `--revoke`) run offline too, from what this machine keeps. Every other command refuses to run offline.
- **`--no-cache`** neither reads nor writes the cache, for debugging.
- **`debug cache status`** reports the profile's cache: its directory, size and budget, entries by kind, and the last known root with when it was confirmed; `--all` lists every profile. **`debug cache clear`** empties it, or every profile's with `--all`, after a question that names them.
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

### Profiles

A profile is one account on this machine: its tokens (`token.json` in its directory, or the Keychain), its directory `<user config dir>/rmctl/<profile>`, its cache `<user cache dir>/rmctl/<profile>/`, and its tables in config.toml (`[profiles.<profile>.…]`). `--profile/-P` or `RMCTL_PROFILE` chooses the one a command uses, `default` when neither does. A name is 1-64 characters of `a-z`, `0-9`, `_` and `-`, starting with a letter or digit.

```sh
rmctl profile add work abcdefgh      # register a new profile, as rmctl -P work register abcdefgh does
rmctl profile ls                     # every profile: current, registered, where its tokens are, what its cache takes
rmctl profile info work              # one profile in detail, with what account shows
rmctl -P work ls /Books              # use it
rmctl profile del work               # remove it entirely, after a question
```

- **`profile ls`** lists every profile this machine keeps something of — a directory, a table in config.toml, a cache — and the one in use once it is registered, and marks the one the command runs as. It reads only this machine.
- **`profile add <name> <code>`** registers a new profile with a one-time code. A profile that already holds a device token is refused; `rmctl -P <name> register --replace <code>` registers it again.
- **`profile info [name]`** shows one profile, the one in use unless named: its directory, where its tokens are and which are there (never their values), its cache with its size and budget, and the config.toml settings that apply to it. For a registered profile the cloud is asked what `account` shows; under `--offline`, or when the cloud does not answer, a note says so and the rest is shown.
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

[profiles.travel.cache]
offline = true      # this profile reads from the cache unless --offline=false
```

`RMCTL_CACHE_TTL` overrides `ttl`; `--offline` and `--offline=false` override `offline`.

### The macOS Keychain

With `backend = "keychain"` the tokens are generic password items of the login Keychain: service `io.altessa.rmctl`, account `<profile>/<token>`, readable after the first unlock on this Mac only and never synchronized to iCloud. It needs a macOS build with cgo, which the released macOS binaries are; any other build says so instead of falling back.

`rmctl secrets migrate --to keychain --set-default --yes` moves a profile's tokens: each is written to the Keychain and read back before anything is removed from the file, and a failure on the way leaves them where they were. `--set-default` appends the profile's table to config.toml (only appends, so comments and other settings stay); without it, the lines to add are printed. A target that already holds a token of the profile is refused unless `--replace`.
