# PowerShell on Windows

Back to the [README](../README.md).

`rmctl` runs on Windows as one `rmctl.exe`, and everything it answers can be read from PowerShell: `--json` goes through `ConvertFrom-Json`, every write takes `--yes`, and `$LASTEXITCODE` says what went wrong. This page installs it, sets out what to know when calling it from PowerShell, and gives complete scripts for the jobs people automate: sending files to the tablet, a hot folder, highlights and typed notes as Markdown, nightly backups with Task Scheduler, reacting to changes, reports and a second account. The same jobs for `bash` are in [automation.md](automation.md).

The scripts are written for PowerShell 7 (`pwsh`); [Windows PowerShell 5.1](#windows-powershell-51) lists what differs in the `powershell.exe` that ships with Windows. The `rmctl` commands they run were tried against a test account, the writes with `--dry-run`.

## Contents

- [Install](#install)
- [What is different on Windows](#what-is-different-on-windows)
- [Ground rules for scripts](#ground-rules-for-scripts)
- [Shared helpers and retries](#shared-helpers-and-retries)
- [Sending files to the tablet](#sending-files-to-the-tablet)
- [A hot folder](#a-hot-folder)
- [Highlights to Markdown](#highlights-to-markdown)
- [Typed notes into an Obsidian vault](#typed-notes-into-an-obsidian-vault)
- [Nightly backup with Task Scheduler](#nightly-backup-with-task-scheduler)
- [Reacting to changes](#reacting-to-changes)
- [Library reports](#library-reports)
- [Profiles and a second account](#profiles-and-a-second-account)
- [Windows PowerShell 5.1](#windows-powershell-51)

## Install

The simplest way is a package manager, which also puts `rmctl` on the `Path` and updates it:

```powershell
scoop bucket add altessa-s https://github.com/altessa-s/scoop-bucket
scoop install rmctl          # later: scoop update rmctl

winget install Altessa.rmctl # later: winget upgrade Altessa.rmctl
```

By hand: every release attaches `rmctl_<version>_windows_amd64.zip`, which holds `rmctl.exe`, the DLLs it draws documents with (`pdfium.dll` and the Cairo and Pango libraries, see [rendering.md](rendering.md)), `LICENSE`, `README.md` and `licenses/`, and `checksums.txt` with the SHA-256 of every archive. `rmctl.exe` needs no runtime to install: Windows finds the DLLs in its own folder, so keep the zip's files together. It is built for x64; Windows on Arm runs it under emulation.

Download both files from the [releases page](https://github.com/altessa-s/rmctl/releases), or from PowerShell:

```powershell
$release = Invoke-RestMethod https://api.github.com/repos/altessa-s/rmctl/releases/latest
$zipAsset = $release.assets | Where-Object name -like 'rmctl_*_windows_amd64.zip'
$sumsAsset = $release.assets | Where-Object name -eq 'checksums.txt'
Invoke-WebRequest $zipAsset.browser_download_url -OutFile $zipAsset.name
Invoke-WebRequest $sumsAsset.browser_download_url -OutFile checksums.txt
```

Check the archive against `checksums.txt`, unpack it into `%LocalAppData%\Programs\rmctl`, and add that folder to the user's `Path`:

```powershell
$zip = Get-Item .\rmctl_*_windows_amd64.zip
$expected = ((Select-String -LiteralPath .\checksums.txt -SimpleMatch $zip.Name).Line -split '\s+')[0]
if ((Get-FileHash -LiteralPath $zip.FullName -Algorithm SHA256).Hash -ne $expected) { throw 'checksum mismatch' }

$dir = Join-Path $env:LOCALAPPDATA 'Programs\rmctl'
Unblock-File -LiteralPath $zip.FullName
Expand-Archive -LiteralPath $zip.FullName -DestinationPath $dir -Force

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (($userPath -split ';') -notcontains $dir) {
    [Environment]::SetEnvironmentVariable('Path', ($userPath.TrimEnd(';') + ";$dir"), 'User')
}
$env:Path += ";$dir"           # this window too; new windows read the user's Path
rmctl version
```

`-ne` compares the hashes without case, so the lowercase line of `checksums.txt` matches the uppercase hash of `Get-FileHash`. `SetEnvironmentVariable` writes the user's `Path` back with any `%VARIABLE%` in it expanded; to keep those, add the folder by hand instead, under *Settings → System → About → Advanced system settings → Environment Variables*. Windows opened afterwards see the new `Path`; a Windows Terminal that was already running may need to be restarted.

To update, run the same steps again. A running `rmctl watch` keeps `rmctl.exe` and its DLLs open, and Windows refuses to replace a running program: stop it first.

Then register the machine with a one-time code from [my.remarkable.com](https://my.remarkable.com/device/desktop/connect), and turn on tab completion:

```powershell
rmctl register abcd1234
rmctl ls /

# Completion: in this window, and in every new one through the profile.
rmctl completion powershell | Out-String | Invoke-Expression
Add-Content -LiteralPath $PROFILE -Value 'rmctl completion powershell | Out-String | Invoke-Expression'
```

`$PROFILE` may not exist yet: `New-Item -ItemType File -Force -Path $PROFILE` creates it.

## What is different on Windows

- **Rendering needs the DLLs beside `rmctl.exe`.** `rmctl render` draws as on macOS and Linux, with the DLLs the zip carries; Scoop and winget install them with it. A copy of `rmctl.exe` alone, without them, does not start. The first rendering writes the bundled fonts once to `%LocalAppData%\rmrender\fonts`.
- **Tokens are in a file.** The macOS Keychain backend is macOS-only, so `secrets migrate --to keychain` is not for Windows; the tokens stay in `token.json` in the profile's directory. On macOS and Linux rmctl refuses a token file that other users can read; Windows has no such mode bits, so there the file is exactly as private as the user's profile folder, `%AppData%` included. Treat it as a password: it gives the whole account.
- **Where things are.** rmctl keeps its files in the user config and cache directories, which on Windows are `%AppData%` (roaming) and `%LocalAppData%`:

| What | Where |
|---|---|
| A profile's tokens | `%AppData%\rmctl\<profile>\token.json` |
| Settings | `%AppData%\rmctl\config.toml` |
| A profile's cache | `%LocalAppData%\rmctl\<profile>\` |

`rmctl profile info` prints the real paths (`config_dir`, `token_location`, `cache_dir` and `config_file` in its JSON), and `RMCTL_CONFIG_DIR`, `RMCTL_CACHE_DIR` and `RMCTL_CONFIG_FILE` move them as on any system ([configuration.md](configuration.md)). A domain account with a roaming profile carries `%AppData%`, the tokens with it, to other machines; the cache stays local.

- **`watch --exec` has no `sh`.** `--exec` runs a program without a shell, which works the same everywhere: [Reacting to changes](#reacting-to-changes) runs `pwsh` with a script. `--shell` hands the command to `sh -c`, which on Windows works only when some `sh.exe` (Git for Windows has one) is on the `Path`. The `RMCTL_*` variables a run gets replace any of the same name without regard to case, as Windows compares names.
- **Color.** A table is colored only when the console accepts ANSI sequences, as Windows Terminal and current consoles do; `--color never` or `NO_COLOR` turns it off. Scripts get `plain` or `json`, never color.
- **Ctrl-C** stops a command as anywhere else, removing a half-written file; the exit status is 130.

## Ground rules for scripts

- **Answers on standard output, notes on standard error.** A pipeline or a variable gets only the answer: `$items = rmctl find --json`. Notes such as `Proposal Draft has no highlights` go to the console, or to a log with `2>>`.
- **Parse JSON.** `--json` is the stable interface: pipe it through `Out-String` and `ConvertFrom-Json`, and the fields are properties, `$item.path`, `$item.kind`. `--format plain` is one TAB-separated line per record, for `-split` on a TAB (see the example below). `text` and `highlights` take their own `--format text|markdown|json`.
- **Read the exit status.** A native program never throws in PowerShell: `rmctl` failing leaves `$LASTEXITCODE` set and the script running. Check it after every call that matters, before using its output. In PowerShell 7.3 and later, `$PSNativeCommandUseErrorActionPreference = $true` together with `$ErrorActionPreference = 'Stop'` makes a non-zero status throw instead; the scripts here leave it off, since status 4 calls for a retry, not an error.

| Status | Meaning | What a script should do |
|---|---|---|
| 0 | success | go on |
| 1 | failure: not found, a refused file, the network | report it; retrying rarely helps |
| 2 | usage error, or a write without `--yes`, or a command that needs the cloud under `--offline` | fix the script |
| 3 | the profile is not registered | run `rmctl register <code>` (or `rmctl -P name register <code>`) |
| 4 | the library kept changing under a write, five replays in a row; nothing was changed | wait and run the write again |
| 130 | interrupted | stop |

- **Say `--yes`.** A write asks first on a terminal, a script started in a console window included, and refuses with status 2 where there is none, such as a hidden scheduled task, unless `--yes` is given; scripts should always pass it. `put` and `backup restore` take `--dry-run`, which checks everything and changes nothing.
- **Decode UTF-8.** PowerShell reads a native program's output in `[Console]::OutputEncoding`, which on Windows is still the console's legacy code page. rmctl writes UTF-8, so a name like `Contract — Signed` or `Отчёт ИТ-поддержки` comes out mangled unless a script starts with `[Console]::OutputEncoding = [Text.UTF8Encoding]::new()`. Arguments are not affected: Windows hands them to rmctl as Unicode.
- **Name items by identifier.** A path breaks when a document is renamed or moved; the `id` from `find --json` does not. Every command that takes a path takes an id or a unique prefix of one.
- **Quote library paths, and use `-LiteralPath` for files.** `"/Clients/Harbor Logistics"` needs its quotes. PowerShell's `-Path` treats `[` and `]` as wildcards, and a document named `[Summary] - Brand Guidelines` is real; `-LiteralPath` takes a name as it is.
- **Pick the account.** `-P name` (or `$env:RMCTL_PROFILE`) chooses the profile; without it, `default`.

```powershell
[Console]::OutputEncoding = [Text.UTF8Encoding]::new()

# JSON: one object per item.
$items = rmctl find --under /Clients --json | Out-String | ConvertFrom-Json
if ($LASTEXITCODE -ne 0) { throw "find failed with exit status $LASTEXITCODE" }
$items | Where-Object kind -ne 'folder' | Format-Table path, kind, id

# Plain: the first three fields of find are the path, the kind and the id.
rmctl find --under /Clients --format plain | ForEach-Object {
    $path, $kind, $id = ($_ -split "`t")[0..2]
    "$id  $path"
}
```

## Shared helpers and retries

The scripts below dot-source one file of helpers, which sets the encoding and the error behavior and defines four functions. Save it as `$HOME\bin\rm-common.ps1`, beside the others. The examples call the scripts by name, as when `$HOME\bin` is on the user's `Path` (added as in [Install](#install)); a full path such as `& "$HOME\bin\rm-send.ps1"` works without.

`Invoke-RmctlRetry` runs a script block that calls `rmctl`, and runs it again while the status is 4 — the library kept changing under a write, so nothing was changed and the same command can simply run again — waiting 5, 20, 45 and 80 seconds. Any other status ends it, and `$LASTEXITCODE` is the last attempt's. `Invoke-RmctlJson` does the same for a command that answers in JSON, and returns the parsed answer or throws.

`$HOME\bin\rm-common.ps1`:

```powershell
# Helpers shared by the rmctl scripts. Dot-source it:  . "$PSScriptRoot\rm-common.ps1"

$ErrorActionPreference = 'Stop'                     # a failing cmdlet stops the script
$PSNativeCommandUseErrorActionPreference = $false   # rmctl's status is read by hand (7.3+)

# rmctl writes UTF-8; read it as UTF-8 so names with accents, dashes or
# Cyrillic letters survive.
[Console]::OutputEncoding = [Text.UTF8Encoding]::new()

function Invoke-RmctlRetry {
    # Runs a script block that calls rmctl, again while rmctl exits 4 (the
    # library kept changing under a write; nothing was changed). Passes the
    # block's output through; $LASTEXITCODE is the last attempt's status.
    param(
        [Parameter(Mandatory)] [scriptblock] $Command,
        [int] $Attempts = 5
    )
    for ($retryAttempt = 1; ; $retryAttempt++) {
        $retryOutput = & $Command
        $retryStatus = $LASTEXITCODE
        if ($retryStatus -ne 4 -or $retryAttempt -ge $Attempts) { break }
        Write-Warning "the library kept changing; trying again ($retryAttempt of $Attempts)"
        Start-Sleep -Seconds (5 * $retryAttempt * $retryAttempt)
    }
    $global:LASTEXITCODE = $retryStatus
    $retryOutput
}

function Invoke-RmctlJson {
    # Runs a script block that calls rmctl with --json and returns the parsed
    # answer; throws when rmctl fails.
    param([Parameter(Mandatory)] [scriptblock] $Command)
    $json = Invoke-RmctlRetry $Command | Out-String
    if ($LASTEXITCODE -ne 0) { throw "rmctl failed with exit status $LASTEXITCODE" }
    $json | ConvertFrom-Json
}

function ConvertTo-LocalPath {
    # A library path below $Under, such as "/Clients/Harbor Logistics/Contract — Signed",
    # as a relative Windows path whose parts are valid file names.
    param(
        [Parameter(Mandatory)] [string] $Path,
        [string] $Under = '/'
    )
    $rest = $Path.Substring($Under.TrimEnd('/').Length).TrimStart('/')
    $invalid = '[' + [regex]::Escape(-join [IO.Path]::GetInvalidFileNameChars()) + ']'
    $parts = foreach ($part in $rest.Split('/')) {
        $name = ($part -replace $invalid, '_').TrimEnd('. ')
        if ($name) { $name } else { '_' }
    }
    $parts -join '\'
}

function Write-Utf8File {
    # Writes text as UTF-8 without a byte order mark, creating its directory.
    param(
        [Parameter(Mandatory)] [string] $Path,
        [Parameter(Mandatory)] [AllowEmptyString()] [string] $Text
    )
    $full = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($Path)
    $null = [IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($full))
    [IO.File]::WriteAllText($full, $Text)
}
```

The script block runs in the caller's scope, so it sees the caller's variables; the helper's own are named `$retry…` so as not to hide them. Any `rmctl` command goes through it:

```powershell
. "$HOME\bin\rm-common.ps1"

Invoke-RmctlRetry { rmctl put .\report.pdf --to /Reports --replace --yes }
if ($LASTEXITCODE -ne 0) { Write-Error "upload failed with exit status $LASTEXITCODE" }

$folders = @(Invoke-RmctlJson { rmctl find --under /Projects --json }) | Where-Object kind -eq 'folder'
```

`@(...)` keeps an answer a list when it has one element or none. `--replace` replaces the document of the same name and keeps its id and tags, but not its annotations; `--content-only` replaces only the PDF and keeps the annotations on the pages that remain.

`ConvertTo-LocalPath` maps a document to a file: `/Clients/Harbor Logistics/Contract — Signed` below `/Clients` becomes `Harbor Logistics\Contract — Signed`, and a character Windows forbids in a file name (`\ / : * ? " < > |`) becomes `_`, as does a trailing dot or space. Names stay as long as they are; a deep tree under a long folder can pass the classic 260-character limit, which Windows lifts when long paths are enabled. `Write-Utf8File` writes UTF-8 without a byte order mark, which Obsidian and `put --notebook` expect at the start of a file.

## Sending files to the tablet

`rm-send.ps1` uploads the files it is given into a folder of the library, `/Inbox` by default: PDFs, EPUBs and `.rmdoc` archives as they are, Markdown and text files as notebooks of typed text with `put --notebook`. A folder stands for the files directly in it. Each file is its own upload, retried on status 4, so one refused file does not stop the others and a retry never sends a file twice.

`$HOME\bin\rm-send.ps1`:

```powershell
#Requires -Version 7.2
# Upload files to the tablet: PDFs, EPUBs and .rmdoc archives as they are,
# Markdown and text files as notebooks of typed text. A folder sends the files
# directly in it. Works from the command line and from Explorer's "Send to".
#   rm-send.ps1 .\paper.pdf .\notes.md -Folder /Inbox -Device pro [-DryRun] [-Pause]
[CmdletBinding(PositionalBinding = $false)]
param(
    [Parameter(Mandatory, ValueFromRemainingArguments)]
    [string[]] $Path,
    [string] $Folder = '/Inbox',
    [ValidateSet('rm2', 'pure', 'pro', 'move')]
    [string] $Device = 'rm2',
    [switch] $DryRun,
    [switch] $Pause                 # keep the window open, for "Send to"
)
. "$PSScriptRoot\rm-common.ps1"

$documents = '.pdf', '.epub', '.rmdoc'
$texts = '.md', '.markdown', '.txt'
$mode = if ($DryRun) { '--dry-run' } else { '--yes' }
$failed = 0

try {
    $files = foreach ($p in $Path) {
        $item = Get-Item -LiteralPath $p
        if ($item.PSIsContainer) { Get-ChildItem -LiteralPath $item.FullName -File } else { $item }
    }
    foreach ($file in $files) {
        $extension = $file.Extension.ToLowerInvariant()
        if ($extension -in $documents) {
            $notebook = @()
        } elseif ($extension -in $texts) {
            $notebook = @('--notebook', '--device', $Device)
        } else {
            Write-Warning "skipped $($file.Name): not a PDF, EPUB, .rmdoc, Markdown or text file"
            continue
        }
        Invoke-RmctlRetry { rmctl put $file.FullName --to $Folder @notebook $mode --format plain }
        if ($LASTEXITCODE -ne 0) {
            Write-Warning "$($file.Name) was not sent (exit status $LASTEXITCODE)"
            $failed++
        }
    }
} catch {
    Write-Warning $_
    $failed++
} finally {
    if ($Pause) { $null = Read-Host 'Press Enter to close' }
}
exit [int]($failed -gt 0)
```

```powershell
PS> rm-send.ps1 '.\Scans\Proposal Draft.pdf' .\Notes\Note.md -DryRun
would upload	Proposal Draft	pdf	-	-	C:\Users\you\Scans\Proposal Draft.pdf	/Inbox
would make	Note	notebook	-	-	C:\Users\you\Notes\Note.md	/Inbox
```

The folder must exist (`rmctl mkdir /Inbox --yes`); `put` answers `not found` for one it cannot find. A Markdown file needs `--notebook`, and `--notebook` takes only `.md`, `.markdown` and `.txt`, which is why the script calls `put` per file with the right flags. `-Device` lays a notebook out for the tablet (`rm2`, `pure`, `pro` or `move`). A whole tree goes up with `mput` instead, which recreates its subfolders and, with `--incremental`, skips the documents already there by name: `rmctl mput "$HOME\Papers" /Papers --incremental --yes`.

**From Explorer.** A shortcut in the *Send to* folder runs the script on the files selected in Explorer, which appends their paths to the shortcut's arguments:

```powershell
$shell = New-Object -ComObject WScript.Shell
$link = $shell.CreateShortcut((Join-Path ([Environment]::GetFolderPath('SendTo')) 'reMarkable.lnk'))
$link.TargetPath = (Get-Command pwsh).Source
$link.Arguments = '-NoProfile -File "{0}" -Pause' -f (Join-Path $HOME 'bin\rm-send.ps1')
$link.WorkingDirectory = $HOME
$link.Save()
```

*Right-click → Send to → reMarkable* then opens a window that shows each upload and waits for Enter. Add `-Folder /Papers` or `-Device move` to `Arguments` for another folder or tablet, one shortcut per target. `-File` must come last among `pwsh`'s own options: everything after the script's path goes to the script.

PowerShell 7 runs local scripts under its default `RemoteSigned` policy. A script saved from a browser or a mail is marked as downloaded and refused until `Unblock-File -LiteralPath $HOME\bin\rm-send.ps1`.

## A hot folder

Drop a PDF or an EPUB into a local folder and it appears in `/Inbox` on the tablet. `rm-hotfolder.ps1` uploads each file on its own, moves what went up into `Sent` and what was refused into `Failed`, and retries a write on status 4. It waits until a file has stopped growing and no program holds it open for writing, so a scan still being saved is not sent half-done. A `FileSystemWatcher` wakes it when something lands; it also looks every 10 seconds, so a file that arrives while it is uploading is not missed.

`$HOME\bin\rm-hotfolder.ps1`:

```powershell
#Requires -Version 7.2
# Upload every PDF and EPUB that lands in a local folder, then move it into
# Sent or Failed. -Once handles what is there and exits; -DryRun only checks.
#   rm-hotfolder.ps1 [-Hot "$HOME\reMarkable Inbox"] [-Folder /Inbox] [-Once] [-DryRun]
param(
    [string] $Hot = (Join-Path $HOME 'reMarkable Inbox'),
    [string] $Folder = '/Inbox',
    [switch] $Once,
    [switch] $DryRun
)
. "$PSScriptRoot\rm-common.ps1"

$Hot = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($Hot)
$sent = Join-Path $Hot 'Sent'
$failed = Join-Path $Hot 'Failed'
foreach ($dir in $Hot, $sent, $failed) { $null = [IO.Directory]::CreateDirectory($dir) }

function Test-Settled([IO.FileInfo] $File) {
    # True when the file kept its size for 2 seconds and nobody has it open
    # for writing: a scanner or a browser is done with it.
    $before = $File.Length
    Start-Sleep -Seconds 2
    $File.Refresh()
    if (-not $File.Exists -or $File.Length -eq 0 -or $File.Length -ne $before) { return $false }
    try {
        $File.Open('Open', 'Read', 'None').Dispose()
        return $true
    } catch {
        return $false
    }
}

function Send-File([IO.FileInfo] $File) {
    $mode = if ($DryRun) { '--dry-run' } else { '--yes' }
    Invoke-RmctlRetry { rmctl put $File.FullName --to $Folder $mode --format plain }
    $status = $LASTEXITCODE
    if ($status -in 2, 3, 130) {
        # A usage error, no registration or Ctrl-C: every file would fail the
        # same way, so stop and leave the file where it is.
        Write-Warning "rmctl exited with status $status; stopping"
        exit $status
    }
    if ($DryRun) { return }
    if ($status -eq 0) {
        $target = $sent
    } else {
        Write-Warning "upload of $($File.Name) failed (exit status $status)"
        $target = $failed
    }
    $stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
    [IO.File]::Move($File.FullName, (Join-Path $target "$stamp-$($File.Name)"))
}

function Invoke-Scan {
    Get-ChildItem -LiteralPath $Hot -File |
        Where-Object { $_.Extension -in '.pdf', '.epub' } |
        ForEach-Object { if (Test-Settled $_) { Send-File $_ } }
}

Invoke-Scan
if ($Once) { exit 0 }

$watcher = [IO.FileSystemWatcher]::new($Hot)
$changes = [IO.WatcherChangeTypes] 'Created, Changed, Renamed'
while ($true) {
    # Returns on a change in the folder, or after 10 seconds: look either way.
    $null = $watcher.WaitForChanged($changes, 10000)
    Invoke-Scan
}
```

```powershell
PS> rm-hotfolder.ps1 -Hot "$HOME\Scans" -Once -DryRun
would upload	Proposal Draft	pdf	-	-	C:\Users\you\Scans\Proposal Draft.pdf	/Inbox
```

The target folder must exist (`rmctl mkdir /Inbox --yes`). `-DryRun` checks every file and the folder and moves nothing; `-Once` handles what is there and exits, for a scheduled task that runs every few minutes instead of a watcher. `-in` compares extensions without case, so `SCAN.PDF` counts. Ctrl-C stops the script at the end of the current wait, within 10 seconds. `Sent` only grows; a line in a scheduled task keeps it to a month: `Get-ChildItem -LiteralPath "$HOME\reMarkable Inbox\Sent" -File | Where-Object LastWriteTime -lt (Get-Date).AddDays(-30) | Remove-Item`.

**Starting it at logon.** Task Scheduler starts the watcher when you sign in, in a hidden window:

```powershell
$action = New-ScheduledTaskAction -Execute (Get-Command pwsh).Source `
    -Argument ('-NoProfile -NonInteractive -WindowStyle Hidden -File "{0}"' -f (Join-Path $HOME 'bin\rm-hotfolder.ps1'))
$trigger = New-ScheduledTaskTrigger -AtLogOn -User "$env:USERDOMAIN\$env:USERNAME"
$settings = New-ScheduledTaskSettingsSet -ExecutionTimeLimit (New-TimeSpan) -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries
Register-ScheduledTask -TaskName 'rmctl hot folder' -Action $action -Trigger $trigger -Settings $settings
Start-ScheduledTask -TaskName 'rmctl hot folder'       # start it now, without signing out
```

`-ExecutionTimeLimit (New-TimeSpan)`, a zero time span, lifts Task Scheduler's default limit of three days. The console window may flash for a moment at logon before `-WindowStyle Hidden` hides it. `Stop-ScheduledTask` and `Unregister-ScheduledTask` stop and remove it.

## Highlights to Markdown

`highlights --format markdown` writes a document's highlights as quotes under a heading per page, the words as the tablet copied them out of the PDF's text layer. `rm-highlights.ps1` finds every PDF and EPUB below a folder with `find --json` and writes one Markdown file per document that has highlights, keeping the folder tree.

`$HOME\bin\rm-highlights.ps1`:

```powershell
#Requires -Version 7.2
# Write the highlights of every PDF and EPUB below a folder as Markdown files.
#   rm-highlights.ps1 -Folder /Clients -Out "$HOME\Notes\Highlights"
param(
    [Parameter(Mandatory)] [string] $Folder,
    [Parameter(Mandatory)] [string] $Out
)
. "$PSScriptRoot\rm-common.ps1"

$Folder = '/' + $Folder.Trim('/')
$items = @(Invoke-RmctlJson { rmctl find --under $Folder --json })

foreach ($doc in $items | Where-Object { $_.kind -notin 'folder', 'notebook' }) {
    $lines = @(rmctl highlights $doc.id --format markdown 2>$null)
    if ($LASTEXITCODE -ne 0) { Write-Warning "skipped $($doc.path)"; continue }
    if (-not ($lines -join '').Trim()) { continue }        # no highlights: no file
    $file = Join-Path $Out ((ConvertTo-LocalPath $doc.path -Under $Folder) + '.md')
    Write-Utf8File $file (((@("# $($doc.name)", '') + $lines) -join "`n") + "`n")
    $file
}
```

```powershell
PS> rm-highlights.ps1 -Folder /Clients -Out "$HOME\Notes\Highlights"
C:\Users\you\Notes\Highlights\Harbor Logistics\Contract — Signed.md

PS> Get-Content -LiteralPath "$HOME\Notes\Highlights\Harbor Logistics\Contract — Signed.md"
# Contract — Signed

## Page 1

> uses

> uses for its own, and no less than reasonable care
```

A document without highlights answers with nothing on standard output and a note on standard error (`Proposal Draft has no highlights`), which `2>$null` drops, so it gets no file. Lines are joined with LF, as Markdown tools expect. For one document, `--format json` carries the page, the color and the text of each highlight:

```powershell
$h = rmctl highlights ef0f6df3 --format json | Out-String | ConvertFrom-Json
$h.highlights | Where-Object color -eq 'yellow' | ForEach-Object text
$h.highlights | Group-Object color | Select-Object Name, Count
```

A highlight over a page without a text layer, such as a scan, has no words: its `has_text` is false and the text answers list it as `(no text)`.

## Typed notes into an Obsidian vault

`text` prints the typed text of a notebook — what was typed with the keyboard, not handwriting, which is ink and is not recognized. `--format markdown` keeps headings, bold lines, bullets, numbered items and checkboxes. `rm-notes-to-vault.ps1` copies every notebook below a folder into a vault as a note with front matter, and skips the notebooks whose record hash has not changed since the last run, without downloading them.

`$HOME\bin\rm-notes-to-vault.ps1`:

```powershell
#Requires -Version 7.2
# Copy the typed text of the notebooks below a folder into Markdown notes, such
# as an Obsidian vault. Unchanged notebooks are not downloaded again.
#   rm-notes-to-vault.ps1 -Folder /Meetings -Vault "$HOME\Documents\Vault\Meetings"
param(
    [Parameter(Mandatory)] [string] $Folder,
    [Parameter(Mandatory)] [string] $Vault
)
. "$PSScriptRoot\rm-common.ps1"

$Folder = '/' + $Folder.Trim('/')
$items = @(Invoke-RmctlJson { rmctl find --under $Folder --json })

foreach ($nb in $items | Where-Object kind -eq 'notebook') {
    $note = Join-Path $Vault ((ConvertTo-LocalPath $nb.path -Under $Folder) + '.md')
    if ((Test-Path -LiteralPath $note) -and
        (Select-String -LiteralPath $note -Pattern "^rm_hash: $($nb.hash)$" -Quiet)) {
        continue                                            # unchanged since the last run
    }
    $lines = @(rmctl text $nb.id --format markdown 2>$null)
    if ($LASTEXITCODE -ne 0) { Write-Warning "skipped $($nb.path)"; continue }
    # A notebook of handwriting only has page headings and no text.
    if (-not ($lines | Where-Object { $_ -notmatch '^(## Page \d+)?$' })) { continue }

    $utc = ([datetime] $nb.modified).ToUniversalTime()
    $modified = $utc.ToString("yyyy-MM-dd'T'HH:mm:ss'Z'", [cultureinfo]::InvariantCulture)
    $yamlPath = $nb.path.Replace('\', '\\').Replace('"', '\"')
    $front = @(
        '---'
        "rm_id: $($nb.id)"
        "rm_path: `"$yamlPath`""
        "rm_modified: $modified"
        "rm_hash: $($nb.hash)"
        '---'
        ''
    )
    Write-Utf8File $note ((($front + $lines) -join "`n") + "`n")
    $note
}
```

```powershell
PS> rm-notes-to-vault.ps1 -Folder /Meetings -Vault "$HOME\Documents\Vault\Meetings"
C:\Users\you\Documents\Vault\Meetings\Retro.md
PS> rm-notes-to-vault.ps1 -Folder /Meetings -Vault "$HOME\Documents\Vault\Meetings"
PS> Get-Content -LiteralPath "$HOME\Documents\Vault\Meetings\Retro.md"
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
…
```

The second run writes nothing: every note's `rm_hash` matches. The note is rewritten whenever the notebook changes, so edits belong on the tablet, not in the vault. `ConvertFrom-Json` in PowerShell 7 turns `modified` into a `[datetime]`; the script writes it back as UTC, the way rmctl prints it. A notebook with several pages has a `## Page N` heading per page.

The JSON answer gives each paragraph with its style (`heading`, `bold`, `bullet`, `numbered`, `plain`), and a checkbox reads as a bullet whose text starts with `[ ] `, so the open action items of a notebook are:

```powershell
PS> $t = rmctl text 019a --format json | Out-String | ConvertFrom-Json
PS> $t.pages.paragraphs | Where-Object { $_.text.StartsWith('[ ] ') } | ForEach-Object { $_.text.Substring(4) }
Book the venue for the Q4 kickoff
```

`StartsWith` rather than `-like`, because `-like` reads `[ ]` as a wildcard. `rmctl find --text "release notes"` searches the same typed text, and the highlighted words, across the library.

## Nightly backup with Task Scheduler

`backup` keeps one `.rmdoc` archive per document and a `manifest.json` in a directory, and on the next run downloads only the documents whose record hash changed; run nightly, it costs one request when nothing changed. `rm-backup.ps1` runs it, appends a line to a log, and with `-Notify` shows a Windows notification through the [BurntToast](https://github.com/Windos/BurntToast) module when it is installed (`Install-Module BurntToast -Scope CurrentUser`).

`$HOME\bin\rm-backup.ps1`:

```powershell
#Requires -Version 7.2
# Incremental backup of a reMarkable library, with a log line per run.
#   rm-backup.ps1 [-Dest "$HOME\Backups\reMarkable"] [-LibraryPath /] [-Notify]
param(
    [string] $Dest = (Join-Path $HOME 'Backups\reMarkable'),
    [string] $LibraryPath = '/',
    [string] $Log = (Join-Path $env:LOCALAPPDATA 'rmctl-scripts\backup.log'),
    [int] $KeepRemovedDays = 90,
    [switch] $Notify
)
. "$PSScriptRoot\rm-common.ps1"

$null = [IO.Directory]::CreateDirectory($Dest)
$null = [IO.Directory]::CreateDirectory((Split-Path $Log))

$answer = rmctl backup $Dest $LibraryPath --jobs 4 --json | Out-String
$status = $LASTEXITCODE
switch ($status) {
    0 {
        $r = $answer | ConvertFrom-Json
        $message = "$($r.documents) documents: $($r.written) written, $($r.skipped) unchanged, " +
            "$($r.moved) moved to removed ($($r.outside) outside the path), $($r.pruned) pruned"
    }
    3 { $message = "rmctl is not registered: run 'rmctl register <code>'" }
    default { $message = "backup failed with exit status $status" }
}
$line = '{0} {1}' -f (Get-Date -Format 'yyyy-MM-dd HH:mm:ss'), $message
Add-Content -LiteralPath $Log -Value $line
$line

# Archives of deleted documents wait in removed\; keep them for a while.
Get-ChildItem -LiteralPath (Join-Path $Dest 'removed') -Filter *.rmdoc -ErrorAction Ignore |
    Where-Object LastWriteTime -lt (Get-Date).AddDays(-$KeepRemovedDays) |
    Remove-Item

if ($Notify -and (Get-Module -ListAvailable -Name BurntToast)) {
    Import-Module BurntToast
    New-BurntToastNotification -Text 'reMarkable backup', $message
}
exit $status
```

```powershell
PS> rm-backup.ps1 -LibraryPath /Clients
2026-11-04 20:51:44 5 documents: 5 written, 0 unchanged, 0 moved to removed (0 outside the path), 0 pruned
PS> rm-backup.ps1 -LibraryPath /Clients
2026-11-04 20:51:45 5 documents: 0 written, 5 unchanged, 0 moved to removed (0 outside the path), 0 pruned
```

The archive of a document deleted from the library, or put in the trash, is moved into `removed\`, and the script deletes those older than 90 days; `--prune` on `backup` deletes them at once instead. A directory belongs to the profile and the path of its first backup: give each profile and path its own directory. `Add-Content` in PowerShell 7 writes UTF-8, so names in the log survive.

**Scheduling it.** A task that runs at 02:30 every night, and at the next chance when the PC was off or asleep then:

```powershell
$action = New-ScheduledTaskAction -Execute (Get-Command pwsh).Source `
    -Argument ('-NoProfile -NonInteractive -WindowStyle Hidden -File "{0}" -Notify' -f (Join-Path $HOME 'bin\rm-backup.ps1'))
$trigger = New-ScheduledTaskTrigger -Daily -At 2:30am
$settings = New-ScheduledTaskSettingsSet -StartWhenAvailable -WakeToRun -ExecutionTimeLimit (New-TimeSpan -Hours 2)
Register-ScheduledTask -TaskName 'rmctl backup' -Action $action -Trigger $trigger -Settings $settings `
    -Description 'Incremental backup of the reMarkable library'

Start-ScheduledTask -TaskName 'rmctl backup'                   # run it once now
Get-ScheduledTaskInfo -TaskName 'rmctl backup' | Select-Object LastRunTime, LastTaskResult
Get-Content -LiteralPath "$env:LOCALAPPDATA\rmctl-scripts\backup.log" -Tail 5
```

`LastTaskResult` is the script's exit status, rmctl's: 0 for success, 3 when the profile is not registered. Registered this way, the task runs as you, only while you are signed in, which is what a notification needs. To run it while nobody is signed in, add `-Principal (New-ScheduledTaskPrincipal -UserId $env:USERNAME -LogonType S4U)` to `Register-ScheduledTask`, from an elevated window if Windows asks for one; such a task has no desktop, so leave out `-Notify`. The task finds `rmctl` through your user `Path`; a full path to `rmctl.exe` in the script works too.

**Restoring.** `backup restore` imports the archives back into the folders the manifest records, in one commit; a document whose id the library still holds comes back as a copy under a new id. Look first:

```powershell
rmctl backup restore "$HOME\Backups\reMarkable" --to /Restored --dry-run
rmctl backup restore "$HOME\Backups\reMarkable" --to /Restored --yes

# The id of a lost document, from the manifest:
(Get-Content -LiteralPath "$HOME\Backups\reMarkable\manifest.json" -Raw | ConvertFrom-Json).documents |
    Where-Object path -like '*Retro*' | Select-Object id, path
```

## Reacting to changes

`watch --exec` runs a program each time the library changes ([watch.md](watch.md)). The program gets the batch as JSON on its standard input, `{"profile": ..., "dropped": N, "events": [{"time", "event", "id", "name", "path", "kind", "this_device"}, ...]}`, and the last event in its environment: `RMCTL_EVENT`, `RMCTL_ID`, `RMCTL_NAME`, `RMCTL_PATH`, `RMCTL_KIND`, `RMCTL_THIS_DEVICE`, `RMCTL_TIME`, `RMCTL_COUNT`, `RMCTL_DROPPED` and `RMCTL_PROFILE`. Names never go into its command line. An item no longer in the library has no path or kind.

This handler keeps Markdown copies: for each document of the batch that is still in the library, the typed text of a notebook or the highlights of a PDF or EPUB, mirroring the library's folders. To keep PDFs as well, the handler can end with `rmctl -P $rmProfile render /Meetings -o "$HOME\Documents\reMarkable PDF" --incremental`, which redraws only the documents that changed ([rendering.md](rendering.md)).

`$HOME\bin\rm-on-change.ps1`:

```powershell
#Requires -Version 7.2
# Run by "rmctl watch --exec pwsh": keep a Markdown copy of the typed text
# (notebooks) or the highlights (PDFs and EPUBs) of every document of the batch.
param([string] $Out = (Join-Path $HOME 'Documents\reMarkable Markdown'))
. "$PSScriptRoot\rm-common.ps1"

# The batch, read as UTF-8 whatever the console's code page.
$reader = [IO.StreamReader]::new([Console]::OpenStandardInput(), [Text.UTF8Encoding]::new())
$batch = $reader.ReadToEnd() | ConvertFrom-Json

if ([int] $env:RMCTL_DROPPED -gt 0) {
    Write-Warning "missed $env:RMCTL_DROPPED events: run rm-highlights.ps1 and rm-notes-to-vault.ps1 on the whole folder"
}
$rmProfile = if ($env:RMCTL_PROFILE) { $env:RMCTL_PROFILE } else { 'default' }

# The last event of each document still in the library.
$docs = @($batch.events) |
    Where-Object { $_.path -and $_.kind -ne 'folder' } |
    Group-Object id |
    ForEach-Object { $_.Group[-1] }

foreach ($doc in $docs) {
    $command = if ($doc.kind -eq 'notebook') { 'text' } else { 'highlights' }
    $lines = @(rmctl -P $rmProfile $command $doc.id --format markdown 2>$null)
    if ($LASTEXITCODE -ne 0) { Write-Warning "could not read $($doc.path)"; continue }
    if (-not ($lines -join '').Trim()) { continue }        # nothing typed or highlighted
    $file = Join-Path $Out ((ConvertTo-LocalPath $doc.path) + '.md')
    Write-Utf8File $file (((@("# $($doc.name)", '') + $lines) -join "`n") + "`n")
    "updated $file"
}
```

Run it with the full path of `pwsh.exe`, so it does not depend on the `Path` that `watch` sees, and the handler's options after `'--'`:

```powershell
$pwsh = (Get-Command pwsh).Source
rmctl watch --exec $pwsh --only document --under /Meetings '--' -NoProfile -NonInteractive -File "$HOME\bin\rm-on-change.ps1"
```

`'--'` is quoted so that PowerShell hands it to rmctl as an argument instead of taking it as its own end-of-parameters marker; everything after it goes to `pwsh`. `--only document` and `--under /Meetings` drop the other events before the handler runs. The handler's output and warnings go to `watch`'s standard error. A batch holds at most 1000 events; past that the oldest are dropped and `RMCTL_DROPPED` says how many, and a handler that sees a non-zero count should rescan rather than trust the batch. A run may take `--exec-timeout` (5m); a failing run is noted and does not stop `watch`. On Ctrl-C, `watch` gives a run under way 5 seconds and exits 130.

The handler can be tried without waiting for the tablet, by handing it a batch:

```powershell
'{"profile":"default","dropped":0,"events":[{"event":"DocAdded","id":"019a539e-a027-4b1e-99b3-3a20bdc8b537","name":"Retro","path":"/Meetings/Retro","kind":"notebook"}]}' |
    pwsh -NoProfile -File "$HOME\bin\rm-on-change.ps1"
```

For a one-line handler, `-Command` instead of a script; the environment variables arrive as Unicode, with no encoding to set:

```powershell
rmctl watch --exec $pwsh '--' -NoProfile -Command 'Add-Content -LiteralPath $HOME\rm-changes.log -Value (''{0} {1} {2}'' -f $env:RMCTL_TIME, $env:RMCTL_EVENT, $env:RMCTL_PATH)'
```

The outer single quotes keep `$HOME` and the variables for the `pwsh` that `watch` starts, and `''` is a single quote inside them. The command holds no double quotes, which PowerShell before 7.3 would mangle on their way to a native program.

**Keeping watch running.** `watch` reconnects by itself when the socket drops. A small loop starts it again if it exits for any other reason than Ctrl-C, and Task Scheduler starts the loop at logon, as for the [hot folder](#a-hot-folder) with `rm-watch.ps1` in `-Argument`:

`$HOME\bin\rm-watch.ps1`:

```powershell
#Requires -Version 7.2
# Keep "rmctl watch --exec" running, with rm-on-change.ps1 as its handler.
[Console]::OutputEncoding = [Text.UTF8Encoding]::new()
$pwsh = (Get-Command pwsh).Source
$handler = Join-Path $PSScriptRoot 'rm-on-change.ps1'
while ($true) {
    rmctl watch --exec $pwsh --only document --under /Meetings '--' -NoProfile -NonInteractive -File $handler
    if ($LASTEXITCODE -eq 130) { break }                   # Ctrl-C
    Write-Warning "rmctl watch exited with status $LASTEXITCODE; starting again in a minute"
    Start-Sleep -Seconds 60
}
```

`watch` prints each event on standard output; in a hidden task that goes nowhere, and `rmctl watch ... > "$env:LOCALAPPDATA\rmctl-scripts\watch.log"` keeps it.

## Library reports

`du` and `dupes` read only the library's indexes, and `find --json` lists every item with its kind, tags, star and modification time, so a report on the whole library takes one online read and no downloads. `rm-report.ps1` refreshes the cache once and then reads only with `--offline`, so every section sees the same library, and shows the sections with `Format-Table`; `-Csv` also writes every document to a CSV file for Excel.

`$HOME\bin\rm-report.ps1`:

```powershell
#Requires -Version 7.2
# A report on the library: space, duplicates, untagged and stale documents.
# One online read refreshes the cache; every other read is --offline.
#   rm-report.ps1 [-StaleDays 90] [-Csv "$HOME\Documents\reMarkable.csv"]
param(
    [int] $StaleDays = 90,
    [string] $Csv
)
. "$PSScriptRoot\rm-common.ps1"

$null = rmctl ls /                                        # refresh the cache once
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

$du = Invoke-RmctlJson { rmctl --offline du --json }
'== Space: {0:N1} MiB in {1} documents' -f ($du.bytes / 1MB), $du.documents
$du.subfolders | Format-Table path, documents,
    @{ Name = 'KiB'; Expression = { [math]::Round($_.bytes / 1KB) }; Alignment = 'Right' }
$du.by_kind | Format-Table kind, documents,
    @{ Name = 'MiB'; Expression = { '{0:N1}' -f ($_.bytes / 1MB) }; Alignment = 'Right' }

$dupes = Invoke-RmctlJson { rmctl --offline dupes --json }
'== Duplicates: {0:N0} KiB taken by extra copies' -f ($dupes.wasted / 1KB)
foreach ($group in $dupes.groups) { '  ' + ($group.documents.path -join '  =  ') }

$items = @(Invoke-RmctlJson { rmctl --offline find --json })
$docs = @($items | Where-Object kind -ne 'folder')

''
'== Documents without tags'
$docs | Where-Object { @($_.tags).Count -eq 0 } | Format-Table path, kind

"== Not modified for $StaleDays days"
$cutoff = [datetime]::UtcNow.AddDays(-$StaleDays)
$docs | Where-Object { ([datetime] $_.modified).ToUniversalTime() -lt $cutoff } |
    Sort-Object { [datetime] $_.modified } |
    Format-Table @{ Name = 'modified'; Expression = { ([datetime] $_.modified).ToString('yyyy-MM-dd') } }, path

if ($Csv) {
    $docs |
        Select-Object id, path, kind, size, modified, pinned, @{ Name = 'tags'; Expression = { $_.tags -join ';' } } |
        Export-Csv -LiteralPath $Csv -NoTypeInformation -Encoding utf8BOM
    "wrote $Csv"
}
```

```powershell
PS> rm-report.ps1
== Space: 7.8 MiB in 41 documents

path      documents  KiB
----      ---------  ---
/Inbox            5 2634
/Projects         9  413
/Reading          4  388
…

== Duplicates: 24 KiB taken by extra copies
  /Finance/Budget 2027 — Working Notes  =  /Inbox/Budget 2027 — Working Notes
…
```

`dupes` compares the PDF or EPUB a document was made from, not its annotations, and never lists notebooks. `utf8BOM` makes Excel read the CSV as UTF-8; where the list separator is `;`, `-UseCulture` instead of the default comma opens it in columns. Other questions are one pipeline over the same `$items`:

```powershell
$items | Where-Object pinned | Select-Object path                                  # starred
$items | Where-Object kind -ne 'folder' | Sort-Object size -Descending |
    Select-Object -First 10 path, @{ Name = 'KiB'; Expression = { [math]::Round($_.size / 1KB) } }
@(Invoke-RmctlJson { rmctl find --all --json }) | Where-Object trashed | Select-Object path   # in the trash
$items | Group-Object kind | Sort-Object Count -Descending | Select-Object Name, Count
$items | Out-GridView                                                                # browse in a window
```

`Out-GridView` needs a desktop session. A weekly CSV is one more scheduled task, registered as the [backup](#nightly-backup-with-task-scheduler) is, with `New-ScheduledTaskTrigger -Weekly -DaysOfWeek Monday -At 8am` and `-Argument ('-NoProfile -NonInteractive -WindowStyle Hidden -File "{0}" -Csv "{1}"' -f (Join-Path $HOME 'bin\rm-report.ps1'), (Join-Path $HOME 'Documents\reMarkable.csv'))`.

## Profiles and a second account

A profile is one account on this machine, with its own token and cache. Register a second one with a one-time code from [my.remarkable.com](https://my.remarkable.com/device/desktop/connect), then name it with `-P` on any command, or set `$env:RMCTL_PROFILE` for the rest of a session:

```powershell
rmctl profile add work abcdefgh
rmctl profile ls
rmctl -P work ls /

$env:RMCTL_PROFILE = 'work'                   # every rmctl in this window uses it
rmctl ls /
Remove-Item Env:RMCTL_PROFILE

rmctl profile ls --json | Out-String | ConvertFrom-Json | Format-Table profile, current, registered, secrets, cache_bytes
```

`export` packs documents of one account, with their pages, ink and tags, into an `.rmdoc` archive, and `put` imports the archive into the other; a document whose id the target already holds gets a new one. `rm-copy-account.ps1` copies documents or whole folders, every document into the one target folder.

`$HOME\bin\rm-copy-account.ps1`:

```powershell
#Requires -Version 7.2
# Copy documents from one account (profile) to another, ink and all.
#   rm-copy-account.ps1 -From work -Source '/Projects/Q4 Product Launch' -To default -Destination /FromWork [-DryRun]
param(
    [Parameter(Mandatory)] [string] $From,
    [Parameter(Mandatory)] [string[]] $Source,
    [Parameter(Mandatory)] [string] $To,
    [Parameter(Mandatory)] [string] $Destination,
    [switch] $DryRun
)
. "$PSScriptRoot\rm-common.ps1"

$archive = Join-Path ([IO.Path]::GetTempPath()) "rmctl-copy-$PID.rmdoc"
$mode = if ($DryRun) { '--dry-run' } else { '--yes' }
try {
    $packed = Invoke-RmctlJson { rmctl -P $From export @Source -o $archive --json }
    "packed $($packed.documents) documents"
    Invoke-RmctlRetry { rmctl -P $To put $archive --to $Destination $mode --format plain }
    $status = $LASTEXITCODE
} finally {
    Remove-Item -LiteralPath $archive -ErrorAction Ignore
}
exit $status
```

A dry run on the test account, copying within the one profile it has:

```powershell
PS> rm-copy-account.ps1 -From default -Source /Strategy -To default -Destination /Inbox -DryRun
packed 3 documents
would import	2027 OKRs — Draft	pdf	a2482667-d0df-4fb7-9b0f-50caa7f0676d	02b64e01-a359-470a-9774-6afdd3558f49	C:\Users\you\AppData\Local\Temp\rmctl-copy-4242.rmdoc	/Inbox
would import	Market Expansion Brainstorm	pdf	ad75a868-5c52-426d-995c-829f1eff3b4f	bf6820e4-4cbf-4515-a1ff-c221921cb64f	C:\Users\you\AppData\Local\Temp\rmctl-copy-4242.rmdoc	/Inbox
would import	Quarterly Review	pdf	5a233d4f-587d-4355-a713-3987f8facd3d	6a6bb31a-eaa8-4605-af35-1c2c7ba3358c	C:\Users\you\AppData\Local\Temp\rmctl-copy-4242.rmdoc	/Inbox
```

The fourth column is the new id the copy would get, the fifth the id it had. To copy a tree with its subfolders, back it up from one profile and restore it into the other: `backup restore` recreates the folder paths the manifest records, whichever profile wrote it.

```powershell
rmctl -P work backup "$HOME\rm-work-projects" /Projects
rmctl -P default backup restore "$HOME\rm-work-projects" --to /From-work --dry-run
```

## Windows PowerShell 5.1

The `powershell.exe` that ships with Windows is 5.1, and the scripts above stop on their `#Requires -Version 7.2` line there. Installing PowerShell 7 (`winget install Microsoft.PowerShell`) is the simple way. Calling `rmctl` from 5.1 works; these are the differences that matter for it:

- **Execution policy.** On Windows clients 5.1 runs no scripts at all by default: `Set-ExecutionPolicy -Scope CurrentUser RemoteSigned` allows local ones.
- **UTF-8.** Set `[Console]::OutputEncoding = [Text.UTF8Encoding]::new()` here too. `$OutputEncoding`, the encoding of text piped *into* a native program, is ASCII in 5.1: set it to `[Text.UTF8Encoding]::new()` as well before piping a batch into a handler.
- **Files.** `-Encoding utf8` writes a byte order mark in 5.1, and there is no `utf8NoBOM`; `[IO.File]::WriteAllText($path, $text)`, as in `Write-Utf8File`, writes without one (with a full path: .NET resolves a relative one against the process's directory, not PowerShell's location). `Add-Content` and `Set-Content` without `-Encoding` write the ANSI code page.
- **JSON.** `ConvertFrom-Json` does not unroll an array: piped straight into `Where-Object`, the whole list arrives as one object. Assign it to a variable first, `$items = rmctl find --json | Out-String | ConvertFrom-Json`, and pipe the variable. Dates stay strings, which `[datetime]` converts; `-AsHashtable` and `-Depth` do not exist.
- **Standard error.** With `$ErrorActionPreference = 'Stop'`, `2>$null` or `2>&1` on a native command turns rmctl's first note on standard error into a terminating error. Leave the preference at `Continue` around such calls.
- **Arguments.** 5.1 drops an empty string argument and mangles one with embedded double quotes; PowerShell 7.3 and later pass both as written. Avoid `--name ""`, and pass a name with quotes in it through a variable.
- **`$PSNativeCommandUseErrorActionPreference`** does not exist before 7.3: a non-zero exit status never throws, so check `$LASTEXITCODE` as the scripts do.
- **`New-Object -ComObject`, `Register-ScheduledTask` and `FileSystemWatcher`** work the same; `::new()` needs 5.0 or later.
