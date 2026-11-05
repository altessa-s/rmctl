# Space, duplicates and edits in place

Back to the [README](../README.md).

## Space and duplicates

- **`du [path] [--depth N] [--all]`** adds up what the documents below a folder (the top level by default) take — the bytes of every file their indexes list, which is what the cloud keeps — and reports the total, each folder down to `--depth` levels (1; 0 is the total alone) with its documents and folders, largest first, and the documents by kind. `--all` counts the trash.
- **`dupes [path] [--all]`** groups the documents whose original — the PDF or EPUB they were made from — is the same file by its hash, one row per document with its path and id, the groups that waste the most first, then the number of groups and the bytes the extra copies take. Annotations are not compared, and notebooks, which have no original, are never listed.

Both read only the library's indexes, so they answer from the cache, `--offline` too.

## Editing in place

`rename`, `pin`, `unpin` and `tag add|rm` change a document or folder without uploading it again: they write only the `.metadata` (`visibleName`, `pinned`, and `version` and `lastModified` marked edited) and, for tags, the `.content`'s `tags`; every other field, known or not, stays as it is, and every other file keeps its hash. `rename` keeps the item in its folder (`mv` moves it); a name cannot hold a slash. Tags are given with `--tag`, repeated, after the items, so a tag never reads as a path; folders take no tags, since the tablet tags documents and their pages and a folder's `.content` holds none. Asking for what already is — a tag that is there, a star on a starred item — is reported as `unchanged` and commits nothing. Each command is one commit, replayed on a conflict, and asks first unless `--yes`.
