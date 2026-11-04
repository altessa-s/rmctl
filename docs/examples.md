# Examples

Back to the [README](../README.md).

The answers below are from a test account, in the `table` format a terminal gets. Complete scripts for backups, hot folders, Markdown exports, rendering and reports are in [automation.md](automation.md).

## Browsing

```console
$ rmctl ls -l "/Projects/Q4 Product Launch"
NAME                        KIND           MODIFIED              SIZE  ID
──────────────────────────  ─────────────  ────────────────  ────────  ────────────────────────────────────
Go-to-Market Plan           pdf            2026-10-07 13:36  48.8 KiB  85b92954-fbc4-417b-a010-ea209769d9fb
Kickoff Notes               annotated-pdf  2026-10-07 13:37  32.4 KiB  8ae19d25-9d5d-4c00-99c0-06c3fb589be3
Kickoff Notes — Transcript  pdf            2026-09-15 15:30  86.3 KiB  bb5939b7-b685-4479-893c-fd1fea45d585
Launch Checklist            pdf            2026-09-15 15:30  25.0 KiB  bdc06cc6-1723-483a-89d7-3a54f776b800
Pricing Strategy v3         pdf            2026-09-15 15:30  64.3 KiB  24fdd1f5-5c13-4b4d-8186-3f56a2b6684b

$ rmctl tree /Clients
Brightline Studio/        folder         a6938f2f-5780-44c6-82e2-f73063026342
├── Proposal Draft        pdf            5b92466c-c695-4779-8850-360125fadcdc
└── Workshop Whiteboard   pdf            1e1654ee-cc58-4281-876e-97744c029e97
Harbor Logistics/         folder         e92b58ac-7d59-4e89-bd36-b0779c456af8
├── Contract — Signed     annotated-pdf  ef0f6df3-cd5c-473e-9a2c-b50e4907cf37
├── Discovery Call Notes  pdf            187c198e-7c64-47f3-a4f8-df6321e79ffe
└── Statement of Work     pdf            ef6d4879-454d-46f7-80d2-fd1c059228a9

$ rmctl stat 8ae1
id        8ae19d25-9d5d-4c00-99c0-06c3fb589be3
name      Kickoff Notes
path      /Projects/Q4 Product Launch/Kickoff Notes
kind      annotated-pdf
size      32.4 KiB
modified  2026-10-07 13:37
created   2026-09-15 15:30
trashed   no
pinned    no
tags      -
hash      3b5b3a3d8d6f9595c388a477118b3c8d73daddb96902f44c5da89a5dbac80c0f

FILE                                                                              SIZE  HASH
────────────────────────────────────────────────────────────────────────────  ────────  ────────────────────────────────────────────────────────────────
8ae19d25-9d5d-4c00-99c0-06c3fb589be3.content                                   1.5 KiB  c426df7e9204a25297356df08ac828b48a743fb5bc93afbc462e0aeca1550aeb
8ae19d25-9d5d-4c00-99c0-06c3fb589be3.metadata                                    388 B  ca8fdc5c224828f587440ff4b555a0a4789170bf11f356bf382abf6a84ebd96a
8ae19d25-9d5d-4c00-99c0-06c3fb589be3.pagedata                                     12 B  c4dba5fe446a2047940c869b5b075528207bff65f0cf688c9b37bcc32fdd430d
8ae19d25-9d5d-4c00-99c0-06c3fb589be3.pdf                                      29.9 KiB  ea893b87acf342f1f1631c1d13f7aa889262ef9afac9cf7e35361b366046e8a4
8ae19d25-9d5d-4c00-99c0-06c3fb589be3/163feae4-ab0b-4798-9571-b3551c6b43ee.rm     631 B  587416f0c2df11d1053d277e04c68a9634753ca8a699fdc5286a7031b60f3322
```

## Finding and measuring

```console
$ rmctl find '(?i)launch|kickoff'
PATH                                                    KIND           ID
──────────────────────────────────────────────────────  ─────────────  ────────────────────────────────────
/Projects/Q4 Product Launch/                            folder         ee7b2e70-148c-40c1-bc76-8ca498becaab
/Projects/Q4 Product Launch/Go-to-Market Plan           pdf            85b92954-fbc4-417b-a010-ea209769d9fb
/Projects/Q4 Product Launch/Kickoff Notes               annotated-pdf  8ae19d25-9d5d-4c00-99c0-06c3fb589be3
/Projects/Q4 Product Launch/Kickoff Notes — Transcript  pdf            bb5939b7-b685-4479-893c-fd1fea45d585
/Projects/Q4 Product Launch/Launch Checklist            pdf            bdc06cc6-1723-483a-89d7-3a54f776b800
/Projects/Q4 Product Launch/Pricing Strategy v3         pdf            24fdd1f5-5c13-4b4d-8186-3f56a2b6684b

$ rmctl du
path       /
bytes      7.8 MiB
documents  40
folders    15

PATH             SIZE  DOCUMENTS  FOLDERS
──────────  ─────────  ─────────  ───────
/Inbox/       2.6 MiB          5        0
/Projects/  413.0 KiB          9        2
/Reading/   388.2 KiB          4        2
/Clients/   234.5 KiB          5        2
/Meetings/  158.8 KiB          4        0
/Finance/   131.2 KiB          3        0
/01_Inbox/   95.8 KiB          2        0
/Strategy/   94.6 KiB          3        0
/Personal/   68.2 KiB          2        0

KIND           DOCUMENTS       SIZE
─────────────  ─────────  ─────────
pdf                   33    4.0 MiB
annotated-pdf          4    3.3 MiB
notebook               1  289.8 KiB
epub                   2  227.2 KiB
```

## A notebook from Markdown, and its text

`retro.md`:

```markdown
# Sprint 42 Retro

**What went well**

- Launch checklist closed two days early
- Pricing review with finance

**To improve**

1. Fewer meetings on Fridays
2. Write the release notes as we go
```

```console
$ rmctl put retro.md /Meetings --notebook --device pro --yes
ACTION         NAME                            KIND           ID                                    RENAMED_FROM
─────────────  ──────────────────────────────  ─────────────  ────────────────────────────────────  ────────────────────────────────────
made           Retro                           notebook       019a539e-a027-4b1e-99b3-3a20bdc8b537  -

$ rmctl text /Meetings/Retro --format markdown
# Sprint 42 Retro

**What went well**

- Launch checklist closed two days early
- Pricing review with finance

**To improve**

1. Fewer meetings on Fridays
2. Write the release notes as we go

$ rmctl find --text 'release notes'
PATH             KIND      ID
───────────────  ────────  ────────────────────────────────────
/Meetings/Retro  notebook  019a539e-a027-4b1e-99b3-3a20bdc8b537

$ rmctl highlights "/Clients/Harbor Logistics/Contract — Signed"
p.1  pink  uses
p.1  pink  uses for its own, and no less than reasonable care
```

## Rendering

```console
$ rmctl render "/Clients/Harbor Logistics/Contract — Signed"
path            Contract — Signed-rendered.pdf
pages           2
skipped blocks  0
refused pages   0
links dropped   0

$ rmctl render /Meetings/Retro --page 1 -o retro.jpg --quality 85 --no-template
path            retro.jpg
pages           1
skipped blocks  0
refused pages   0
links dropped   0

$ rmctl render /Notebook --page 1 -o sketch.svg
path            sketch.svg
pages           1
skipped blocks  0
refused pages   0
links dropped   0
```

## For scripts

`plain` is one TAB-separated line per record; `--json` is the stable interface.

```console
$ rmctl ls /Strategy --format plain
2027 OKRs — Draft	pdf	02b64e01-a359-470a-9774-6afdd3558f49	false	false
Market Expansion Brainstorm	pdf	bf6820e4-4cbf-4515-a1ff-c221921cb64f	false	false
Quarterly Review	pdf	6a6bb31a-eaa8-4605-af35-1c2c7ba3358c	false	false

$ rmctl text /Meetings/Retro --format json | jq -r '.pages[].paragraphs[] | select(.style == "numbered") | .text'
Fewer meetings on Fridays
Write the release notes as we go

$ rmctl stat /Meetings/Retro --json | jq '{kind, size, files: [.files[].name]}'
{
  "kind": "notebook",
  "size": 2135,
  "files": [
    "019a539e-a027-4b1e-99b3-3a20bdc8b537/9534d7fd-ca31-43b3-80c4-0be3e75b4f93.rm",
    "019a539e-a027-4b1e-99b3-3a20bdc8b537.pagedata",
    "019a539e-a027-4b1e-99b3-3a20bdc8b537.content",
    "019a539e-a027-4b1e-99b3-3a20bdc8b537.metadata"
  ]
}
```
