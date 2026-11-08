# 01 — History and Original Architecture

This doc is context for the reimplementation: what CompuServe was, what ran it, and how users reached it.

Tags: **[C]** confirmed · **[S]** secondary · **[GAP]** unknown.

## 1. Timeline

| Date | Event | Source |
|---|---|---|
| 1969 | Compu-Serv Network, Inc. founded in Columbus, OH, as a Golden United Life subsidiary (timesharing) | [S] Wikipedia; [C] Trevor memo |
| Feb 1980 | CB Simulator launched | [S] |
| 24 Sep 1979 | MicroNET consumer service launched, sold through Radio Shack | [S] |
| May 1980 | H&R Block buys CompuServe (~$25M) | [S] |
| 1 Jul 1980 | MicroNET renamed **CompuServe Information Service (CIS)** | [S] |
| 1982 | B protocol replaces A [C, Ranshaw]; CompuServe Network Services formed | |
| 15 Jun 1987 | GIF87a released (Steve Wilhite's team) | [S] |
| 1989 | CIM/HMI introduced; Internet mail gateway; The Source bought (23 Jun) and shut down (1 Aug) | [S] |
| Apr 1995 | Peak of ~3M members | [S] |
| 1995–96 | Forums move to the NT-based "NISA" hosts (HMI-only) | [C] cishmi.faq |
| 1996 | CSi rebrand / CompuServe 3.0 | [S] |
| Feb 1998 | WorldCom buys CompuServe; the consumer service goes to AOL | [S] |
| 30 Jun 2009 | CompuServe Classic (HMI) and OurWorld shut down | eWeek, The Register |
| 15 Dec 2017 | Forums closed | [S] |

Wikipedia's claims that "HMI was discontinued in 2004" and that "forums were converted to HTML in 1997" conflict with WinCIM working until 2009. Treat them as unreliable.

## 2. Host hardware and OS

Source: Sandy Trevor, internal memo "We Call Them 10's", 31 Aug 1988, <https://ftp.inwap.com/pdp10/compuserve.txt> [C].

- **Machines:** DEC 36-bit, in this order:
  - KA-10 (1969)
  - KI-10s (seven by 1974)
  - KL / DECSYSTEM-2050s
  - **Systems Concepts SC-30** (ordered 1986, arrived late 1987)
  - **SC-40** (1993). CompuServe licensed the design and built them in-house. Some were still online after 2005.
- **OS:** heavily modified TOPS-10, the "C/D/E" monitors (E added a class scheduler).
- **Applications:** written in XF4 and Bliss-36, plus Wilhite's BASIC compiler.
- **Front ends:** 680i (PDP-8) front ends, then a PDP-15 comms switch.
- **Sites:** Upper Arlington, Columbus (1973); Dublin (1978); later Hilliard.
- **Mid-90s:** the NISA servers, Windows NT on Intel, ran forums and mail for HMI clients [C, cishmi.faq]. <https://gunkies.org/wiki/CompuServe_recollections> [S]

Primary source not yet explored: 32 GB of raw CompuServe disk images, <https://archive.org/details/2015-05-compuserve-raw-disks>. They use 584-byte sectors in a nonstandard format and are hard to read. They are the best hope for exact host prompts and help text [GAP].

## 3. User IDs

- User IDs were TOPS-10 **PPNs** (project, programmer): `70000,130`, `75116,3524`, and later 10-digit forms like `102345,6789`.
- Internet mail form (from 1989): `70000.130@compuserve.com`. The comma becomes a period.
- The claim that IDs only use octal digits (no 8 or 9) is plausible but not verified. `75116,3524` contains no 8 or 9; other surviving examples should be checked.

**For our server:**
- Allocate IDs as `7xxxx,xxx` (project ≥ 70000).
- Parse with a lenient regex, `^\d{5,6},\d{1,5}$`.
- Accept `.` in place of `,` (`/INT:60`-style options are stripped first).

## 4. Network and access paths

- **CompuServe's own network:** PDP-11 nodes from 1977, later 486/Qbus "Trinodes". It later moved to X.25, Frame Relay, ATM and IP. Users dialled a local node, which prompted `Host Name:`.
- **Partner networks:** Tymnet, Telenet/SprintNet, Datapac, Transpac, Infonet, Mercury. Each had its own login dialogue before reaching CIS (scripts in CompuServe's 1996 CSERVE package).
- **Host names:** `CIS` in the manuals, `CPS` in the 1996 scripts, `CISAGREE` over the Internet.
- **Internet:**
  - 1992: `telnet hermes.merit.edu` → "Which Host?" → `compuserve`.
  - Later: direct telnet (exact hostname [GAP]).
  - 1995: PPP via `<id>/GO:PPPCONNECT`.
  - WinCIM could also use Winsock TCP on **port 4144**.

## 5. Clients

| Kind | Examples | Interface |
|---|---|---|
| Dumb terminal / emulator | Any terminal program, VT100, CompuServe's VIDTEX | ASCII + VIDTEX escapes, B for downloads |
| Offline readers (ASCII) | TAPCIS (≤6.0), AutoSIG/ATO, OzCIS | Scripted ASCII. Depend on exact prompts and `GO DEFAULT` settings |
| GUI (HMI) | DOSCIM, WinCIM, MacCIM, OS/2 CIM, CSNAV, MacNAV, OzWIN, TAPCIS 6.1 | HMI over the B+ Transport Layer |

See [02](02-terminal-ux.md) for the ASCII experience, [03](03-link-protocols.md) for B/B+, and [04](04-cim-hmi.md) for HMI.

## 6. Graphics

- **CompuServe RLE** [C]:
  - Starts with `ESC G H` (256×192) or `ESC G M` (128×96).
  - Each byte is a run length + 0x20 (0–94 pixels). Runs alternate background/foreground, starting with background, and wrap across lines.
  - Ends with `ESC G N` (optionally preceded by BEL).
  - Used for weather maps (`GO AWX`) and the CB "picture" pages.
  - Send it only to clients that report `GH`/`GM` in `ESC I`.
  - Refs: <https://github.com/RevCurtisP/csrle>, PRONOM fmt/1538.
- **GIF:** CompuServe's own format, from 1987. Delivered by B+ download (`ESC [ > 1 g` announces one to capable clients; doc 03 §5.4).
- **NAPLPS:** no evidence that CIS used it.

## 7. Other reimplementations

| Project | What it is |
|---|---|
| `24high/CompuServe-Emulator` (C#, 2026) | WinCIM 3.x HMI server. **Key reference** (doc 04). |
| `paulsullivan33-dev/compuserve-simulator` (Python) | Text-mode CIS-flavoured simulation, 1988 setting. Not protocol-faithful. |
| `CompuServeReloaded/csr-server` (C#, 2023) | Stub only. |
| `RevCurtisP/csrle` | RLE tools and the original RLE.TXT. |
