# Pages and archives

Back to the [README](../README.md).

## Pages

| What | Behavior |
|---|---|
| Typed text | The paragraphs of a notebook's pages, in the document's order, with their styles: headings, bold, bullets, numbered items, checkboxes. |
| Highlights | The marker over a PDF or EPUB, with the words the tablet copied out of the text layer, the page, and the color. |
| Page diagnostics | Per page: strokes by tool and color, paragraphs, highlights, images, and the blocks the reader stepped over. |
| Search by text | `find` also looks into the typed text and the highlighted passages of documents that have pages; the other filters run first. |
| Unmark | Erases every highlight of a document, or of the pages named, the way the tablet's eraser does: each mark becomes a tombstone, every other byte stays, and the edited page is read back before it is stored. One commit. |
| Notebook from text | A `.md`, `.markdown` or `.txt` file becomes a notebook of typed text the tablet opens as its own: `# heading` is a heading, deeper headings and wholly bold lines are bold, bullets and numbered items are list paragraphs, a rule is a dash line; tables and pictures are refused, naming their line. Pages are laid out for the device (rm2, pure, pro, move) and paginated by estimate, at most 2000. |

The page commands read and write the `.rm` page format through [rmlines-go](https://github.com/altessa-s/rmlines-go), which compiles a C23 library through cgo: they need clang 19+ or GCC 13+ (`CC=clang-19 go install …` when the default compiler is older). Built with `CGO_ENABLED=0`, rmctl keeps every other command and answers the page commands with "built without cgo". The released binaries are built with cgo on each platform's own runner. Rendering has its own needs; see [Rendering](rendering.md).

## Archives

The `.rmdoc` container goes through [rmdoc-go](https://github.com/altessa-s/rmdoc-go), cgo like the page format.

| What | Behavior |
|---|---|
| Export | `export` packs documents into one archive stored the way the tablet writes one: each document's files under their record names, in index order. A folder, or `/`, stands for every document below it that is not in the trash on its own; a document named twice is packed once. Every name is checked before anything is downloaded. The tablet imports one document per archive. |
| Import | `put file.rmdoc [folder]` adds every document of an archive with its pages, annotations and tags, in one commit; it mixes with PDFs and EPUBs in one `put`. A document whose identifier the library already holds takes a fresh identifier and its files are renamed to it, so the library's document is never touched: its row of the answer names the identifier it had under `renamed_from`. The `.metadata` is filed under the folder and taken out of the trash; nothing else in it changes. `--dry-run` only opens the archive and lists what it would import. |
| Offline | `rmdoc <file>` lists an archive's documents, kinds and files, names masked unless `--raw`. `text` and `highlights` read a local archive's pages without the cloud: an argument ending with `.rmdoc` that is a local file is read from it, every document in turn (in JSON, always an array). `render` draws a document of a local archive the same way, `--id` picking one of several. |
| Limits | An entry is read only up to 1 GiB, an archive up to 4 GiB; rmdoc-go's own bounds hold the entry count, the names and the expansion ratio. |
