# Mirrors

Back to the [README](../README.md).

A mirror keeps a local directory and a cloud folder in step, both ways. A PDF or EPUB you put in the directory appears on the tablet; what you write on the tablet comes back as a PDF in `_annotated/` beside it. Renames, moves and deletions on either side follow the other way.

```sh
rmctl mirror add papers ~/Papers /Papers     # define it in config.toml
rmctl mirror run papers --dry-run            # see what the first pass would do
rmctl mirror run papers                      # do it
rmctl mirror service install                 # keep every mirror in step from now on
```

```text
~/Papers/
├── Книги/Книга.epub            ⇄ /Papers/Книги/Книга
├── Report.pdf                  ⇄ /Papers/Report
├── _annotated/                 ← drawings of what was annotated on the tablet
│   ├── Книги/Книга.pdf
│   └── Report.pdf
├── .rmctl-removed/             ← files of documents deleted on the tablet
├── .rmctl-mirror.json          ← the journal: what was in step last time
└── .rmctl-mirror.lock
```

## Defining mirrors

- **`mirror add <name> <dir> <cloud-folder> [--pull-new] [--no-delete]`** appends a `[[mirror]]` table to config.toml (see [configuration](configuration.md#mirrors)). The directory must exist; the cloud folder is created by the first pass when it is missing. `--profile`/`-P` (or `RMCTL_PROFILE`) given here is the profile the mirror always uses; without it the mirror uses the profile of the run. A mirror whose directory contains the new one or lies inside it, or one of the same profile whose cloud folder does, is refused: two mirrors over one file would each take the other's changes for their own. Adding a mirror is the consent to run it: `mirror run` of a defined mirror does not ask.
- **`mirror ls`** lists the mirrors with what each directory's journal says — the documents in step, when the last pass ended, what it left pending, its error — and the files added, changed or deleted locally since, by their size and time. It reads only this machine.
- **`mirror rm <name>`** takes the table out of config.toml, leaving every other line. No file is touched, here or in the cloud; the journal stays, so a mirror defined again on the directory picks up where this one left off.

## A pass

`mirror run <name>...` (or `--all`) makes one pass of each mirror and prints one line per action, then one per pass. `--dry-run` works the pass out and changes nothing, journal included. `--dir D --folder F` makes a pass of a mirror config.toml does not define; it asks first, or takes `--yes`.

A pass compares three things: the local files, the cloud folder, and the journal `.rmctl-mirror.json`, which records every document that was in step at the last pass — its local path, the SHA-256, size and time of the file, and the document's identifier, name, folder, record hash and pages — and its drawing. Documents are followed by identifier, so a rename on the tablet is a rename, not a deletion and a new document.

| Action | When | What happens |
|---|---|---|
| `upload` | a PDF or EPUB no document follows | it goes into the folder of its directory (created when missing), named after the file without its extension, as `put` names it |
| `adopt` | a new file whose name a document of its folder already has | the two are paired; nothing is transferred |
| `update` | a followed PDF changed locally | its PDF is replaced and its pages' annotations kept, as `put --content-only` does; a page count that changed is said in the detail |
| `conflict` | the same, when the document's pages changed on the tablet too | the same: the local PDF goes up, the annotations stay, and the document is drawn again |
| `replace` | a followed EPUB changed locally, whose document carries no annotations | the document is replaced whole, as `put --replace` does: same identifier, tags kept |
| `move-local` | a document renamed or moved on the tablet, inside the folder | its file and its drawing are renamed or moved after it |
| `move-cloud` | a followed file gone, and a file of the same SHA-256 that nothing follows | the file was renamed or moved locally: the document is renamed or moved after it, keeping its annotations |
| `trash` | a followed file deleted locally | its document goes to the trash — never deleted for good — and its drawing into `.rmctl-removed/` |
| `remove` | a document deleted, put in the trash or moved out of the folder on the tablet | its file moves into `.rmctl-removed/` at the same path (a number tells a name taken there apart), and its drawing with it |
| `draw` | a document that carries annotations whose pages changed since its last drawing | it is drawn as `render` draws it into `_annotated/` at the file's path with `.pdf`: `Книги/Книга.epub` becomes `_annotated/Книги/Книга.pdf` |
| `export` | the same in rmctl-lite, which draws nothing | its `.rmdoc` goes there instead: `_annotated/Книги/Книга.rmdoc`; the pass's line says so |
| `skip` | a file written less than 2 seconds ago, an EPUB changed locally whose document carries annotations, a file that cannot be read, a file deleted locally that is back by the time its document would go to the trash (an application saving it anew) | nothing, until a later pass |
| `refuse` | a deletion the guard stops | nothing; see below |
| `detach` | a deletion under `--no-delete` | the other side keeps its copy, and the mirror stops following it |

- **What the mirror takes.** PDFs and EPUBs. Anything under a name that starts with a dot is ignored, and so are `_annotated/` at the top, `.DS_Store`, `Thumbs.db`, `desktop.ini`, and the files an application keeps while it writes (`~$…`, `.part`, `.partial`, `.crdownload`, `.download`, `.tmp`, `.temp`, `.swp`, a name ending in `~`). Markdown and text files are not uploaded: `put --notebook` turns one into a notebook, but a notebook does not come back as the same file, so there is nothing to keep in step.
- **What is drawn.** A document mirrored from a file is drawn once it carries annotations (a page of ink or highlights) and again each time its pages change; opening, moving or renaming it changes nothing that is drawn. With `--pull-new` (in the table, `pull_new = true`), every document created on the tablet in the folder — a notebook, or a PDF from another device — is drawn into `_annotated/` too, at its folder's path, and its drawing goes into `.rmctl-removed/` when it goes. Without it they are left alone.
- **Conflicts.** The cloud and the directory can both change between passes. A PDF changed on both sides keeps both: the local PDF replaces the document's, and the annotations, which only the tablet makes, stay on their pages. Pages whose annotations would be lost because the new PDF has fewer pages are refused, and the update stays pending; `rmctl put --content-only --drop-annotations` drops them by hand. An EPUB has no such update. One changed locally replaces its document whole when the document carries no annotations, since there is nothing to lose; when it carries some, it is skipped and stays pending, the line saying why, and `rmctl put --replace` replaces it by hand.
- **Loops.** What the mirror changes, it records in the journal: the next pass finds it in step, and a notification of its own change is a pass with nothing to do. A long pass writes the journal every 10 seconds as it goes, so one cut short by a crash repeats nothing older; an upload of its last seconds is paired with its document by name.
- **Names.** A name is the same in either Unicode form (macOS often writes `é` as `e` and an accent, the tablet as one character) and, where the file system ignores case, in either case: a document renamed on the tablet to the same name in another form or case renames its file, and a file is paired with the document of its name in any form.
- **Never overwritten, never deleted.** A move never replaces a file that is there, not even one written a moment before it: the rename is one the system refuses to make over a file. Nothing is deleted outright on either side: documents go to the trash, files into `.rmctl-removed/`. The only files the mirror writes over or removes are its own drawings in `_annotated/`.
- **One process per directory.** A pass takes the lock `.rmctl-mirror.lock` (the system's advisory lock, released however the process ends); another rmctl on the same directory is refused, and a watch waits for it. Of two mirrors of config.toml over one directory, or one folder of a profile — a hand edit can define them, `mirror add` cannot — the one defined later is not run, and the run says why.
- **The journal is the truth.** A journal that does not decode stops the pass: move it aside to start the mirror over. A directory whose journal names another cloud folder than the one at the mirror's path now — the folder renamed, moved or trashed on the tablet — is refused, naming where the folder went, rather than taking every document it lacks for deleted.

### The mass-deletion guard

A pass refuses the deletions of one side — documents to the trash, files into `.rmctl-removed/` — when they are:

- more than 50 in one pass;
- more than half of the documents the mirror follows, two at least (one document deleted out of one or two goes through);
- any at all when that side is empty: a directory with no file in it, or a cloud folder with no document, is taken for a drive that is not mounted or a sync that went wrong.

Each refused deletion is a `refuse` line and stays pending. Run the pass again with `--force-delete` to carry them out, or put the files or documents back. A directory that is not there at all fails the pass before anything is compared. `--no-delete` (`no_delete = true`) carries no deletion either way: what is deleted on one side stays on the other, and the mirror stops following it.

## Keeping in step: `--watch`

`mirror run --all --watch` keeps the mirrors in step until Ctrl-C (exit 130):

- **A pass after every change**, once the changes stop for `--debounce` (2s), or after 30 seconds of changes that do not stop. A file still being written when a pass looked is taken by another pass once it has settled. The system's file watcher (inotify, kqueue or ReadDirectoryChangesW, through fsnotify) announces the local changes at once: it watches the directory and every directory below it, not each file, and the directories made later as they appear. The library's notification socket announces the tablet's changes, as `rmctl watch` follows it.
- **Polling when the watcher cannot watch.** Where the watcher cannot start or stops — network shares and some removable drives, too many directories for the system's limit (on macOS, which holds a descriptor open for each file, more files than half the process may open), events lost in an overflow — the directory is listed every `--poll` (3s) instead, names, sizes and times, nothing read; a full pass follows at once, since changes may have been missed, and the watcher is tried again every `--rescan`.
- **A full pass** at start, when the socket reconnects after a drop, when a look at the directory comes much later than due (the machine slept), and every `--rescan` (15m), since a notification can be missed.
- **Retries.** A pass that fails as a whole — the network down, the directory unmounted, another process holding it, a profile whose library cannot be opened yet — is tried again after 5 seconds, then 10, doubling up to 5 minutes. A step that fails waits for the next pass. One mirror's failure stops no other.
- **config.toml** is read again every 30 seconds with `--all`: mirrors added start, mirrors removed stop, mirrors changed start again. Installing the service once is enough.

## The service

`mirror service install` starts `rmctl mirror run --service` — every mirror of config.toml, watched, whatever its profile — with your session, for your user alone:

| System | Manager | Definition |
|---|---|---|
| macOS | launchd agent, at login (`RunAtLoad`), again after a failure (`KeepAlive` on an unsuccessful exit) | `~/Library/LaunchAgents/io.altessa.rmctl.mirror.plist` |
| Linux | systemd user unit, with the session, `Restart=on-failure` | `~/.config/systemd/user/rmctl-mirror.service` (`$XDG_CONFIG_HOME`) |
| Windows | Task Scheduler task at logon, hidden, restarted on failure; rmctl starts through `conhost.exe --headless`, so no console window opens | the task `rmctl-mirror`, defined from `rmctl-mirror.xml` beside the log |

- **Install again** after upgrading rmctl or changing where its configuration is: the definition is replaced. It runs the rmctl that installed it; a Homebrew install is run through its stable `opt` link, so an upgrade does not break it. It keeps the variables of the run that installed it that say where things are — `RMCTL_CONFIG_DIR`, `RMCTL_CACHE_DIR`, `RMCTL_CONFIG_FILE`, `RMCTL_PROFILE`, `RMCTL_SECRETS`, `RMCTL_KEYCHAIN_SERVICE`, `RMCTL_CACHE_TTL`, `RMCTL_CACHE_BUDGET` and the `RMCLOUD_*` hosts — except on Windows, where a task carries no environment: set them as user variables there.
- **Linux.** A systemd user unit runs while you are logged in. `loginctl enable-linger $USER` keeps it running after you log out and starts it at boot. Without a systemd user session (`systemctl --user` does not answer, as in many containers), install refuses; start `rmctl mirror run --all --watch` from your own startup instead.
- **`mirror service status`** says whether it is installed and running; **`mirror service uninstall`** stops it and removes the definition, touching no mirrored file.
- **The log** is `logs/mirror.log` in rmctl's configuration directory (`~/Library/Application Support/rmctl/logs` on macOS, `~/.config/rmctl/logs` on Linux, `%AppData%\rmctl\logs` on Windows): the lines `mirror run` prints, as plain TAB-separated fields, rotated at 5 MiB with three older files kept. **`mirror service logs [-n N] [-f]`** prints its last lines, and `-f` follows it. What the process writes before the log opens, and a crash, go to `mirror-service.out` beside it on macOS, to the journal on Linux (`journalctl --user -u rmctl-mirror`).
- **Homebrew.** The formula defines the same service: `brew services start rmctl` runs `rmctl mirror run --service` through launchd or systemd, as `mirror service install` does. Use one or the other.

## In JSON

`mirror run --json` writes one object per line of the table, as the actions happen: `time`, `mirror` (its name, or the directory of an ad-hoc mirror), `action`, `path` and `to` (relative to the directory, with slashes), `document`, `detail`, `error` and `dry_run`; a line with `"action": "pass"` ends each pass, with `pending`. `mirror ls --json` lists the mirrors with `tracked`, `last_pass`, `last_error`, `pending`, `local_changes` and `error`.
