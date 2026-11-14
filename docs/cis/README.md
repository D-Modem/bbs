# CompuServe Revival — Research and Design

These documents are the base for a new implementation of the CompuServe Information Service backend. We will write the backend in Go as a fork of [D-Modem/bbs](https://github.com/D-Modem/bbs). It will serve the classic terminal interface and the WinCIM/CIM GUI clients.

> **Update:** we do not use a fork. We build the service in this repository, as `internal/cis` (a `frontend = compuserve` listener) and `internal/xfer/cisb.go`. Where doc 05 says "the fork" and "upstream files", read "this repo" and "the existing files of the BBS". The objective is compatibility with the real clients (TAPCIS, OzCIS, AutoSIG, WinCIM and the other CIMs), not a copy that only looks the same.

| Doc | Contents |
|---|---|
| [01 — History and architecture](01-history-and-architecture.md) | Timeline, PDP-10/SC-40 hosts, NISA NT servers, user IDs, networks, clients, graphics, other revival projects |
| [02 — Terminal (ASCII) experience](02-terminal-ux.md) | Verbatim login, menus, prompts, forum command language, message headers, libraries, Mail, CB, billing, GO words |
| [03 — Link protocols](03-link-protocols.md) | A, B and B+ at the byte level, ESC I identification, VIDTEX escapes, how transfers start |
| [04 — GUI service (CIM/HMI)](04-cim-hmi.md) | Connection scripts, `?LOG` codes, Secure Login, the AP handshake, B+ Transport Layer, HMI message catalogue, reverse-engineering plan |
| [05 — Server design](05-server-design.md) | The layout of the fork, the data mapping, connection lifecycle, walkthroughs, tests, milestones |

## Confidence tags used throughout

- **[C] / [V]** Confirmed: a primary specification, a document of the period or a verbatim capture says it.
- **[P]** Paraphrased from a source of the period.
- **[S]** Secondary: Wikipedia or recollections.
- **[RE]** Comes from a single reverse-engineering source (mainly the 2026 `24high/CompuServe-Emulator`). It is necessary to check it on the wire.
- **[I]** Inference or design decision.
- **[GAP]** Not publicly known.

## Most important sources

- Russ Ranshaw, *The CompuServe B Plus Protocol* (1993): the specification from CompuServe.
- The `bp.c` of Steve Wilhite (1985) and the OOBPLUS of Steve Sneed (1991): reference code for B and B+.
- The TAPCIS 5.3 manual and scripts, and the Forum 4D command list (1989): the contract for the ASCII commands.
- Session captures from 1987–1995 on discmaster.textfiles.com.
- `24high/CompuServe-Emulator` (C#, 2026): the only detailed HMI and Secure Login notes that we found. **Cite it, but do not copy from it:** it contains scripts from CompuServe and decompiler output.
- The 1994 HMI developer FAQ from CompuServe and the 1995 NISA migration FAQ.

## Biggest open questions

1. The HMI message catalogue after pages, articles and mail-read (doc 04 §4).
2. The format of the CB line and the text for the band selection (doc 02 §6).
3. Did CIM clients before 1996 use Secure Login, and which mode numbers did they use?
4. The exact help texts of the host. Possible sources: the archive.org disk images and the manual scans of the Computer History Museum.
