# Automation

`rmctl` is built to run unattended: every answer can be JSON, every write takes `--yes`, and the exit status says what went wrong. This page collects scenarios that put it to work from scripts, schedulers and file watchers, each with a complete script.

The scripts were run against a test account, the writes with `--dry-run`, and pass `shellcheck`. They need `bash` and [jq](https://jqlang.org); save them in `~/bin` (or anywhere on the `PATH`) and make them executable with `chmod +x`. Where a scheduler runs them, its `PATH` must reach `rmctl` and `jq`: `/opt/homebrew/bin` for Homebrew on Apple silicon, `/usr/local/bin` or `/home/linuxbrew/.linuxbrew/bin` elsewhere.

## Contents

- [Ground rules for scripts](#ground-rules-for-scripts)
- [Nightly backup](#nightly-backup)
- [A hot folder](#a-hot-folder)
- [Highlights to Markdown](#highlights-to-markdown)
- [A daily journal page](#a-daily-journal-page)
- [Typed notes into an Obsidian vault](#typed-notes-into-an-obsidian-vault)
- [Reacting to changes](#reacting-to-changes)
- [Rendering a folder for a website](#rendering-a-folder-for-a-website)
- [Library reports](#library-reports)
- [A second account](#a-second-account)
- [CI and retries](#ci-and-retries)

## Ground rules for scripts

- **Parse JSON.** `--json` (or `RMCTL_FORMAT=json`) is the stable interface; `--format plain` is one TAB-separated line per record, for `cut` and `awk`. Answers go to standard output, progress and notes to standard error, so a pipe only ever sees the answer. `text` and `highlights` take their own `--format text|markdown|json`, and `render` answers in JSON with `--json`.
- **Name items by identifier.** A path breaks when a document is renamed or moved; the `id` from `find --json` does not. Every command that takes a path takes an id or a unique prefix of one.
- **Say `--yes`.** A writing command asks first on a terminal and, anywhere else, refuses with exit status 2 unless `--yes` is given. `put`, `backup restore` and `--content-only` also take `--dry-run`, which checks everything and changes nothing.
- **Pick the account.** `-P name` (or `RMCTL_PROFILE`) chooses the profile; without it, `default`.
- **Read from the cache.** `--offline` makes no request at all: `ls`, `tree`, `stat`, `find`, `du`, `dupes`, `text`, `highlights` and `rmdoc` answer from what the last online read left in the cache, and the commands that need the cloud, such as `get`, `put` and `backup`, refuse (exit 2).
- **Check the exit status.**

| Status | Meaning | What a script should do |
|---|---|---|
| 0 | success | go on |
| 1 | failure: not found, a refused file, the network | report it; retrying rarely helps |
| 2 | usage error, or a write without `--yes`, or a command that needs the cloud under `--offline` | fix the script |
| 3 | the profile is not registered | run `rmctl register <code>` (or `rmctl -P name register <code>`) |
| 4 | the library kept changing under a write, five replays in a row | wait and run the write again |
| 130 | interrupted | stop |

## Nightly backup

`backup` keeps one `.rmdoc` archive per document and a `manifest.json` in a directory, and on the next run downloads only the documents whose record hash changed. Run nightly, it costs one request when nothing changed.

`~/bin/rm-backup.sh`:

```bash
#!/usr/bin/env bash
# Nightly incremental backup of a reMarkable library.
set -euo pipefail

dest="${RM_BACKUP_DIR:-$HOME/Backups/remarkable}"
path="${RM_BACKUP_PATH:-/}"

mkdir -p "$dest"
status=0
answer=$(rmctl backup "$dest" "$path" --jobs 4 --json) || status=$?

case $status in
  0) jq -r --arg when "$(date '+%F %T')" \
       '"\($when) \(.documents) documents: \(.written) written, \(.skipped) unchanged, \(.moved) moved to removed/ (\(.outside) outside the path), \(.pruned) pruned"' \
       <<<"$answer" ;;
  3) echo "rmctl is not registered: run 'rmctl register <code>'" >&2 ;;
  *) echo "backup failed with exit status $status" >&2 ;;
esac
exit "$status"
```

```console
$ RM_BACKUP_PATH=/Clients rm-backup.sh
2026-11-04 20:51:44 5 documents: 5 written, 0 unchanged, 0 moved to removed/ (0 outside the path), 0 pruned
$ RM_BACKUP_PATH=/Clients rm-backup.sh
2026-11-04 20:51:45 5 documents: 0 written, 5 unchanged, 0 moved to removed/ (0 outside the path), 0 pruned
```

**What happens to deleted documents.** The archive of a document deleted from the library, or put in the trash, is moved into `<dir>/removed/`; with `--prune` it is deleted instead. The archive of a document that is still in the library but was moved out of the backed-up path always goes to `removed/`, `--prune` or not. Without `--prune` nothing is ever lost, and `removed/` only grows; a line like this one in the script keeps it to three months:

```bash
find "$dest/removed" -name '*.rmdoc' -mtime +90 -delete 2>/dev/null || true
```

`--all` backs up the documents in the trash too, so trashing a document no longer moves its archive away. A directory belongs to the profile and the path of its first backup: backing another profile into it is refused unless `--force`, another path unless `--rescope`. Give each profile and path its own directory.

### macOS: launchd

`~/Library/LaunchAgents/io.altessa.rmctl.backup.plist` runs the script at 02:30, and at the next wake when the Mac slept through it. launchd expands neither `~` nor `$HOME`, so the paths are written out; replace `/Users/you`.

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>io.altessa.rmctl.backup</string>
  <key>ProgramArguments</key>
  <array>
    <string>/Users/you/bin/rm-backup.sh</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>PATH</key>
    <string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin</string>
  </dict>
  <key>StartCalendarInterval</key>
  <dict>
    <key>Hour</key>
    <integer>2</integer>
    <key>Minute</key>
    <integer>30</integer>
  </dict>
  <key>StandardOutPath</key>
  <string>/Users/you/Library/Logs/rmctl-backup.log</string>
  <key>StandardErrorPath</key>
  <string>/Users/you/Library/Logs/rmctl-backup.log</string>
</dict>
</plist>
```

```sh
launchctl bootstrap "gui/$(id -u)" ~/Library/LaunchAgents/io.altessa.rmctl.backup.plist
launchctl kickstart "gui/$(id -u)/io.altessa.rmctl.backup"     # run it once now
tail ~/Library/Logs/rmctl-backup.log
```

A profile that keeps its tokens in the Keychain works from a launch agent: the login Keychain is readable once the Mac has been unlocked after boot.

### Linux: a systemd timer

`~/.config/systemd/user/rmctl-backup.service`:

```ini
[Unit]
Description=Back up the reMarkable library

[Service]
Type=oneshot
Environment=PATH=/usr/local/bin:/usr/bin:/bin
ExecStart=%h/bin/rm-backup.sh
```

`~/.config/systemd/user/rmctl-backup.timer`:

```ini
[Unit]
Description=Back up the reMarkable library nightly

[Timer]
OnCalendar=*-*-* 02:30:00
Persistent=true
RandomizedDelaySec=10m

[Install]
WantedBy=timers.target
```

```sh
systemctl --user daemon-reload
systemctl --user enable --now rmctl-backup.timer
systemctl --user start rmctl-backup.service      # run it once now
journalctl --user -u rmctl-backup.service
loginctl enable-linger "$USER"                   # keep the timer running while logged out
```

`Persistent=true` runs a backup missed while the machine was off at the next boot.

### Anywhere: cron

```cron
30 2 * * * PATH=/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin $HOME/bin/rm-backup.sh >>$HOME/rm-backup.log 2>&1
```

### Restoring

`backup restore` imports the archives back into the folders the manifest records, in one commit, and creates the missing folders. A document whose id the library still holds comes back as a copy under a new id, so a restore never overwrites anything. Always look first with `--dry-run`:

```console
$ rmctl backup rm-backup /Meetings
dir        rm-backup
documents  5
written    5
skipped    0
moved      0
pruned     0
outside    0

$ rmctl backup restore rm-backup --to /Restored --dry-run
ACTION         NAME                        KIND      ID                                    RENAMED_FROM                          FOLDER
─────────────  ──────────────────────────  ────────  ────────────────────────────────────  ────────────────────────────────────  ──────────────────
would create   Restored/                   folder    -                                     -                                     /
would create   Meetings/                   folder    -                                     -                                     /Restored
would restore  1-on-1 with Maria           pdf       8828249c-d2f5-49fb-982a-6011ed125967  27cb2f10-bf47-4479-b18e-844a0547bdd9  /Restored/Meetings
would restore  Board Prep — Q3 Review      pdf       ba2ccfde-6e39-450c-be5f-8dae9e681dcc  fbf666ae-631a-44f9-a9a0-be212a654e78  /Restored/Meetings
would restore  Retro                       notebook  9b9a6c33-8be1-46dc-ba55-b3cada39858d  019a539e-a027-4b1e-99b3-3a20bdc8b537  /Restored/Meetings
would restore  Weekly Team Sync            pdf       20e709cc-8c35-4115-a4b9-a72dd903f257  cb68bd81-bfd6-4837-ace1-7580bc31c68d  /Restored/Meetings
would restore  Weekly Team Sync — Summary  pdf       f7775e54-7221-487e-b332-8e25d5542040  e0063e7a-2aea-4130-9976-7cc60c8909d6  /Restored/Meetings

$ rmctl backup restore rm-backup --id 019a --dry-run
ACTION         NAME   KIND      ID                                    RENAMED_FROM                          FOLDER
─────────────  ─────  ────────  ────────────────────────────────────  ────────────────────────────────────  ─────────
would restore  Retro  notebook  468f29fe-82ec-4f92-af6f-3b007c796780  019a539e-a027-4b1e-99b3-3a20bdc8b537  /Meetings
```

Then the same command with `--yes` instead of `--dry-run`. To find the id of a lost document, read the manifest: `jq -r '.documents[] | [.id, .path] | @tsv' rm-backup/manifest.json`. An archive in `removed/` is no longer in the manifest; it imports on its own with `rmctl put rm-backup/removed/<id>.rmdoc /Restored --yes`. Without the cloud, `rmctl text`, `highlights` and `render` read an archive of the backup directly.

## A hot folder

Drop a PDF or an EPUB into a local folder and it appears in `/Inbox` on the tablet. The script uploads each file on its own, so one bad file does not hold up the others, moves what went up into `sent/` and what was refused into `failed/`, and tries a write again when the library was changing (exit 4). It waits for a file to stop growing, so a scan still being written is not sent half-done. It reacts to [fswatch](https://github.com/emcrisostomo/fswatch) on macOS (`brew install fswatch`) or `inotifywait` from inotify-tools on Linux, and checks every 30 seconds without either.

`~/bin/rm-hotfolder.sh`:

```bash
#!/usr/bin/env bash
# Upload every PDF and EPUB that lands in a local folder, then move it aside.
# Usage: rm-hotfolder.sh [dir] [--once]     DRY_RUN=1 checks without uploading.
set -uo pipefail

hot="${1:-$HOME/reMarkable Inbox}"
once="${2:-}"
folder="${RM_FOLDER:-/Inbox}"
sent="$hot/sent"
failed="$hot/failed"
mkdir -p "$sent" "$failed"

if [[ -n ${DRY_RUN:-} ]]; then mode=(--dry-run); else mode=(--yes); fi

# Wait until a file stops growing, so a half-copied scan is not sent.
settled() {
  local before after
  before=$(wc -c <"$1") || return 1
  sleep 2
  after=$(wc -c <"$1") || return 1
  [[ $before -eq $after && $after -gt 0 ]]
}

upload() {
  local f=$1 status attempt
  for attempt in 1 2 3; do
    rmctl put "$f" --to "$folder" "${mode[@]}" --format plain
    status=$?
    [[ $status -eq 4 ]] || break   # 4: the library kept changing; try again
    sleep $((attempt * 10))
  done
  [[ -n ${DRY_RUN:-} ]] && return "$status"
  local stamp
  stamp=$(date +%Y%m%d-%H%M%S)
  if [[ $status -eq 0 ]]; then
    mv -- "$f" "$sent/$stamp-${f##*/}"
  else
    echo "upload of ${f##*/} failed (exit $status)" >&2
    mv -- "$f" "$failed/$stamp-${f##*/}"
  fi
}

scan() {
  local f
  while IFS= read -r -d '' f; do
    settled "$f" && upload "$f"
  done < <(find "$hot" -maxdepth 1 -type f \( -iname '*.pdf' -o -iname '*.epub' \) -print0)
}

scan
[[ $once == --once ]] && exit 0

if command -v fswatch >/dev/null; then          # macOS: brew install fswatch
  fswatch -o "$hot" | while read -r _; do scan; done
elif command -v inotifywait >/dev/null; then    # Linux: inotify-tools
  inotifywait -m -q -e close_write -e moved_to "$hot" | while read -r _; do scan; done
else
  while sleep 30; do scan; done
fi
```

The target folder must exist (`rmctl mkdir /Inbox --yes`); `put` refuses a folder it cannot find. `DRY_RUN=1` checks every file and the folder and moves nothing, `--once` handles what is there and exits:

```console
$ DRY_RUN=1 rm-hotfolder.sh ~/Scans --once
would upload	sync	pdf	-	-	/Users/you/Scans/sync.pdf	/Inbox
```

Moving a file into `sent/` is itself an event for the watcher; the scan it starts finds nothing and does nothing. To keep the hot folder running, use a launch agent or a systemd user service as in [Reacting to changes](#reacting-to-changes), with `ProgramArguments` or `ExecStart` pointing at the script.

**Without a watcher.** `mput` uploads every PDF and EPUB below a directory, recreating its subfolders, and `--incremental` skips the documents the folder already holds by name, so a cron line keeps a whole tree in step:

```cron
*/15 * * * * PATH=/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin rmctl mput $HOME/Papers /Papers --incremental --yes >>$HOME/rm-mput.log 2>&1
```

`mput` never removes anything from the cloud, and a document replaced locally under the same name is skipped, not updated; `put --replace` or `put --content-only` updates one.

## Highlights to Markdown

`highlights --format markdown` writes a document's highlights as quotes under a heading per page, the words as the tablet copied them out of the PDF's text layer. The script finds every PDF and EPUB below a folder and writes one Markdown file per document with highlights, keeping the folder tree.

`~/bin/rm-highlights.sh`:

```bash
#!/usr/bin/env bash
# Write the highlights of every PDF and EPUB below a folder as Markdown files.
# Usage: rm-highlights.sh /Folder outdir
set -euo pipefail

folder="${1:?usage: rm-highlights.sh /Folder outdir}"
out="${2:?usage: rm-highlights.sh /Folder outdir}"
folder="${folder%/}"

rmctl find --under "${folder:-/}" --json |
  jq -r '.[] | select(.kind != "folder" and .kind != "notebook") | [.id, .path] | @tsv' |
  while IFS=$'\t' read -r id path; do
    if ! md=$(rmctl highlights "$id" --format markdown </dev/null 2>/dev/null); then
      echo "skipped $path" >&2
      continue
    fi
    [[ -n $md ]] || continue                     # no highlights: no file
    file="$out/${path#"$folder"/}.md"
    mkdir -p "$(dirname "$file")"
    printf '# %s\n\n%s\n' "${path##*/}" "$md" >"$file"
    echo "$file"
  done
```

```console
$ rm-highlights.sh /Clients ~/Notes/Highlights
/Users/you/Notes/Highlights/Harbor Logistics/Contract — Signed.md

$ cat "$HOME/Notes/Highlights/Harbor Logistics/Contract — Signed.md"
# Contract — Signed

## Page 1

> uses

> uses for its own, and no less than reasonable care
```

A document without highlights answers with nothing on standard output (and a note on standard error), so it gets no file. For one document, `--format json` carries the page, the color and the text of each highlight: `rmctl highlights <id> --format json | jq -r '.highlights[] | select(.color == "yellow") | .text'`. A highlight over a page without a text layer, such as a scan, has no words and is listed as `(no text)`.

## A daily journal page

`put --notebook` turns a Markdown or text file into a notebook of typed text that the tablet edits as its own: `# heading` becomes a heading, a wholly bold line a bold paragraph, `-` and `1.` list items, `---` a rule. The script writes today's page — the date, the first three lines of a to-do file and a place for notes — and puts it into `/Journal`, once a day.

`~/bin/rm-journal.sh`:

```bash
#!/usr/bin/env bash
# Put today's journal page on the tablet as a notebook of typed text.
# DRY_RUN=1 builds and checks the notebook without uploading it.
set -euo pipefail

folder="${RM_JOURNAL_FOLDER:-/Journal}"
device="${RM_DEVICE:-rm2}"            # rm2, pure, pro or move
todo="${RM_TODO_FILE:-$HOME/todo.txt}"
day=$(date +%F)
name="Journal $day"

if rmctl stat "$folder/$name" >/dev/null 2>&1; then
  echo "$folder/$name is already there"
  exit 0
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
md="$tmp/$name.md"

{
  printf '# %s\n\n' "$(date '+%A, %d %B %Y')"
  printf '**Focus**\n\n'
  if [[ -s $todo ]]; then
    head -n 3 "$todo" | sed 's/^/- /'
  else
    printf -- '- \n'
  fi
  printf '\n**Notes**\n\n---\n'
} >"$md"

if [[ -n ${DRY_RUN:-} ]]; then
  rmctl put "$md" --to "$folder" --notebook --device "$device" --dry-run --format plain
  exit
fi

rmctl stat "$folder" >/dev/null 2>&1 || rmctl mkdir "$folder" --yes >/dev/null
rmctl put "$md" --to "$folder" --notebook --device "$device" --yes --format plain
```

`RM_DEVICE` lays the page out for the tablet (`rm2`, `pure`, `pro` or `move`). The document takes the file's name, `Journal 2026-11-04`; a second run on the same day finds it with `stat` and does nothing. `DRY_RUN=1` builds the page and checks it — a table or a picture in the Markdown would be refused here, naming its line:

```console
$ DRY_RUN=1 RM_JOURNAL_FOLDER=/Personal RM_DEVICE=move rm-journal.sh
would make	Journal 2026-11-04	notebook	-	-	/var/folders/…/Journal 2026-11-04.md	/Personal
```

Every morning at 6, from cron (on macOS a launch agent with `StartCalendarInterval` `Hour` 6, as for the backup):

```cron
0 6 * * * PATH=/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin $HOME/bin/rm-journal.sh >>$HOME/rm-journal.log 2>&1
```

## Typed notes into an Obsidian vault

`text` prints the typed text of a notebook — what was typed with the keyboard, not handwriting, which is ink and is not recognized. `--format markdown` keeps headings, bold lines, bullets, numbered items and checkboxes. The script copies every notebook below a folder into a vault as a note with front matter, and skips the notebooks whose record hash has not changed since the last run, without downloading them.

`~/bin/rm-notes-to-vault.sh`:

```bash
#!/usr/bin/env bash
# Copy the typed text of the notebooks below a folder into Markdown notes,
# such as an Obsidian vault. Unchanged notebooks are not downloaded again.
# Usage: rm-notes-to-vault.sh /Meetings ~/Vault/Meetings
set -euo pipefail

folder="${1:?usage: rm-notes-to-vault.sh /Folder vaultdir}"
vault="${2:?usage: rm-notes-to-vault.sh /Folder vaultdir}"
folder="${folder%/}"
mkdir -p "$vault"

rmctl find --under "${folder:-/}" --json |
  jq -r '.[] | select(.kind == "notebook") | [.id, .hash, .modified, .path] | @tsv' |
  while IFS=$'\t' read -r id hash modified path; do
    note="$vault/${path#"$folder"/}.md"
    if [[ -f $note ]] && grep -qx "rm_hash: $hash" "$note"; then
      continue                                   # unchanged since last run
    fi
    md=$(rmctl text "$id" --format markdown </dev/null 2>/dev/null) || { echo "skipped $path" >&2; continue; }
    # A notebook of handwriting only has page headings and no text.
    grep -qvE '^(## Page [0-9]+)?$' <<<"$md" || continue
    mkdir -p "$(dirname "$note")"
    {
      printf -- '---\nrm_id: %s\nrm_path: "%s"\nrm_modified: %s\nrm_hash: %s\n---\n\n' \
        "$id" "${path//\"/\\\"}" "$modified" "$hash"
      printf '%s\n' "$md"
    } >"$note"
    echo "$note"
  done
```

```console
$ rm-notes-to-vault.sh /Meetings ~/Vault/Meetings
/Users/you/Vault/Meetings/Retro.md
$ rm-notes-to-vault.sh /Meetings ~/Vault/Meetings
$ cat ~/Vault/Meetings/Retro.md
---
rm_id: 019a539e-a027-4b1e-99b3-3a20bdc8b537
rm_path: "/Meetings/Retro"
rm_modified: 2026-11-04T13:29:27Z
rm_hash: c6fd51b2633652d0f68c8a0d5cf921ed5bae0537b6d6f1e263c405b4911f70ad
---

# Sprint 42 Retro

**What went well**

- Launch checklist closed two days early
- Pricing review with finance

**To improve**

1. Fewer meetings on Fridays
2. Write the release notes as we go

———

- [ ] Book the venue for the Q4 kickoff
```

The note is rewritten whenever the notebook changes, so edits belong on the tablet, not in the vault. A notebook with several pages has a `## Page N` heading per page. The JSON answer gives each paragraph with its style (`heading`, `bold`, `bullet`, `numbered`, `plain`), and a checkbox reads as a bullet whose text starts with `[ ] `, so the open action items of a notebook are:

```console
$ rmctl text 019a --format json | jq -r '.pages[].paragraphs[] | select(.text | startswith("[ ] ")) | .text[4:]'
Book the venue for the Q4 kickoff
```

 `find --text` searches the same typed text, and the highlighted words, across the library: `rmctl --offline find --text "release notes" --format plain` answers from the cache.

## Reacting to changes

`watch --exec` runs a program each time the library changes, while `watch` stays connected to the cloud's notifications. Events that arrive within `--debounce` (2s) make one run; runs happen one at a time, in order. The program gets the batch as JSON on its standard input, `{"profile": ..., "dropped": N, "events": [{"time", "event", "id", "name", "path", "kind", "this_device"}, ...]}`, and the last event in its environment: `RMCTL_EVENT` (the cloud's name for it, such as `DocAdded` or `DocDeleted`), `RMCTL_ID`, `RMCTL_NAME`, `RMCTL_PATH`, `RMCTL_KIND`, `RMCTL_THIS_DEVICE`, `RMCTL_TIME`, `RMCTL_COUNT`, `RMCTL_DROPPED` and `RMCTL_PROFILE`. Names never go into its command line, so a document called `$(rm -rf ~)` is just a name. An item no longer in the library has no path or kind.

This handler renders each document of the batch that is still in the library to a PDF beside the others, mirroring the library's folders, and shows a notification (`osascript` on macOS, `notify-send` on Linux).

`~/bin/rm-on-change.sh`:

```bash
#!/usr/bin/env bash
# Run by "rmctl watch --exec": render every document of the batch that is
# still in the library to a PDF, and say so.
set -uo pipefail

out="${RM_RENDER_DIR:-$HOME/reMarkable PDFs}"
batch=$(cat)                                     # the batch as JSON

if [[ ${RMCTL_DROPPED:-0} -gt 0 ]]; then
  echo "missed $RMCTL_DROPPED events: run a full render of the folder" >&2
fi

notify() {
  if command -v osascript >/dev/null; then
    osascript -e 'on run argv' -e 'display notification (item 1 of argv) with title "reMarkable"' -e 'end run' "$1"
  elif command -v notify-send >/dev/null; then
    notify-send reMarkable "$1"
  fi
}

jq -r '.events[] | select(.path != "" and .kind != "folder") | [.id, .path] | @tsv' <<<"$batch" |
  sort -u |
  while IFS=$'\t' read -r id path; do
    pdf="$out$path.pdf"
    mkdir -p "$(dirname "$pdf")"
    if rmctl -P "$RMCTL_PROFILE" render "$id" -o "$pdf" --json </dev/null >/dev/null; then
      notify "${path##*/} updated"
    else
      echo "could not render $path" >&2
    fi
  done
```

```sh
rmctl watch --exec ~/bin/rm-on-change.sh --only document --under /Meetings
```

`--only document` and `--under /Meetings` drop the other events before the program runs. A batch holds at most 1000 events; past that the oldest are dropped and `RMCTL_DROPPED` says how many, and a program that sees a non-zero count should rescan rather than trust the batch. A run may take `--exec-timeout` (5m). Its standard output and error go to `watch`'s standard error, and a failing run is noted and does not stop `watch`. On Ctrl-C, `watch` gives a run under way 5 seconds and exits 130.

The handler can be tried without waiting for the tablet, by handing it a batch:

```sh
echo '{"profile":"default","dropped":0,"events":[{"event":"DocAdded","id":"019a539e-a027-4b1e-99b3-3a20bdc8b537","path":"/Meetings/Retro","kind":"notebook"}]}' |
  RMCTL_PROFILE=default rm-on-change.sh
```

For one-liners, `--shell` hands `--exec` to `sh -c`:

```sh
rmctl watch --shell --exec 'jq -r ".events[] | [.time, .event, .path] | @tsv" >> ~/rm-changes.log'
```

### Keeping watch running

`watch` reconnects by itself when the socket drops. To start it at login and bring it back if it exits, on macOS, `~/Library/LaunchAgents/io.altessa.rmctl.watch.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>io.altessa.rmctl.watch</string>
  <key>ProgramArguments</key>
  <array>
    <string>/opt/homebrew/bin/rmctl</string>
    <string>watch</string>
    <string>--exec</string>
    <string>/Users/you/bin/rm-on-change.sh</string>
    <string>--only</string>
    <string>document</string>
    <string>--under</string>
    <string>/Meetings</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>PATH</key>
    <string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin</string>
  </dict>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>ThrottleInterval</key>
  <integer>60</integer>
  <key>StandardOutPath</key>
  <string>/Users/you/Library/Logs/rmctl-watch.log</string>
  <key>StandardErrorPath</key>
  <string>/Users/you/Library/Logs/rmctl-watch.log</string>
</dict>
</plist>
```

On Linux, `~/.config/systemd/user/rmctl-watch.service`, enabled with `systemctl --user enable --now rmctl-watch.service`:

```ini
[Unit]
Description=Render reMarkable documents as they change
After=network-online.target

[Service]
Environment=PATH=/usr/local/bin:/usr/bin:/bin
ExecStart=/usr/local/bin/rmctl watch --exec %h/bin/rm-on-change.sh --only document --under /Meetings
Restart=always
RestartSec=60

[Install]
WantedBy=default.target
```

## Rendering a folder for a website

`render` draws a document the way the tablet shows it: the whole document as a PDF, or one page as a PNG, JPEG or SVG. The script publishes a folder: per document a PDF, a small JPEG of the first page as a thumbnail, an SVG of every page without its template, and an `index.html` linking them. Each document is downloaded once with `export` and drawn from the local archive, and a document whose record hash has not changed since the last run is not drawn again.

When the PDFs are all a site needs, `render` does it alone: a folder draws every document below it into a directory with the same tree, and `--incremental` draws only the documents changed since the last run, by their modification time:

```console
$ rmctl render /Strategy -o ~/Sites/notes/pdf --incremental --jobs 8
```

The script below adds the thumbnails, the page SVGs and the index, and flat names a URL can carry.

`~/bin/rm-site.sh`:

```bash
#!/usr/bin/env bash
# Render every document below a folder to a PDF, a JPEG thumbnail of its
# first page and an SVG of every page, plus an index.html linking them.
# Each document is downloaded once, as a .rmdoc, and drawn from that.
# Usage: rm-site.sh /Folder outdir
set -euo pipefail

folder="${1:?usage: rm-site.sh /Folder outdir}"
out="${2:?usage: rm-site.sh /Folder outdir}"
mkdir -p "$out"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

rmctl find --under "$folder" --json |
  jq -r '.[] | select(.kind != "folder") | [.id, .hash, .name] | @tsv' >"$tmp/docs.tsv"

: >"$tmp/index.tsv"
while IFS=$'\t' read -r id hash name <&3; do
  slug=$(printf '%s' "$name" | tr '[:upper:]' '[:lower:]' | tr -cs 'a-z0-9' '-' | sed 's/^-//; s/-$//')
  base="${id:0:8}${slug:+-$slug}"
  printf '%s\t%s\n' "$base" "$name" >>"$tmp/index.tsv"
  if [[ -f $out/$base.hash && $(<"$out/$base.hash") == "$hash" ]]; then
    continue                                     # unchanged since last run
  fi
  rmctl export "$id" -o "$tmp/doc.rmdoc" --json >/dev/null
  pages=$(rmctl render "$tmp/doc.rmdoc" -o "$out/$base.pdf" --json | jq .pages)
  rmctl render "$tmp/doc.rmdoc" --page 1 -o "$out/$base.jpg" --scale 0.25 --quality 80 --json >/dev/null
  mkdir -p "$out/$base"
  for ((p = 1; p <= pages; p++)); do
    rmctl render "$tmp/doc.rmdoc" --page "$p" -o "$out/$base/page-$p.svg" --no-template --json >/dev/null
  done
  printf '%s' "$hash" >"$out/$base.hash"
  echo "rendered $name ($pages pages)"
done 3<"$tmp/docs.tsv"

jq -Rr 'split("\t") | "<li><a href=\"\(.[0] | @uri).pdf\"><img src=\"\(.[0] | @uri).jpg\" alt=\"\"><br>\(.[1] | @html)</a></li>"' \
  "$tmp/index.tsv" | {
  echo '<!doctype html><meta charset="utf-8"><title>Notes</title><ul>'
  cat
  echo '</ul>'
} >"$out/index.html"
```

```console
$ rm-site.sh /Strategy ~/Sites/notes
rendered 2027 OKRs — Draft (1 pages)
rendered Market Expansion Brainstorm (1 pages)
rendered Quarterly Review (4 pages)
$ rm-site.sh /Strategy ~/Sites/notes
$ ls -1 ~/Sites/notes
02b64e01-2027-okrs-draft
02b64e01-2027-okrs-draft.hash
02b64e01-2027-okrs-draft.jpg
02b64e01-2027-okrs-draft.pdf
6a6bb31a-quarterly-review
6a6bb31a-quarterly-review.hash
6a6bb31a-quarterly-review.jpg
6a6bb31a-quarterly-review.pdf
bf6820e4-market-expansion-brainstorm
bf6820e4-market-expansion-brainstorm.hash
bf6820e4-market-expansion-brainstorm.jpg
bf6820e4-market-expansion-brainstorm.pdf
index.html
$ ls ~/Sites/notes/6a6bb31a-quarterly-review
page-1.svg  page-2.svg  page-3.svg  page-4.svg
```

The second run draws nothing: every record hash matches its `.hash` file.

`--scale 0.25` makes the thumbnail a quarter of the tablet's 1404 pixels across; `--quality` is the JPEG's, 1 to 100. For a PDF made from a PDF, `--mode preserving` appends the ink to the original file, its bytes untouched, and `--mode recomposed` keeps its forms and annotations; `--paper letter` lays notebook pages out on US Letter instead of A4. Rendering is in the macOS and Linux builds; on Linux the release archive's `libpdfium.so` must stay beside `rmctl`.

## Library reports

`du` and `dupes` read only the library's indexes, and `find --json` lists every item with its kind, tags, star and modification time, so a report on the whole library takes one online read and no downloads. The script refreshes the cache once and then reads only with `--offline`, so every section sees the same library.

`~/bin/rm-report.sh`:

```bash
#!/usr/bin/env bash
# A weekly report on the library: space, duplicates, untagged and stale documents.
# One online read refreshes the cache; every other read is --offline, so the
# whole report sees the same library and asks the cloud nothing more.
set -euo pipefail

stale_days="${RM_STALE_DAYS:-90}"

rmctl ls / >/dev/null                            # refresh the cache once

echo "== Space"
rmctl --offline du --json |
  jq -r '"total \(.bytes / 1048576 * 10 | floor / 10) MiB in \(.documents) documents",
         (.subfolders[] | "  \(.path)\t\(.bytes / 1024 | floor) KiB\t\(.documents) documents")'

echo
echo "== Duplicates"
rmctl --offline dupes --json |
  jq -r '(.groups[] | "  \(.documents | map(.path) | join("  =  "))"),
         "  \(.wasted / 1024 | floor) KiB taken by extra copies"'

items=$(mktemp)
trap 'rm -f "$items"' EXIT
rmctl --offline find --json >"$items"

echo
echo "== Documents without tags"
jq -r '.[] | select(.kind != "folder" and (.tags | length) == 0) | "  \(.path)"' \
  "$items"

echo
echo "== Not modified for $stale_days days"
jq -r --argjson days "$stale_days" '
  .[] | select(.kind != "folder")
      | select((.modified | fromdateiso8601) < (now - $days * 86400))
      | "  \(.modified[0:10])  \(.path)"' "$items" | sort
```

```console
$ rm-report.sh
== Space
total 7.8 MiB in 41 documents
  /Inbox	2633 KiB	5 documents
  /Projects	413 KiB	9 documents
  /Reading	388 KiB	4 documents
  /Clients	234 KiB	5 documents
  /Meetings	160 KiB	5 documents
  /Finance	131 KiB	3 documents
  /01_Inbox	95 KiB	2 documents
  /Strategy	94 KiB	3 documents
  /Personal	68 KiB	2 documents

== Duplicates
  /Finance/Budget 2027 — Working Notes  =  /Inbox/Budget 2027 — Working Notes
  23 KiB taken by extra copies

== Documents without tags
  /01_Inbox/Ideas to Sort
  /01_Inbox/Quick Notes
  …

== Not modified for 90 days
```

`dupes` compares the PDF or EPUB a document was made from, not its annotations, and never lists notebooks. cron mails a job's output to the user when mail is set up, which makes a weekly report one line:

```cron
0 8 * * 1 PATH=/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin $HOME/bin/rm-report.sh
```

Other questions are one `jq` filter over `rmctl find --json`: the starred documents (`select(.pinned)`), the ten largest (`sort_by(-.size) | .[:10]`), what is in the trash (`rmctl find --all --json | jq '.[] | select(.trashed)'`).

## A second account

A profile is one account on this machine, with its own token and cache. Register a second one with a one-time code from [my.remarkable.com](https://my.remarkable.com/device/desktop/connect), then name it with `-P` on any command:

```sh
rmctl profile add work abcdefgh
rmctl profile ls
rmctl -P work ls /
```

`export` packs documents of one account, with their pages, ink and tags, into an `.rmdoc` archive, and `put` imports the archive into the other; a document whose id the target already holds gets a new one. The script copies a document or a whole folder.

`~/bin/rm-copy-account.sh`:

```bash
#!/usr/bin/env bash
# Copy documents from one account (profile) to another, ink and all.
# Usage: rm-copy-account.sh FROM_PROFILE /path... TO_PROFILE /Folder [--dry-run]
#   rm-copy-account.sh work "/Projects/Q4 Product Launch" personal /FromWork
set -euo pipefail

from="${1:?usage: rm-copy-account.sh FROM_PROFILE /path TO_PROFILE /Folder [--dry-run]}"
src="${2:?}"
to="${3:?}"
dest="${4:?}"
if [[ ${5:-} == --dry-run ]]; then mode=(--dry-run); else mode=(--yes); fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

rmctl -P "$from" export "$src" -o "$tmp/copy.rmdoc" --json |
  jq -r '"packed \(.documents) documents"'
rmctl -P "$to" put "$tmp/copy.rmdoc" --to "$dest" "${mode[@]}" --format plain
```

A dry run on the test account, copying within the one profile it has:

```console
$ rm-copy-account.sh default "/Projects/Q4 Product Launch" default /Inbox --dry-run
packed 5 documents
would import	Go-to-Market Plan	pdf	1b82baf2-fc65-4c69-aaf8-7538dd1bc43d	85b92954-fbc4-417b-a010-ea209769d9fb	/var/folders/…/copy.rmdoc	/Inbox
would import	Kickoff Notes	annotated-pdf	f8f658e1-13f3-4e86-98dc-9a69befa14cf	8ae19d25-9d5d-4c00-99c0-06c3fb589be3	/var/folders/…/copy.rmdoc	/Inbox
…
```

`put` places every document of the archive in the one folder it is given. To copy a tree with its subfolders, back it up from one profile and restore it into the other: `backup restore` recreates the folder paths the manifest records, whichever profile wrote it.

```sh
rmctl -P work backup ~/rm-work-projects /Projects
rmctl -P default backup restore ~/rm-work-projects --to /From-work --dry-run
```

The tokens of a profile can live in the macOS Keychain instead of a file: `rmctl -P work secrets migrate --to keychain --set-default --yes`.

## CI and retries

In a CI job there is no terminal and no state: the token comes from a secret, the cache from the job's own directory. `RMCTL_CONFIG_DIR` and `RMCTL_CACHE_DIR` point rmctl at them; the profile's `token.json` goes into `$RMCTL_CONFIG_DIR/<profile>/`, readable by its owner only, or rmctl refuses it. The secret is the contents of the `token.json` that `rmctl profile info` names (a profile kept in the Keychain moves to a file with `rmctl secrets migrate --to file --yes`). Treat it as a password: it gives the whole account.

`ci/rm-ci.sh`:

```bash
#!/usr/bin/env bash
# A CI job that reads the library with a token kept as a secret.
# RMCTL_TOKEN_JSON holds the contents of a registered profile's token.json.
set -euo pipefail

work="${RUNNER_TEMP:-$(mktemp -d)}"
export RMCTL_CONFIG_DIR="$work/rmctl-config"
export RMCTL_CACHE_DIR="$work/rmctl-cache"
export RMCTL_FORMAT=json
mkdir -p "$RMCTL_CONFIG_DIR/default"
(umask 077 && printf '%s' "${RMCTL_TOKEN_JSON:?set the RMCTL_TOKEN_JSON secret}" \
  >"$RMCTL_CONFIG_DIR/default/token.json")

rmctl ls / >/dev/null                            # one online read fills the cache

# Every read below comes from the cache: no request, the same library throughout.
rmctl --offline du --depth 0 | jq -r '"\(.documents) documents, \(.bytes) bytes"'
rmctl --offline find --tag publish | jq -r '.[] | select(.kind != "folder") | .path'
```

`--offline` before any online read fails, naming what is missing: `profile default has no root in the cache: run without --offline to read it from the cloud`. In GitHub Actions, the step is:

```yaml
- name: Library report
  env:
    RMCTL_TOKEN_JSON: ${{ secrets.RMCTL_TOKEN_JSON }}
  run: ci/rm-ci.sh
```

A write commits a new root of the library, and when the tablet or another client changed the library meanwhile, rmctl replays the change on the new root, up to five times. When it still could not land, the exit status is 4 and nothing was changed, so the same command can simply run again. This wrapper does that, waiting longer each time, and passes every other status through:

`~/bin/rm-retry.sh`:

```bash
#!/usr/bin/env bash
# Run an rmctl command, trying again while the library keeps changing (exit 4).
# Usage: rm-retry.sh put report.pdf /Reports --yes
set -uo pipefail

attempts="${RM_ATTEMPTS:-5}"
for ((i = 1; ; i++)); do
  rmctl "$@"
  status=$?
  case $status in
    4) ;;                                        # retried below
    3) echo "not registered: provide the profile's token" >&2; exit 3 ;;
    *) exit "$status" ;;                         # 0, 1, 2, 130: as they are
  esac
  if ((i >= attempts)); then
    echo "the library kept changing; gave up after $i attempts" >&2
    exit 4
  fi
  sleep $((i * i * 5))
done
```

```sh
rm-retry.sh put build/report.pdf --to /Reports --replace --yes
```

`--replace` replaces the document of the same name and keeps its id and tags, but not its annotations; `--content-only` replaces only the PDF and keeps the annotations on the pages that remain.
