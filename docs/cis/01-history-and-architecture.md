# 01 — History and Original Architecture

This document gives the context for the new implementation: what CompuServe was, what ran it, and how users connected to it.

Tags: **[C]** confirmed · **[S]** secondary · **[GAP]** unknown.

## 1. Timeline

| Date | Event | Source |
|---|---|---|
| 1969 | Compu-Serv Network, Inc. founded in Columbus, OH, as a subsidiary of Golden United Life (timesharing) | [S] Wikipedia; [C] Trevor memo |
| Feb 1980 | Start of the CB Simulator | [S] |
| 24 Sep 1979 | Start of the MicroNET consumer service, sold through Radio Shack | [S] |
| May 1980 | H&R Block buys CompuServe (~$25M) | [S] |
| 1 Jul 1980 | MicroNET gets the new name **CompuServe Information Service (CIS)** | [S] |
| 1982 | The B protocol replaces A [C, Ranshaw]. CompuServe Network Services is formed | |
| 15 Jun 1987 | GIF87a released (the team of Steve Wilhite) | [S] |
| 1989 | Start of CIM/HMI. Internet mail gateway. CompuServe buys The Source (23 Jun) and closes it (1 Aug) | [S] |
| Apr 1995 | Peak of ~3M members | [S] |
| 1995–96 | Forums move to the NT-based "NISA" hosts (HMI only) | [C] cishmi.faq |
| 1996 | CSi rebrand / CompuServe 3.0 | [S] |
| Feb 1998 | WorldCom buys CompuServe. The consumer service goes to AOL | [S] |
| 30 Jun 2009 | CompuServe Classic (HMI) and OurWorld close | eWeek, The Register |
| 15 Dec 2017 | Forums closed | [S] |

Wikipedia says that "HMI was discontinued in 2004" and that "forums were converted to HTML in 1997". These claims do not agree with the fact that WinCIM operated until 2009. Do not trust them.

## 2. Host hardware and OS

Source: Sandy Trevor, internal memo "We Call Them 10's", 31 Aug 1988, <https://ftp.inwap.com/pdp10/compuserve.txt> [C].

- **Machines:** DEC 36-bit, in this sequence:
  - KA-10 (1969)
  - KI-10s (seven by 1974)
  - KL / DECSYSTEM-2050s
  - **Systems Concepts SC-30** (ordered 1986, arrived late 1987)
  - **SC-40** (1993). CompuServe got a licence for the design and built the machines itself. Some were still online after 2005.
- **OS:** TOPS-10 with many changes, the "C/D/E" monitors (E added a class scheduler).
- **Applications:** written in XF4 and Bliss-36, and the BASIC compiler of Wilhite.
- **Front ends:** 680i (PDP-8) front ends, then a PDP-15 comms switch.
- **Sites:** Upper Arlington, Columbus (1973); Dublin (1978); later Hilliard.
- **Mid-90s:** the NISA servers, Windows NT on Intel, ran forums and mail for HMI clients [C, cishmi.faq]. <https://gunkies.org/wiki/CompuServe_recollections> [S]

A primary source that we did not examine yet: 32 GB of raw CompuServe disk images, <https://archive.org/details/2015-05-compuserve-raw-disks>. They use 584-byte sectors in a nonstandard format, and they are difficult to read. They are the best source for the exact prompts and help text of the host [GAP].

## 3. User IDs

- User IDs were TOPS-10 **PPNs** (project, programmer): `70000,130`, `75116,3524`, and later 10-digit forms, for example `102345,6789`.
- The internet mail form (from 1989): `70000.130@compuserve.com`. The comma becomes a period.
- Some sources say that IDs use only octal digits (no 8 or 9). This is possible, but we did not verify it. `75116,3524` contains no 8 or 9. It is necessary to check other examples that remain.

**For our server:**
- Allocate IDs as `7xxxx,xxx` (project ≥ 70000).
- Parse with a lenient regex, `^\d{5,6},\d{1,5}$`.
- Accept `.` in place of `,` (first remove options of the type `/INT:60`).

## 4. Network and access paths

- **The network of CompuServe:** PDP-11 nodes from 1977, later 486/Qbus "Trinodes". It later moved to X.25, Frame Relay, ATM and IP. Users dialled a local node, which showed the prompt `Host Name:`.
- **Partner networks:** Tymnet, Telenet/SprintNet, Datapac, Transpac, Infonet, Mercury. Each network had its own login dialogue before the connection to CIS (scripts in the 1996 CSERVE package of CompuServe).
- **Host names:** `CIS` in the manuals, `CPS` in the 1996 scripts, `CISAGREE` over the Internet.
- **Internet:**
  - 1992: `telnet hermes.merit.edu` → "Which Host?" → `compuserve`.
  - Later: direct telnet (exact hostname [GAP]).
  - 1995: PPP through `<id>/GO:PPPCONNECT`.
  - WinCIM could also use Winsock TCP on **port 4144**.

## 5. Clients

| Kind | Examples | Interface |
|---|---|---|
| Dumb terminal / emulator | All terminal programs, VT100, the VIDTEX of CompuServe | ASCII + VIDTEX escapes, B for downloads |
| Offline readers (ASCII) | TAPCIS (≤6.0), AutoSIG/ATO, OzCIS | Scripted ASCII. They depend on exact prompts and `GO DEFAULT` settings |
| GUI (HMI) | DOSCIM, WinCIM, MacCIM, OS/2 CIM, CSNAV, MacNAV, OzWIN, TAPCIS 6.1 | HMI over the B+ Transport Layer |

Refer to [02](02-terminal-ux.md) for the ASCII experience, [03](03-link-protocols.md) for B/B+, and [04](04-cim-hmi.md) for HMI.

## 6. Graphics

- **CompuServe RLE** [C]:
  - Starts with `ESC G H` (256×192) or `ESC G M` (128×96).
  - Each byte is a run length + 0x20 (0–94 pixels). Runs are background and foreground in turn, with background first. They wrap across lines.
  - Ends with `ESC G N` (optionally with a BEL before it).
  - Used for weather maps (`GO AWX`) and the CB "picture" pages.
  - Send it only to clients that report `GH`/`GM` in `ESC I`.
  - Refs: <https://github.com/RevCurtisP/csrle>, PRONOM fmt/1538.
- **GIF:** the format of CompuServe, from 1987. Sent by B+ download (`ESC [ > 1 g` tells capable clients that a GIF comes; doc 03 §5.4).
- **NAPLPS:** no evidence that CIS used it.

## 7. Other reimplementations

| Project | What it is |
|---|---|
| `24high/CompuServe-Emulator` (C#, 2026) | WinCIM 3.x HMI server. **Key reference** (doc 04). |
| `paulsullivan33-dev/compuserve-simulator` (Python) | Text-mode simulation in the style of CIS, 1988 setting. It does not follow the protocol exactly. |
| `CompuServeReloaded/csr-server` (C#, 2023) | Stub only. |
| `RevCurtisP/csrle` | RLE tools and the original RLE.TXT. |
