# Uploads

Back to the [README](../README.md).

`put` validates before it uploads: a PDF or EPUB by its extension, at most 500 MiB, not empty, a PDF that opens and has pages, an EPUB that is a ZIP without DRM. The cover is the page last read unless the first page is asked for. A dry run validates the file and resolves the folder and uploads nothing. Replacing the document of the same name in the folder keeps its identifier, its name and its tags; its pages are new, so its annotations do not survive, and rmctl says so before it replaces anything.

**`--current-page N`** (a PDF, counted from 1) is the page the document opens on: it is recorded as the `.metadata`'s `lastOpenedPage` (N-1), and with `--content-only` on a document whose `.content` keeps its pages in `cPages`, as `cPages.lastOpened` too. A page beyond the PDF is refused. EPUBs are paginated by the tablet, so they take no page number.

**`--cover first|last`** maps to the `.content`'s `coverPageNumber`: `first` is 0, the first page; `last` is -1, the page last read, the tablet's default. The tablet knows no third state, so there is no `--cover none`.

**`--content-only`** replaces only the PDF of the PDF document of the same name in the folder (the one `--replace` would pick) and keeps everything else: its identifier, name, folder, tags, pin, cover and reading settings, every page's annotations (`<id>/<page>.rm`, per-page metadata, old-style highlights), and every `.content` and `.metadata` field rmctl does not have to change. It changes the PDF, the `.content`'s page list (`pages` and `redirectionPageMap`, or `cPages.pages` and `cPages.original`), `pageCount`, `originalPageCount` and `sizeInBytes`, the `.pagedata` (one template line per page), and the `.metadata`'s `lastModified` and `version`; cached thumbnails are dropped for the tablet to draw again. Every kept page also takes a new page identifier, with its annotation files filed under it unchanged, because the tablet keeps page previews by identifier and would otherwise go on showing the old PDF; the plan's `id_map` lists old → new. Annotations hang on page identifiers, which map to the PDF's pages:

- **The same number of pages, or more:** every page keeps its place and its annotations (under its new identifier); each extra page of the new PDF gets a new page, appended. Pages added on the tablet with no PDF page under them stay where they are.
- **Fewer pages:** the pages whose PDF page is gone are dropped. If any of them carries annotations, the upload is refused (exit 1), naming each such page and its annotation files; `--drop-annotations` drops them deliberately. Without annotations they simply go.
- **No document of that name, or one that is not a PDF:** refused. With nothing to keep, `--content-only` is most likely a mistake; upload without it.

It takes one PDF, asks first ("Replace the PDF of /Books/Paper, keeping its annotations on N pages?"), and is one commit like any write. `--dry-run` prints the plan — the old and new page counts, the pages kept, appended and dropped, and the annotated pages affected — and the JSON answer carries it as `content_plan`. It excludes `--replace`, `--notebook`, `--tag` and `--cover`.
