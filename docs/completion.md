# Shell completion

Back to the [README](../README.md).

`rmctl completion bash|zsh|fish|powershell` writes the completion script of a shell. With it, Tab completes commands and flags, and every argument or flag that names an item of the library: a path one folder at a time (`/Bo` becomes `/Books/`, ready for the next name), or an identifier from its first characters, described by its path. Names with spaces or other characters the shell treats specially come out quoted as that shell needs.

## Installing

The Homebrew formulas install the scripts of bash, zsh and fish with rmctl; with any other install, write them once:

| Shell | Install |
|---|---|
| bash | `rmctl completion bash > ~/.local/share/bash-completion/completions/rmctl` (or `$(brew --prefix)/etc/bash_completion.d/rmctl`). It needs bash 4.1 or newer and the bash-completion package (`bash-completion@2` with Homebrew; the bash 3.2 macOS ships is too old). For one session: `source <(rmctl completion bash)`. |
| zsh | `rmctl completion zsh > "${fpath[1]}/_rmctl"`, then a new shell; `compinit` must run in `~/.zshrc` (`autoload -U compinit; compinit`). Homebrew installs `_rmctl` into its `site-functions`, which its zsh setup puts on `fpath`. For one session: `source <(rmctl completion zsh)`. |
| fish | `rmctl completion fish > ~/.config/fish/completions/rmctl.fish` |
| PowerShell | `rmctl completion powershell \| Out-String \| Invoke-Expression` in your profile (`notepad $PROFILE`); Scoop and winget install no completion script. |

`rmctl completion <shell> --help` prints the same for each shell. `--no-descriptions` leaves out the descriptions after each candidate (the kind of an item, the path of an identifier).

## What completes

- **Library paths and identifiers**: `ls`, `tree`, `stat`, `get`, `export`, `mget`, `sync`, `backup` (its folder), `du`, `dupes`, `text`, `highlights`, `render`, `open`, `mkdir`, `mv` (both arguments, the second among folders), `rename` (the item), `pin`, `unpin`, `tag add|rm`, `trash`, `restore`, `rm`, `unmark`, `put` and `mput` (the folder), `debug files`, `debug page`, and the flags `find --under`, `watch --only`, `watch --under`, `put --to`, `restore --to` and `backup restore --to`. A destination offers folders alone. A command that takes one item offers nothing once it has it.
- **The trash** is offered when the command shows it: with `--all` (`ls -a`, `tree -a`, `find -a`, `du -a`, `render -a`…); `restore` offers the items in the trash alone, and `rm`, which deletes for good, offers them with the others.
- **Local files**: the files `put` uploads (`.pdf`, `.epub`, `.rmdoc`, `.md`, `.markdown`, `.txt`), and after the first one, a word that starts with `/` is first tried as the library folder they go to, then as another file. `text`, `highlights`, `render`, `open` and `rmdoc` complete local `.rmdoc` archives once a word names no item of the library (`./`, `~/`…); `mput`, `backup`, `backup restore` and `mget -o` complete directories.
- **Values**: `--mode`, `--grain`, `--paper`, `--format` and `--unreadable-pages` of `render` and `open`, and `--cover` and `--device` of `put`.

## Where the names come from

Completion never asks the cloud. Every command that reads the library (`ls`, `find`, `get`, `sync` and the others, `--offline` too) leaves a listing of it in the profile's cache — each item's identifier, name, kind, parent and whether it is in the trash, a few dozen bytes an item — and every write leaves the listing of the root it committed, so a folder made or renamed completes at once. A Tab reads that one file: it reads config.toml and the environment for the profile (`--profile`, `RMCTL_PROFILE`) and the cache directory (`RMCTL_CACHE_DIR`), builds no transport, reads no token, never asks for credentials, and writes and evicts nothing. Before any command has read the library, after `debug cache clear`, under `--no-cache`, or for a profile with no cache, it completes nothing and says nothing.

So completion shows the library as this machine last saw it: a document added from the tablet since completes after the next command that reads the library. It is fast whatever the library's size: the listing of 5,000 items reads and builds in about 3 ms, and a whole Tab, process included, takes about 12 ms on an Apple M4 Pro (`rmctl version` alone takes 7 ms).
