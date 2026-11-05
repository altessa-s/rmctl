# AppleScript

Back to the [README](../README.md).

On macOS, AppleScript puts `rmctl` behind the Finder, Mail, Notes, Preview and the clipboard: a script runs the command with `do shell script`, reads its answer, and shows the outcome in a dialog or a notification. This page collects scenarios, each with a complete script and how to install it. One of them is also given in JavaScript for Automation (JXA), where reading JSON is shorter.

The scripts were compiled with `osacompile` and run with `osascript` against a test account, the uploads with `--dry-run`; the parts that open a dialog, a list or another application were only compiled. They work with an `rmctl` installed by Homebrew; for another location, change the `rmctl` property at the top of each script.

## Contents

- [Ground rules](#ground-rules)
- [Send the Finder selection](#send-the-finder-selection)
- [A Quick Action in Shortcuts or Automator](#a-quick-action-in-shortcuts-or-automator)
- [A hot folder](#a-hot-folder)
- [Render a document and open it in Preview](#render-a-document-and-open-it-in-preview)
- [A page on the clipboard](#a-page-on-the-clipboard)
- [Highlights to the clipboard](#highlights-to-the-clipboard)
- [A notebook into Apple Notes](#a-notebook-into-apple-notes)
- [PDF attachments from Mail](#pdf-attachments-from-mail)
- [A daily backup](#a-daily-backup)
- [Library usage in a dialog](#library-usage-in-a-dialog)
- [Permissions](#permissions)

## Ground rules

- **Find the binary.** `do shell script` runs `/bin/sh` with a minimal `PATH` (`/usr/bin:/bin:/usr/sbin:/sbin`), which does not reach Homebrew. Each script starts with a property that sets it, so one line serves Apple silicon (`/opt/homebrew/bin`) and Intel Macs (`/usr/local/bin`):

  ```applescript
  property rmctl : "PATH=/opt/homebrew/bin:/usr/local/bin:$PATH rmctl"
  ```

  A full path such as `"/opt/homebrew/bin/rmctl"` works too. When neither finds it, the shell exits with status 127.
- **Quote every argument.** Paths and names carry spaces, quotes and dashes: pass each through `quoted form of`, as in `rmctl & " render " & quoted form of theId`.
- **Say `--yes`.** A script is not a terminal, so a writing command refuses with exit status 2 unless `--yes` is given. The uploading scripts keep the flag in a `confirm` property: set it to `"--dry-run"` and they check every file and the folder and send nothing.
- **Read the answer.** `do shell script` returns standard output; notes on standard error are dropped when the command succeeds. In a script the answer is plain, one TAB-separated line per record, which `text item delimiters` splits; `--json` is the stable interface, read here with AppleScriptObjC's `NSJSONSerialization` or with `JSON.parse` in JXA. `do shell script` turns line feeds into returns unless `without altering line endings`, which matters for Markdown put on the clipboard.
- **Name items by identifier.** The list scripts show paths but pass the `id` from `find`, which still works when two documents share a path.
- **Handle the exit status.** A failing command raises an error whose message is the command's standard error (`rmctl: put: not found`) and whose number is its exit status: 1 for a failure, 2 for a usage error, 3 for a profile that is not registered, 4 for a library that kept changing under a write (see [Automation](automation.md#ground-rules-for-scripts)). `try … on error errMsg number errNum` catches it; -128 is the user's Cancel.
- **Pick the account.** Add `-P name` to the property (`"… rmctl -P work"`) to use another profile than `default`.

Every script runs from Script Editor (open it, press Run) or from a terminal with `osascript file.applescript`. Saved as a compiled script (`.scpt`) in `~/Library/Scripts`, it appears in the Script menu once Script Editor's Settings › General › "Show Script menu in menu bar" is on.

## Send the Finder selection

Select PDFs, EPUBs or Markdown files in the Finder and run this script: the documents go up as they are, the Markdown and text files as notebooks of typed text. `put --notebook` takes only text files, so the script makes two calls.

`~/Library/Scripts/Send to reMarkable.scpt`:

```applescript
-- Send the files selected in the Finder to a folder of the reMarkable library.
-- PDFs and EPUBs go up as they are; Markdown and text files become notebooks.

property rmctl : "PATH=/opt/homebrew/bin:/usr/local/bin:$PATH rmctl"
property destination : "/Inbox"
property confirm : "--yes" -- "--dry-run" checks everything and sends nothing

on run
	tell application "Finder" to set theFiles to selection as alias list
	if theFiles is {} then
		display dialog "Select PDF, EPUB or Markdown files in the Finder first." buttons {"OK"} default button 1 with title "Send to reMarkable" with icon caution
		return
	end if
	try
		set sent to sendFiles(theFiles)
		display notification (sent as text) & " file(s) sent to " & destination with title "Send to reMarkable"
	on error errMsg number errNum
		if errNum is 3 then set errMsg to "rmctl is not registered: run 'rmctl register <code>' in Terminal."
		if errNum is 127 then set errMsg to "rmctl was not found in /opt/homebrew/bin or /usr/local/bin."
		display dialog errMsg buttons {"OK"} default button 1 with title "Send to reMarkable" with icon stop
	end try
end run

-- Uploads the documents and the notes among theFiles, in two calls: --notebook
-- takes only text files. Returns how many files were sent.
on sendFiles(theFiles)
	set docArgs to ""
	set noteArgs to ""
	set sent to 0
	repeat with f in theFiles
		set p to POSIX path of f
		if p ends with ".pdf" or p ends with ".epub" then
			set docArgs to docArgs & " " & quoted form of p
			set sent to sent + 1
		else if p ends with ".md" or p ends with ".markdown" or p ends with ".txt" then
			set noteArgs to noteArgs & " " & quoted form of p
			set sent to sent + 1
		end if
	end repeat
	if sent is 0 then error "None of the selected files is a PDF, an EPUB or a Markdown file."
	set whereTo to " --to " & quoted form of destination & " " & confirm
	if docArgs is not "" then do shell script rmctl & " put" & docArgs & whereTo
	if noteArgs is not "" then do shell script rmctl & " put" & noteArgs & whereTo & " --notebook"
	return sent
end sendFiles
```

`ends with` ignores case in AppleScript, so `Paper.PDF` counts. Files of other kinds are left out. Each file is its own upload: when one fails (a locked PDF, an EPUB with DRM), the others still go up and the dialog shows `put`'s report of the failure. `--to` names the folder even when its name looks like a file's; to tag the uploads, add `--tag work` to `whereTo`.

The first run asks to let Script Editor (or the Script menu, or Terminal for `osascript`) control the Finder.

## A Quick Action in Shortcuts or Automator

The same upload as a Quick Action appears in the Finder's context menu under Quick Actions, and works on the files right-clicked. The script gets the files as `input` and returns them.

```applescript
-- Quick Action: "Run AppleScript" with the Finder's files as input.

property rmctl : "PATH=/opt/homebrew/bin:/usr/local/bin:$PATH rmctl"
property destination : "/Inbox"
property confirm : "--yes"

on run {input, parameters}
	set docArgs to ""
	set noteArgs to ""
	set sent to 0
	repeat with f in input
		set p to POSIX path of f
		if p ends with ".pdf" or p ends with ".epub" then
			set docArgs to docArgs & " " & quoted form of p
			set sent to sent + 1
		else if p ends with ".md" or p ends with ".markdown" or p ends with ".txt" then
			set noteArgs to noteArgs & " " & quoted form of p
			set sent to sent + 1
		end if
	end repeat
	set whereTo to " --to " & quoted form of destination & " " & confirm
	try
		if docArgs is not "" then do shell script rmctl & " put" & docArgs & whereTo
		if noteArgs is not "" then do shell script rmctl & " put" & noteArgs & whereTo & " --notebook"
		display notification (sent as text) & " file(s) sent to " & destination with title "Send to reMarkable"
	on error errMsg number errNum
		display dialog errMsg buttons {"OK"} default button 1 with title "Send to reMarkable" with icon stop
	end try
	return input
end run
```

**Shortcuts.** Make a new shortcut named "Send to reMarkable"; in its details (the ⓘ button) turn on "Use as Quick Action" with "Finder" checked; set it to receive Files from Quick Actions; add the "Run AppleScript" action and replace its text with the script. Shortcuts runs scripts only once Settings › Advanced › "Allow Running Scripts" is on.

**Automator.** Make a new Quick Action; set "Workflow receives current" to "files or folders" in "Finder"; add "Run AppleScript" and replace its text with the script; save it as "Send to reMarkable". Automator keeps it in `~/Library/Services`.

## A hot folder

A Folder Action runs when files land in a Finder folder: this one uploads each PDF, EPUB or Markdown file dropped there, then moves it into a `Sent` folder inside, so what is left in the folder is what did not go up.

`~/Library/Scripts/Folder Action Scripts/reMarkable hot folder.scpt`:

```applescript
-- Folder Action: upload what is dropped into the folder, then move it into Sent.

property rmctl : "PATH=/opt/homebrew/bin:/usr/local/bin:$PATH rmctl"
property destination : "/Inbox"
property confirm : "--yes" -- "--dry-run" checks each file and leaves it in place

on adding folder items to thisFolder after receiving addedItems
	set sentFolder to POSIX path of thisFolder & "Sent"
	do shell script "mkdir -p " & quoted form of sentFolder
	repeat with f in addedItems
		set p to POSIX path of f
		set flags to missing value
		if p ends with ".pdf" or p ends with ".epub" then
			set flags to ""
		else if p ends with ".md" or p ends with ".markdown" or p ends with ".txt" then
			set flags to " --notebook"
		end if
		if flags is not missing value then
			try
				do shell script rmctl & " put " & quoted form of p & " --to " & quoted form of destination & flags & " " & confirm
				if confirm is "--yes" then do shell script "mv -n " & quoted form of p & " " & quoted form of sentFolder
			on error errMsg number errNum
				display notification errMsg with title "reMarkable hot folder" subtitle "Not sent (exit status " & errNum & ")"
			end try
		end if
	end repeat
end adding folder items to
```

To install it, save the script as a compiled script in `~/Library/Scripts/Folder Action Scripts/` (create the folder if needed). Then right-click the folder in the Finder, choose Services › Folder Actions Setup…, check "Enable Folder Actions", and attach "reMarkable hot folder.scpt" to the folder.

A file that fails stays in the folder and a notification says why; drop it again (or move it out and back) to retry. `mv -n` leaves a file in place rather than overwrite one of the same name in `Sent`. The action fires when an item appears, so a browser download in progress shows up under a temporary name (Safari's `.download`), which the script skips, and the finished file may not count as added. Download into another folder and move the file in. The same job as a shell script on `fswatch` or launchd's `WatchPaths` is in [Automation](automation.md#a-hot-folder).

Folder Actions run in the background (`Folder Actions Dispatcher`), which macOS may ask to allow to access the folder when it is in Desktop, Documents or Downloads.

## Render a document and open it in Preview

The script lists the library's documents, renders the one picked to a PDF with its handwriting, highlights and typed text, writes it to the Desktop under the document's name, and opens it in Preview.

`~/Library/Scripts/Render to PDF.scpt`:

```applescript
-- Pick a document of the reMarkable library, render it with its annotations
-- to a PDF on the Desktop and open it in Preview.

property rmctl : "PATH=/opt/homebrew/bin:/usr/local/bin:$PATH rmctl"

on run
	try
		set {thePaths, theIds} to listDocuments({"pdf", "annotated-pdf", "epub", "notebook"})
		set choice to choose from list thePaths with title "Render to PDF" with prompt "Which document?" OK button name "Render"
		if choice is false then return
		set theId to item (indexOf(item 1 of choice, thePaths)) of theIds
		set pdfPath to renderPDF(theId, POSIX path of (path to desktop folder))
		do shell script "open -a Preview " & quoted form of pdfPath
	on error errMsg number errNum
		if errNum is -128 then return -- Cancel
		display dialog errMsg buttons {"OK"} default button 1 with title "Render to PDF" with icon stop
	end try
end run

-- The paths and the ids of the documents of the given kinds, from
-- "find --format plain": path, kind, id, starred, trashed per line.
on listDocuments(kinds)
	set answer to do shell script rmctl & " find --format plain"
	set thePaths to {}
	set theIds to {}
	set saved to AppleScript's text item delimiters
	set AppleScript's text item delimiters to tab
	repeat with aLine in paragraphs of answer
		set fields to text items of aLine
		if (count of fields) ≥ 3 then
			if kinds contains (item 2 of fields) then
				set end of thePaths to item 1 of fields
				set end of theIds to item 3 of fields
			end if
		end if
	end repeat
	set AppleScript's text item delimiters to saved
	return {thePaths, theIds}
end listDocuments

on indexOf(x, theList)
	repeat with i from 1 to count of theList
		if item i of theList is x then return i
	end repeat
	error "Not in the list: " & x
end indexOf

-- Renders into folderPath under the document's own name; the first line of
-- the answer is "path<TAB>the file written".
on renderPDF(theId, folderPath)
	set answer to do shell script rmctl & " render " & quoted form of theId & " -o " & quoted form of folderPath
	set firstLine to paragraph 1 of answer
	return text ((offset of tab in firstLine) + 1) thru -1 of firstLine
end renderPDF
```

`find` with no pattern lists every item, one line each: path, kind (`folder`, `pdf`, `annotated-pdf`, `epub`, `notebook`), id, starred, in the trash. To list one folder, use `find --under /Books --format plain`; for the starred documents, `find --starred --format plain`.

`render -o` with a directory writes `<name>-rendered.pdf` inside it, and the first line of the answer is `path` and the file written, which the script hands to `open -a Preview`. Add `--paper letter` for notebooks on US Letter, or `--mode preserving` to append the marks to the original PDF untouched (see [Rendering](rendering.md)).

## A page on the clipboard

To paste a page of handwriting into Keynote, Pages, Slack or a mail, the script asks for a document and a page number, renders that page as an image and puts the image on the clipboard.

`~/Library/Scripts/Page to clipboard.scpt`:

```applescript
-- Render one page of a document as an image and put it on the clipboard,
-- ready to paste into Keynote, Pages, Slack or a mail.

property rmctl : "PATH=/opt/homebrew/bin:/usr/local/bin:$PATH rmctl"
property imageFormat : "png" -- or "jpg"
property scale : "1" -- pixels per tablet unit, as text: "2" doubles the size

on run
	try
		set {thePaths, theIds} to listDocuments({"pdf", "annotated-pdf", "epub", "notebook"})
		set choice to choose from list thePaths with title "Page to clipboard" with prompt "Which document?"
		if choice is false then return
		set theId to item (indexOf(item 1 of choice, thePaths)) of theIds
		set answer to display dialog "Which page?" default answer "1" with title "Page to clipboard"
		set pageNumber to (text returned of answer) as integer
		pageToClipboard(theId, pageNumber)
		display notification "Page " & pageNumber & " of " & item 1 of choice & " is on the clipboard." with title "Page to clipboard"
	on error errMsg number errNum
		if errNum is -128 then return
		display dialog errMsg buttons {"OK"} default button 1 with title "Page to clipboard" with icon stop
	end try
end run

on pageToClipboard(theId, pageNumber)
	set imageFile to POSIX path of (path to temporary items) & "rmctl-page." & imageFormat
	do shell script rmctl & " render " & quoted form of theId & " --page " & pageNumber & " --format " & imageFormat & " --scale " & scale & " -o " & quoted form of imageFile
	if imageFormat is "png" then
		set the clipboard to (read (POSIX file imageFile) as «class PNGf»)
	else
		set the clipboard to (read (POSIX file imageFile) as JPEG picture)
	end if
end pageToClipboard

on listDocuments(kinds)
	set answer to do shell script rmctl & " find --format plain"
	set thePaths to {}
	set theIds to {}
	set saved to AppleScript's text item delimiters
	set AppleScript's text item delimiters to tab
	repeat with aLine in paragraphs of answer
		set fields to text items of aLine
		if (count of fields) ≥ 3 then
			if kinds contains (item 2 of fields) then
				set end of thePaths to item 1 of fields
				set end of theIds to item 3 of fields
			end if
		end if
	end repeat
	set AppleScript's text item delimiters to saved
	return {thePaths, theIds}
end listDocuments

on indexOf(x, theList)
	repeat with i from 1 to count of theList
		if item i of theList is x then return i
	end repeat
	error "Not in the list: " & x
end indexOf
```

At scale 1 a page is 1404 pixels wide, as on the tablet's screen. `scale` is text so that a value such as `"1.5"` reaches the command as written, whatever the Mac's decimal separator. A page past the end fails with `render`'s message, for example `page 9 of 2: no such page`. For a JPEG, set `imageFormat` to `"jpg"` and, if needed, add `--quality 90` to the command.

## Highlights to the clipboard

The script lists the PDFs and EPUBs, and copies the highlights of the one picked as Markdown, one quote per highlight under a heading per page, ready to paste into a note.

`~/Library/Scripts/Highlights to clipboard.scpt`:

```applescript
-- Copy the highlights of a PDF or an EPUB to the clipboard as Markdown.

property rmctl : "PATH=/opt/homebrew/bin:/usr/local/bin:$PATH rmctl"

on run
	try
		set {thePaths, theIds} to listDocuments({"pdf", "annotated-pdf", "epub"})
		set choice to choose from list thePaths with title "Highlights to clipboard" with prompt "Whose highlights?"
		if choice is false then return
		set thePath to item 1 of choice
		set markdown to highlightsMarkdown(thePath, item (indexOf(thePath, thePaths)) of theIds)
		if markdown is "" then
			display notification thePath & " has no highlights." with title "Highlights to clipboard"
		else
			set the clipboard to markdown
			display notification "The highlights of " & thePath & " are on the clipboard." with title "Highlights to clipboard"
		end if
	on error errMsg number errNum
		if errNum is -128 then return
		display dialog errMsg buttons {"OK"} default button 1 with title "Highlights to clipboard" with icon stop
	end try
end run

-- "# name" and the highlights as quotes under "## Page N"; "" when there are none.
-- Line endings stay LF: do shell script turns them into CR otherwise.
on highlightsMarkdown(thePath, theId)
	set quotes to do shell script rmctl & " highlights " & quoted form of theId & " --format markdown" without altering line endings
	if quotes is "" then return ""
	set saved to AppleScript's text item delimiters
	set AppleScript's text item delimiters to "/"
	set theName to last text item of thePath
	set AppleScript's text item delimiters to saved
	return "# " & theName & linefeed & linefeed & quotes
end highlightsMarkdown

on listDocuments(kinds)
	set answer to do shell script rmctl & " find --format plain"
	set thePaths to {}
	set theIds to {}
	set saved to AppleScript's text item delimiters
	set AppleScript's text item delimiters to tab
	repeat with aLine in paragraphs of answer
		set fields to text items of aLine
		if (count of fields) ≥ 3 then
			if kinds contains (item 2 of fields) then
				set end of thePaths to item 1 of fields
				set end of theIds to item 3 of fields
			end if
		end if
	end repeat
	set AppleScript's text item delimiters to saved
	return {thePaths, theIds}
end listDocuments

on indexOf(x, theList)
	repeat with i from 1 to count of theList
		if item i of theList is x then return i
	end repeat
	error "Not in the list: " & x
end indexOf
```

What lands on the clipboard:

```markdown
# Contract — Signed

## Page 1

> uses

> uses for its own, and no less than reasonable care
```

The words are those the tablet copied out of the PDF's text layer; a highlight over a scanned page with no text layer reads `(no text)`. To write a file instead, replace `set the clipboard to markdown` with a `write` to a file, or run `rmctl highlights <id> --format markdown > notes.md` from a shell.

## A notebook into Apple Notes

The script lists the notebooks, reads the typed text of the one picked as Markdown, and makes a note of it in the default folder of Notes. Handwriting is ink, not text, and does not come along.

`~/Library/Scripts/Notebook to Notes.scpt`:

```applescript
-- Make an Apple Notes note of the typed text of a reMarkable notebook.

property rmctl : "PATH=/opt/homebrew/bin:/usr/local/bin:$PATH rmctl"

on run
	try
		set {thePaths, theIds} to listDocuments({"notebook"})
		set choice to choose from list thePaths with title "Notebook to Notes" with prompt "Which notebook?"
		if choice is false then return
		set thePath to item 1 of choice
		set html to noteHTML(thePath, item (indexOf(thePath, thePaths)) of theIds)
		tell application "Notes"
			set theNote to make new note at default folder of default account with properties {body:html}
			show theNote
			activate
		end tell
	on error errMsg number errNum
		if errNum is -128 then return
		display dialog errMsg buttons {"OK"} default button 1 with title "Notebook to Notes" with icon stop
	end try
end run

-- Notes keeps a note's body as HTML: the notebook's name as the title, then
-- one <div> per line of its typed text as Markdown. Headings and bold lines
-- keep their style; bullets, numbers and checkboxes stay as they are written.
on noteHTML(thePath, theId)
	set typed to do shell script rmctl & " text " & quoted form of theId & " --format markdown"
	set saved to AppleScript's text item delimiters
	set AppleScript's text item delimiters to "/"
	set theName to last text item of thePath
	set AppleScript's text item delimiters to saved
	set html to "<div><h1>" & escapeHTML(theName) & "</h1></div>"
	repeat with aLine in paragraphs of typed
		set t to contents of aLine
		if t is "" then
			set html to html & "<div><br></div>"
		else if t starts with "# " then
			set html to html & "<div><h1>" & escapeHTML(text 3 thru -1 of t) & "</h1></div>"
		else if t starts with "## " or t starts with "### " then
			set html to html & "<div><h2>" & escapeHTML(text ((offset of " " in t) + 1) thru -1 of t) & "</h2></div>"
		else if t starts with "**" and t ends with "**" and (length of t) > 4 then
			set html to html & "<div><b>" & escapeHTML(text 3 thru -3 of t) & "</b></div>"
		else
			set html to html & "<div>" & escapeHTML(t) & "</div>"
		end if
	end repeat
	return html
end noteHTML

on escapeHTML(t)
	repeat with pair in {{"&", "&amp;"}, {"<", "&lt;"}, {">", "&gt;"}}
		set saved to AppleScript's text item delimiters
		set AppleScript's text item delimiters to item 1 of pair
		set parts to text items of t
		set AppleScript's text item delimiters to item 2 of pair
		set t to parts as text
		set AppleScript's text item delimiters to saved
	end repeat
	return t
end escapeHTML

on listDocuments(kinds)
	set answer to do shell script rmctl & " find --format plain"
	set thePaths to {}
	set theIds to {}
	set saved to AppleScript's text item delimiters
	set AppleScript's text item delimiters to tab
	repeat with aLine in paragraphs of answer
		set fields to text items of aLine
		if (count of fields) ≥ 3 then
			if kinds contains (item 2 of fields) then
				set end of thePaths to item 1 of fields
				set end of theIds to item 3 of fields
			end if
		end if
	end repeat
	set AppleScript's text item delimiters to saved
	return {thePaths, theIds}
end listDocuments

on indexOf(x, theList)
	repeat with i from 1 to count of theList
		if item i of theList is x then return i
	end repeat
	error "Not in the list: " & x
end indexOf
```

A notebook with a heading, bold lines, a bulleted and a numbered list and a checkbox becomes a note titled with the notebook's name, then:

```text
Sprint 42 Retro            (a heading)
What went well             (bold)
- Launch checklist closed two days early
- Pricing review with finance
To improve                 (bold)
1. Fewer meetings on Fridays
2. Write the release notes as we go
- [ ] Book the venue for the Q4 kickoff
```

The first run asks to let Script Editor control Notes. With several accounts in Notes, `default account` is the one set in Notes › Settings; to choose a folder, write `at folder "reMarkable" of account "iCloud"` instead.

## PDF attachments from Mail

A Mail rule can run a script on the messages it matches: this one saves each PDF attachment, uploads it to `/Inbox` with the tag `mail`, and deletes the saved copy.

`~/Library/Application Scripts/com.apple.mail/PDFs to reMarkable.scpt`:

```applescript
-- Mail rule: send the PDF attachments of the matching messages to /Inbox.

property rmctl : "PATH=/opt/homebrew/bin:/usr/local/bin:$PATH rmctl"
property destination : "/Inbox"
property confirm : "--yes"

using terms from application "Mail"
	on perform mail action with messages theMessages for rule theRule
		-- Mail saves attachments where it may write: a folder of Downloads.
		set workFolder to POSIX path of (path to downloads folder) & "Mail to reMarkable/"
		do shell script "mkdir -p " & quoted form of workFolder
		tell application "Mail"
			repeat with theMessage in theMessages
				repeat with theAttachment in mail attachments of theMessage
					set theName to name of theAttachment
					if theName ends with ".pdf" or MIME type of theAttachment is "application/pdf" then
						set pdfPath to workFolder & theName
						if pdfPath does not end with ".pdf" then set pdfPath to pdfPath & ".pdf"
						try
							save theAttachment in (POSIX file pdfPath)
							my upload(pdfPath)
						on error errMsg number errNum
							tell current application to display notification errMsg with title "Mail to reMarkable" subtitle theName
						end try
					end if
				end repeat
			end repeat
		end tell
	end perform mail action with messages
end using terms from

on upload(pdfPath)
	try
		do shell script rmctl & " put " & quoted form of pdfPath & " --to " & quoted form of destination & " --tag mail " & confirm
		do shell script "rm -f " & quoted form of pdfPath
	on error errMsg number errNum
		do shell script "rm -f " & quoted form of pdfPath
		error errMsg number errNum
	end try
end upload
```

To install it, save the script as a compiled script in `~/Library/Application Scripts/com.apple.mail/` — Mail runs rule scripts only from there. In Mail › Settings › Rules, add a rule with the conditions you want (for example "From contains invoices@" and "Any Attachment Name ends with .pdf"), and the action "Run AppleScript" with the script. Mail asks whether to apply the rule to the messages already in the mailbox; answer "Don't Apply" unless you want them all uploaded.

An attachment Mail cannot save (one it has not downloaded, for instance), a locked PDF and a PDF that fails to upload are each reported in a notification, and the saved copy is deleted. The rule runs when Mail receives the message, so the Mac and Mail must be running.

## A daily backup

`backup` keeps one `.rmdoc` archive per document in a folder and on the next run downloads only what changed; this script runs it and reports the counts from the JSON answer in a notification. Run daily by launchd, it costs one request when nothing changed.

`~/Library/Scripts/reMarkable backup.scpt`:

```applescript
-- Back up the reMarkable library incrementally and say how it went.

use AppleScript version "2.4"
use framework "Foundation"
use scripting additions

property rmctl : "PATH=/opt/homebrew/bin:/usr/local/bin:$PATH rmctl"
property backupFolder : "" -- "" is ~/Backups/reMarkable
property libraryPath : "/"

on run
	set dest to backupFolder
	if dest is "" then set dest to POSIX path of (path to home folder) & "Backups/reMarkable"
	try
		set r to backUp(dest)
		display notification (|written| of r as text) & " written, " & (|skipped| of r as text) & " unchanged, " & (|moved| of r as text) & " moved to removed/" with title "reMarkable backup" subtitle (|documents| of r as text) & " documents in " & dest
	on error errMsg number errNum
		if errNum is 3 then set errMsg to "rmctl is not registered: run 'rmctl register <code>' in Terminal."
		display notification errMsg with title "reMarkable backup failed" subtitle "exit status " & errNum
		error errMsg number errNum -- so that launchd and the log see the failure
	end try
end run

-- The answer of "backup --json" as a record: documents, written, skipped,
-- moved, pruned and outside.
on backUp(dest)
	do shell script "mkdir -p " & quoted form of dest
	set answer to do shell script rmctl & " backup " & quoted form of dest & " " & quoted form of libraryPath & " --jobs 4 --json"
	return parseJSON(answer)
end backUp

on parseJSON(json)
	set theData to (current application's NSString's stringWithString:json)'s dataUsingEncoding:(current application's NSUTF8StringEncoding)
	set {theObject, theError} to current application's NSJSONSerialization's JSONObjectWithData:theData options:0 |error|:(reference)
	if theObject is missing value then error (theError's localizedDescription() as text)
	return item 1 of ((current application's NSArray's arrayWithObject:theObject) as list)
end parseJSON
```

The answer it reads, for a folder of five documents backed up twice:

```json
{"dir": "/Users/you/Backups/reMarkable", "documents": 5, "written": 0, "skipped": 5, "moved": 0, "pruned": 0, "outside": 0}
```

`parseJSON` turns a JSON object into a record whose labels are the keys, written between bars (`|written| of r`) since they are not AppleScript terms. Set `libraryPath` to back up one folder, and give each path its own `backupFolder`: a backup folder belongs to the path and the profile of its first backup. What happens to deleted documents, and how to restore, is in [Backups](backups.md).

**launchd.** `~/Library/LaunchAgents/io.altessa.rmctl.applescript-backup.plist` runs the script at 02:30, and at the next wake when the Mac slept through it; launchd expands neither `~` nor `$HOME`, so replace `/Users/you`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>io.altessa.rmctl.applescript-backup</string>
  <key>ProgramArguments</key>
  <array>
    <string>/usr/bin/osascript</string>
    <string>/Users/you/Library/Scripts/reMarkable backup.scpt</string>
  </array>
  <key>StartCalendarInterval</key>
  <dict>
    <key>Hour</key>
    <integer>2</integer>
    <key>Minute</key>
    <integer>30</integer>
  </dict>
  <key>StandardErrorPath</key>
  <string>/Users/you/Library/Logs/rmctl-backup.log</string>
</dict>
</plist>
```

```sh
launchctl bootstrap "gui/$(id -u)" ~/Library/LaunchAgents/io.altessa.rmctl.applescript-backup.plist
launchctl kickstart "gui/$(id -u)/io.altessa.rmctl.applescript-backup"     # run it once now
```

The script sets `PATH` itself, so the plist needs no `EnvironmentVariables`. A failure leaves its message in the log and makes `osascript` exit with status 1.

**Calendar.** Calendar no longer runs scripts from an alert, but it opens files: in Script Editor, choose File › Export…, File Format "Application", and save `reMarkable backup.app`. Make a repeating event, add the alert "Custom…" › "Open file", and pick the application. The first run of the application asks to let it show notifications.

## Library usage in a dialog

The script reads `du --json` and shows the library's size, its top-level folders largest first, and the documents by kind.

`~/Library/Scripts/reMarkable usage.scpt`:

```applescript
-- Show what the reMarkable library takes, folder by folder and by kind.

use AppleScript version "2.4"
use framework "Foundation"
use scripting additions

property rmctl : "PATH=/opt/homebrew/bin:/usr/local/bin:$PATH rmctl"

on run
	try
		display dialog usageReport("/") buttons {"OK"} default button 1 with title "reMarkable library"
	on error errMsg number errNum
		if errNum is -128 then return
		display dialog errMsg buttons {"OK"} default button 1 with title "reMarkable library" with icon stop
	end try
end run

on usageReport(thePath)
	set r to parseJSON(do shell script rmctl & " du " & quoted form of thePath & " --json")
	set report to |path| of r & " — " & formatBytes(|bytes| of r) & " in " & |documents| of r & " documents and " & |folders| of r & " folders" & return & return
	repeat with f in |subfolders| of r
		set report to report & |path| of f & tab & formatBytes(|bytes| of f) & tab & |documents| of f & " documents" & return
	end repeat
	set report to report & return
	repeat with k in |by_kind| of r
		set report to report & |kind| of k & tab & formatBytes(|bytes| of k) & tab & |documents| of k & " documents" & return
	end repeat
	return report
end usageReport

on formatBytes(n)
	return (current application's NSByteCountFormatter's stringFromByteCount:n countStyle:(current application's NSByteCountFormatterCountStyleFile)) as text
end formatBytes

on parseJSON(json)
	set theData to (current application's NSString's stringWithString:json)'s dataUsingEncoding:(current application's NSUTF8StringEncoding)
	set {theObject, theError} to current application's NSJSONSerialization's JSONObjectWithData:theData options:0 |error|:(reference)
	if theObject is missing value then error (theError's localizedDescription() as text)
	return item 1 of ((current application's NSArray's arrayWithObject:theObject) as list)
end parseJSON
```

The dialog reads, for the test library (columns aligned here; the dialog separates them with tabs):

```text
/ — 8.2 MB in 41 documents and 15 folders

/Inbox      2.7 MB   5 documents
/Projects   423 KB   9 documents
/Reading    397 KB   4 documents
…

pdf             4.2 MB   33 documents
annotated-pdf   3.5 MB    4 documents
notebook        299 KB    2 documents
epub            233 KB    2 documents
```

`du` reads only the indexes, so it answers from the cache, and with `--offline` (add it to the command) without the network. `NSByteCountFormatter` writes sizes the way the Finder does, in the Mac's language. For one folder, replace `"/"` with its path; `--depth 2` adds the level below.

The same in JavaScript for Automation, where `JSON.parse` replaces the AppleScriptObjC handler. `~/Library/Scripts/reMarkable usage.js` runs from the Script menu, or with `osascript -l JavaScript "reMarkable usage.js" /Books` for one folder:

```javascript
// Show what the reMarkable library takes, in JavaScript for Automation.
const rmctl = 'PATH=/opt/homebrew/bin:/usr/local/bin:$PATH rmctl'

const app = Application.currentApplication()
app.includeStandardAdditions = true

function usageReport(path) {
  const quoted = "'" + path.replace(/'/g, "'\\''") + "'"
  const du = JSON.parse(app.doShellScript(`${rmctl} du ${quoted} --json`))
  const mb = (n) => (n / 1e6).toFixed(1) + ' MB'
  const lines = [`${du.path} — ${mb(du.bytes)} in ${du.documents} documents and ${du.folders} folders`, '']
  for (const f of du.subfolders || []) lines.push(`${f.path}\t${mb(f.bytes)}\t${f.documents} documents`)
  lines.push('')
  for (const k of du.by_kind || []) lines.push(`${k.kind}\t${mb(k.bytes)}\t${k.documents} documents`)
  return lines.join('\n')
}

function run(argv) {
  try {
    app.displayDialog(usageReport(argv[0] || '/'), { withTitle: 'reMarkable library', buttons: ['OK'], defaultButton: 'OK' })
  } catch (e) {
    if (e.errorNumber === -128) return
    app.displayDialog(e.message, { withTitle: 'reMarkable library', buttons: ['OK'], defaultButton: 'OK', withIcon: 'stop' })
  }
}
```

JXA has no `quoted form of`; the `quoted` line does the same, wrapping the path in single quotes.

## Permissions

macOS asks once per application, and remembers the answer in System Settings › Privacy & Security. The application is the one running the script: Script Editor, the Script menu (`osascript`), Terminal, Shortcuts, Automator's Quick Action host, Folder Actions Dispatcher, Mail, or an exported application.

| Prompt | When | Where to change it |
|---|---|---|
| "… wants to control Finder" | the Finder selection script | Automation |
| "… wants to control Notes" | the Notes script | Automation |
| "… would like to access files in your Desktop / Documents / Downloads folder" | rendering to the Desktop, a hot folder or files there, Mail's work folder in Downloads | Files and Folders |
| Notifications | the first `display notification` from an application | Notifications |
| "Allow Running Scripts" | a shortcut with "Run AppleScript" | Shortcuts › Settings › Advanced |

`rmctl` itself, run by `do shell script`, needs nothing more: it reads its token from its profile's file, or from the login Keychain, which a script or a launch agent can read once the Mac has been unlocked after boot (see [Configuration](configuration.md)). A script run by launchd before anyone logs in has no Keychain and no notifications.
