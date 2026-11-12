# Pages and archives

Back to the [README](../README.md).

## Pages

| What | Behavior |
|---|---|
| Typed text | The paragraphs of a notebook's pages, in the document's order, with their styles: headings, bold, bullets, numbered items, checkboxes. |
| Highlights | The marker over a PDF or EPUB, with the words the tablet copied out of the text layer, the page, and the color. |
| Highlight notes | `highlights --format note` writes a document's highlights and typed text as a Markdown note for Obsidian, Logseq or a Readwise-style vault; a folder writes one note per document into a directory, keeping the tree, incrementally. See [Highlight notes](#highlight-notes). |
| Page diagnostics | Per page: strokes by tool and color, paragraphs, highlights, images, and the blocks the reader stepped over. |
| Search by text | `find` also looks into the typed text and the highlighted passages of documents that have pages; the other filters run first. |
| Unmark | Erases every highlight of a document, or of the pages named, the way the tablet's eraser does: each mark becomes a tombstone, every other byte stays, and the edited page is read back before it is stored. One commit. |
| Notebook from text | A `.md`, `.markdown` or `.txt` file becomes a notebook of typed text the tablet opens as its own: `# heading` is a heading, deeper headings and wholly bold lines are bold, bullets and numbered items are list paragraphs, a rule is a dash line; tables and pictures are refused, naming their line. Pages are laid out for the device (rm2, pure, pro, move) and paginated by estimate, at most 2000. |

The page commands read and write the `.rm` page format through [rmlines-go](https://github.com/altessa-s/rmlines-go), which compiles a C23 library through cgo: they need clang 19+ or GCC 13+ (`CC=clang-19 go install …` when the default compiler is older). Built with `CGO_ENABLED=0`, rmctl keeps every other command and answers the page commands with "built without cgo". The released binaries are built with cgo on each platform's own runner. Rendering has its own needs; see [Rendering](rendering.md).

## Highlight notes

`rmctl highlights` prints a document's highlights as text, Markdown quotes or JSON. `--format note` writes them as a note for a notes app instead: the same document always gives the same bytes, so a note written again changes only where the document did, and a vault under git or Obsidian Sync sees a small diff.

```console
$ rmctl highlights "/Books/Deep Work.epub" --format note
```

```markdown
---
title: "Deep Work"
remarkable-id: "3f2a8c1e-5b7d-4e2f-9a61-0c4d2e8b7f13"
remarkable-path: "/Books/Deep Work.epub"
type: epub
modified: 2026-03-04T10:20:30Z
pages: 312
highlights: 3
tags:
  - "focus"
  - "to-read"
---

# Deep Work

## Highlights

### Page 12

> Deep work is the ability to focus without distraction
> on a cognitively demanding task.
>
> — p. 12 #highlight/yellow

> (no text)
>
> — p. 12

### Page 40

> Clarity about what matters provides clarity about what does not.
>
> — p. 40 #highlight/green

## Typed text

### Page 40

#### Why it matters

- schedule every minute
```

| Part | What it holds |
|---|---|
| Front matter | YAML, its keys always in this order: `title` (the document's name without a `.pdf` or `.epub` it ends with), `remarkable-id`, `remarkable-path` (left out for a document of a local `.rmdoc`), `type` (`pdf`, `epub` or `notebook`), `modified` (the document's last change, UTC, to the second; left out when the document has none), `pages`, `highlights`, and `tags`, the document's tags as a notes app takes them — white space becomes `-`, a `#` in front goes, sorted and without repeats; left out when there are none. Strings are double-quoted, so a name with a colon or a quote stays one value. `title` and `tags` are the keys Obsidian, Logseq and the Readwise exports into them read; the others are for queries, such as a Dataview table of the documents read this month. The rmctl version is left out on purpose: an upgrade would otherwise change every note. |
| Title | `# <title>`, as the Readwise exports open a note. |
| Highlights | `## Highlights`, a `### Page N` for each page that has any, in the document's order, and each highlight as a quote, top of the page first, then from the left; the words as the tablet copied them out of the text layer, `(no text)` over a page without one. Its last line is the page — `— p. 12` — and the color as a tag, `#highlight/yellow`, which Obsidian and Logseq list and search; a highlight of no known color has no tag. Left out when there are none. |
| Typed text | `## Typed text`, a `### Page N` for each page with typed text, its paragraphs as `text --format markdown` writes them, a typed heading as `####` so it stays below the page. Left out when there is none. |

`--pdf-link <pattern>` makes each page reference a link, `[p. 12](<target>)`, to the page of a PDF kept beside the notes, such as the one `rmctl render` draws of the same folder. `{root}` is the way up from the note's directory to the top of the notes (empty, or `../` repeated), `{path}` the note's path below that top without `.md`, `{name}` its last part, `{id}` the document's id and `{page}` the page; any other `{…}` is refused. The note's path and the PDF's share their names — the same safe names and the same collision suffixes, `.md` for `.pdf` — so `{path}` names both. Obsidian opens `Doc.pdf#page=12` at that page in its own viewer. A note printed on its own has its file name as `{path}` and an empty `{root}`.

A folder, or `/` for the whole library, writes the note of every document below it into a directory, keeping the folder tree, as `render` lays out its PDFs:

| What | Behavior |
|---|---|
| Layout | `-o` names the directory, created when missing; without it, a directory named after the folder, or `remarkable` for `/`, in the working directory. Each document becomes `<folder path>/<name>.md`, its name made safe for a file (`/`, `\`, `:` and control characters become `-`) and a `.pdf` or `.epub` it ends with dropped; folders are directories under their safe names, and only those that receive a note are created. Two names that collide in one directory, compared without case, are told apart: the first by creation time, then by id, keeps its name, and the later ones get ` (<first 8 characters of the id>)` before `.md`. Documents in the trash are left out unless `--all`. `--page` and a `--format` other than `note` are refused. |
| Which documents | A document with highlights gets a note. One without gets none (`skipped`), unless `--all-docs`, which writes every document's note, or unless the record shows it had one: a note follows its document, so a document whose highlights were all erased gets a note without them rather than keeping stale ones. A document that has no page file at all, such as a PDF never opened on the tablet, is skipped without downloading anything. |
| Record | The directory keeps `.rmctl-notes.json`, written whole at the end of a run (and before any note is written when a record no longer holds), which records each note's document id, the document's modification time and the options it was written with (`--pdf-link`, `--all-docs`, and the layout of the note itself), and the documents that had nothing to write. A record that is missing or unreadable is no error: every document is read, and the record written anew. |
| `--incremental` | Skips a document whose note is there with the document's modification time, compared to the second, and whose record names the same document, modified at the same time, written with the same options; every note written takes the document's time. A document the record shows had nothing to write is skipped without reading it while it stays as it was. Another `--pdf-link` or `--all-docs`, or a newer note layout, writes every note again. A note is rmctl's: one edited by hand no longer has the document's time, and is written again; keep your own thoughts in a note of your own that links to it. |
| Documents gone | The notes the record lists whose document is no longer below the folder — deleted, put in the trash, moved out, or renamed (its note then has a new name) — are left where they are, and the answer counts them as `left`, with a note on standard error. `--prune` moves them into `.rmctl-removed/` in the directory, at the same path there, and drops them from the record; the leading dot keeps them out of Obsidian's and Logseq's views, like their own trash. Nothing is ever deleted, and a file the record does not list is never touched. Unlike `backup --prune`, which deletes, the worst `--prune` does is move a note aside. |
| Answer | A row per document — `written`, `unchanged`, `skipped` or `failed`, the note's path, the document's path, the highlights of a note written, the error — then the totals: `written`, `unchanged`, `skipped`, `failed`, `moved` and `left`. `--json` adds the paths moved and left. `--jobs N` (`-j`, 4 by default) reads N documents at a time. A document that fails is reported and the others go on; the command fails once the answer is written if any did. `-o`, `--all`, `--all-docs`, `--incremental`, `--prune` and `--jobs` are for a folder alone. |

Notes need the page format library, not the renderer: they are written by rmctl-lite as by rmctl, and offline from the cache once `sync` has fetched the pages.

**Readwise.** rmctl calls no service but the reMarkable cloud. The note keeps the shape of the notes Readwise exports into Obsidian and Logseq — `title` and `tags` in the front matter, `# Title`, `## Highlights`, each highlight with its location — so they sit beside Readwise's own and the templates and queries written for those read them too. To bring them into Readwise itself, use one of Readwise's own importers; rmctl sends them nowhere.

## Archives

The `.rmdoc` container goes through [rmdoc-go](https://github.com/altessa-s/rmdoc-go), cgo like the page format.

| What | Behavior |
|---|---|
| Export | `export` packs documents into one archive stored the way the tablet writes one: each document's files under their record names, in index order. A folder, or `/`, stands for every document below it that is not in the trash on its own; a document named twice is packed once. Every name is checked before anything is downloaded. The tablet imports one document per archive. |
| Import | `put file.rmdoc [folder]` adds every document of an archive with its pages, annotations and tags, in one commit; it mixes with PDFs and EPUBs in one `put`. A document whose identifier the library already holds takes a fresh identifier and its files are renamed to it, so the library's document is never touched: its row of the answer names the identifier it had under `renamed_from`. The `.metadata` is filed under the folder and taken out of the trash; nothing else in it changes. `--dry-run` only opens the archive and lists what it would import. |
| Offline | `rmdoc <file>` lists an archive's documents, kinds and files, names masked unless `--raw`. `text` and `highlights` read a local archive's pages without the cloud: an argument ending with `.rmdoc` that is a local file is read from it, every document in turn (in JSON, always an array). `render` draws a document of a local archive the same way, `--id` picking one of several. |
| Limits | An entry is read only up to 1 GiB, an archive up to 4 GiB; rmdoc-go's own bounds hold the entry count, the names and the expansion ratio. |
