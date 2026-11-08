# CompuServe Revival — Research and Design

These docs are the groundwork for reimplementing the CompuServe Information Service backend. The backend will be written in Go as a fork of [D-Modem/bbs](https://github.com/D-Modem/bbs). It will serve both the classic terminal interface and the WinCIM/CIM GUI clients.

> **Update:** rather than a fork, the service is being built in this repository, as `internal/cis` (a `frontend = compuserve` listener) and `internal/xfer/cisb.go`. Where doc 05 talks about "the fork" and "upstream files", read "this repo" and "the BBS's existing files". The goal is compatibility with the real clients (TAPCIS, OzCIS, AutoSIG, WinCIM and the other CIMs), not a look-alike.

| Doc | Contents |
|---|---|
| [01 — History and architecture](01-history-and-architecture.md) | Timeline, PDP-10/SC-40 hosts, NISA NT servers, user IDs, networks, clients, graphics, other revival projects |
| [02 — Terminal (ASCII) experience](02-terminal-ux.md) | Verbatim login, menus, prompts, forum command language, message headers, libraries, Mail, CB, billing, GO words |
| [03 — Link protocols](03-link-protocols.md) | A, B and B+ at the byte level, ESC I identification, VIDTEX escapes, how transfers start |
| [04 — GUI service (CIM/HMI)](04-cim-hmi.md) | Connection scripts, `?LOG` codes, Secure Login, the AP handshake, B+ Transport Layer, HMI message catalogue, reverse-engineering plan |
| [05 — Server design](05-server-design.md) | How the fork is laid out, the data mapping, connection lifecycle, walkthroughs, testing, milestones |

## Confidence tags used throughout

- **[C] / [V]** Confirmed: a primary spec, period document or verbatim capture says so.
- **[P]** Paraphrased from a period source.
- **[S]** Secondary: Wikipedia or recollections.
- **[RE]** Comes from a single reverse-engineering source (mainly the 2026 `24high/CompuServe-Emulator`). It needs checking on the wire.
- **[I]** Inference or design choice.
- **[GAP]** Not publicly known.

## Most important sources

- Russ Ranshaw, *The CompuServe B Plus Protocol* (1993): CompuServe's own spec.
- Steve Wilhite's `bp.c` (1985) and Steve Sneed's OOBPLUS (1991): reference code for B and B+.
- TAPCIS 5.3 manual and scripts, and the Forum 4D command listing (1989): the ASCII command contract.
- Session captures from 1987–1995 on discmaster.textfiles.com.
- `24high/CompuServe-Emulator` (C#, 2026): the only detailed HMI and Secure Login notes found. **Cite it, but don't copy from it:** it contains CompuServe's scripts and decompiler output.
- CompuServe's 1994 HMI developer FAQ and the 1995 NISA migration FAQ.

## Biggest open questions

1. The HMI message catalogue beyond pages, articles and mail-read (doc 04 §4).
2. The CB line format and band selection text (doc 02 §6).
3. Whether pre-1996 CIM clients used Secure Login and which mode numbers they used.
4. Exact host help texts. Candidates: the archive.org disk images and the Computer History Museum manual scans.
