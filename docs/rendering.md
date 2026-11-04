# Rendering

Back to the [README](../README.md).

`rmctl render <path|id|file.rmdoc>` draws a document the way the tablet shows it — handwriting with its pen textures, typed text, highlights, images and page templates, over the pages of the PDF or EPUB it was made from — through [rmrender-go](https://github.com/altessa-s/rmrender-go), with the fonts and templates it bundles. A cloud document is downloaded and packed as `export` packs it, then drawn; an argument ending with `.rmdoc` that is a local file, such as one `export` or `backup` wrote, is drawn from that archive without the cloud (`./` in front, or the identifier, names a cloud document whose name ends with `.rmdoc`).

| What | Behavior |
|---|---|
| PDF | The whole document, its pages in the document's order. `--mode` matters only for a document made from a PDF: `fresh` (the default) draws a new PDF, keeping links and the outline but not forms or other annotations; `preserving` appends the marks to the original PDF as an incremental update, its bytes untouched, and refuses a document whose pages were moved, added or hidden; `recomposed` writes a new PDF from the original's objects, keeping forms and annotations, and breaks signatures. |
| Page | `--page N` draws page N, from 1, as an image: `--format png` (the default for a page), `jpg` (or `jpeg`) or `svg`; without `--format`, an `-o` ending with `.pdf`, `.png`, `.jpg`, `.jpeg` or `.svg` decides. A PNG or a JPEG is drawn on white; an SVG has no background. `--scale` is a PNG's or a JPEG's pixels per tablet unit: at 1, the default, a page is 1404 pixels wide, as on the tablet's screen. `--quality` is a JPEG's, 1 to 100; 0, the default, is the renderer's, 75. |
| Templates | `--no-template` draws the pages without their templates, in any format. |
| Paper | `--paper a4\|letter\|tablet` (A4 by default) is the sheet a notebook page is laid out on; a page of a PDF keeps its own size. |
| Output | Without `-o`, `<name>-rendered.pdf`, or `<name>-p<N>.png`, `.jpg` or `.svg` for a page, in the working directory, so it never takes the place of the document's own PDF; a directory takes that name inside it. The file appears only once it is whole. The answer is the path, the pages drawn and what could not be kept: blocks of pages that did not parse, pages naming a page the PDF does not have, links a fresh PDF could not carry over; each also noted on standard error. |

Rendering is in the macOS and Linux builds:

- **macOS** draws with CoreGraphics and CoreText; nothing else is needed.
- **Linux** draws with Cairo, Pango and PDFium. Cairo, Pango, Fontconfig, HarfBuzz and GdkPixbuf come from the system: the binary needs them to start (on Debian or Ubuntu, `libcairo2`, `libpango-1.0-0`, `libpangocairo-1.0-0`, `libfontconfig1`, `libharfbuzz0b` and `libgdk-pixbuf-2.0-0`). PDFium is not packaged by distributions: the release archive carries `libpdfium.so` beside `rmctl`, which finds it in its own directory through its `$ORIGIN` run path, so keep the two together; the Homebrew formula installs both. The licences of PDFium and of the bundled fonts are under `licenses/` in the archive.
- **Windows** has the command, which says rendering is not available in this build.

Built from source, rendering needs a C23 compiler that reads rmrender-go's sources (clang 19+, GCC 15+ or a current Apple clang) and, on Linux, the Cairo stack's development packages and PDFium, which `.github/scripts/setup-render-linux.sh` sets up the way CI does; see rmrender-go's README. Without cgo, or on another system, `render` says it is not available. The bundled fonts add about 27 MB to the binary.
