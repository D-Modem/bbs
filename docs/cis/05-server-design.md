# 05 — Server Design: CompuServe on a D-Modem/bbs Fork

**Decision:** build the CompuServe backend as a **fork of [D-Modem/bbs](https://github.com/D-Modem/bbs)**, a Go BBS (AGPL-3.0) for telnet, SSH, TN3270 and modem callers.

The fork adds two CompuServe front ends:
- **ASCII**: the classic terminal service.
- **HMI**: the WinCIM/CIM GUI service.

The two front ends run on the existing users, message base, mail, file areas, chat hub, transfers and listeners of the BBS.

The details of the protocols and the user interface are in docs [02](02-terminal-ux.md), [03](03-link-protocols.md) and [04](04-cim-hmi.md).

## 1. Why D-Modem/bbs fits

We examined commit `b9a9acc` (7 Nov 2026): about 87k lines of Go, Go 1.24, created 10 Oct 2026.

| CompuServe needs | D-Modem/bbs already has |
|---|---|
| Telnet and modem callers, 8-bit clean | `internal/telnet` (BINARY, ECHO, SGA, NAWS, TTYPE, IAC doubling). Modem callers come through the D-Modem softmodem. Thus, **real dial-up into CompuServe** is possible. |
| Listeners, limits, TLS, bans, panic isolation | `internal/server` (`Listener{Kind: telnet\|raw\|rlogin\|ssh}`, limits for each IP, `handle()`), `internal/guard` |
| One core with more than one front end | `internal/app` defines a `Frontend` interface. `textui` and `ui3270` are two front ends over the same screens. **This is the shape that we want.** |
| Users, sessions, who's online, "jobs" | `store.User`, `bbs.Node` (node number ≈ CIS job number), calls log |
| Forum messages with threads | `store.Message{AreaID, ReplyTo, Thread, …}`, areas with groups and read/post access |
| CompuServe Mail | Private mail in `store` (`AreaID = 0`, an inbox for each user) |
| Forum libraries | `store.FileArea` / `store.File` (download count, uploader, descriptions), file store, archive readers |
| XMODEM / YMODEM / Kermit | `internal/xfer`, a protocol registry (`xfer.Protocols`) with `Send(Port, files)` / `Receive(Port, sink)` |
| CB Simulator and forum conferences | Chat hub with rooms, permanent rooms, paging (`bbs/chat*.go`) |
| DB, migrations, backups | SQLite, PostgreSQL or MySQL. Additive `ALTER TABLE` migrations (`store/schema.go migrate`). Backups. |
| Bonus | HTTP API, web terminal (xterm.js), web forum, NNTP: the CIS forums in a browser or newsreader with no extra work |

To build all of this from the start would take months. The work that is specific to CompuServe (login, page tree, the ASCII command language, B+, HMI) is about the same in the two cases.

## 2. Constraints and risks

1. **We must fork. A library import will not operate.** All the useful code is under `internal/`, and Go does not let a different module import `internal` packages. Keep the fork mergeable (§4).
2. **The licence is AGPL-3.0-only**, with a commercial licence as the alternative. Our fork must stay AGPL. We must offer the source to all persons who use the service over the network (a `GO SOURCE` page with a link to the repository is sufficient). To send changes upstream, we must sign their CLA (CONTRIBUTING.md).
3. **The project is young:** about 4 weeks old, about 62 commits, one organization. Expect API changes. To decrease this risk, keep our code in new packages and change few upstream files.
4. **Probe collisions.** For ANSI callers, the BBS sends `ENQ` (answerback) and `CSI > 0 q` (XTVERSION) at connect. These two have a meaning for CompuServe clients:
   - `ENQ` starts the B-protocol handshake (doc 03 §3.1).
   - `ESC [ > 0 q` is the application-mode query of CIM (doc 04 §2.4).

   **CIS listeners must skip the terminal probes of the BBS** and run their own identification (`ESC I`).
5. **The app screens have the shape of a BBS.** `internal/app` defines BBS menus (message areas, profile…). The page tree, `!` prompts and forum command language of CompuServe are different. **We use the services under `app` again, but not its screens.** Our front ends are new packages.

## 3. Architecture

```
                         D-Modem/bbs fork (cmd/bbs)                          ← new in the fork: ★
 ┌──────────────────────────────────────────────────────────────────────────────────────────┐
 │ internal/server  listeners:  telnet :23 (BBS) │ ★cis :2323 (telnet) │ ★cim :4144 (raw)    │
 │                                   │                    │                    │             │
 │                         textui / ui3270          ★cis/login  (Host Name, User ID, SLP,    │
 │                         (the normal BBS,          ?LOG codes) → ★cis/termid (ESC I, ENQ)  │
 │                          kept or disabled)              │                                 │
 │                                                ┌────────┴─────────┐                       │
 │                                         ★cis/ascii            ★cis/hmi                    │
 │                                    (pages, ! prompts,     (HMI messages over the          │
 │                                     forum/mail/CB cmds)    B+ Transport Layer)            │
 │                                                └────────┬─────────┘                       │
 │                                                   ★cis/core                               │
 │                       (pages+GO words, forums=sections+libraries, mail addressing,        │
 │                        CB bands/channels, billing meter, PPN user IDs)                    │
 │                                                         │                                 │
 │   internal/bbs (nodes, chat hub, access)   internal/store (+★ migrations)   internal/xfer │
 │                                                          (+★ cisb.go: B / B+ / TL)        │
 └──────────────────────────────────────────────────────────────────────────────────────────┘
```

### 3.1 New packages

| Package | Purpose |
|---|---|
| `internal/cis/login` | Network dialogue: `CONNECT` banner, `Host Name:` (CIS/CPS/CISAGREE), `User ID:` parse (`+`, `/INT:60`, `/secure:1`, `/GO:x`, `;TTY`, `*NAVIG`), password or SLP (doc 04 §2.3), `?LOG xxx` codes, banner and logoff text |
| `internal/cis/termid` | `ESC I` reply parser and checksum, the `ENQ` B-version test, `Caps` struct, GIF caps (`ESC [ > 0 g`) |
| `internal/xfer/cisb.go` | CIS B / Interim / B+ engine: quotes, checksum and CRCs, packets, window, `Tr`/`TI`, and **Transport Layer** `M`/`L` for HMI. Registered in `xfer.Protocols` as `B` and `Q` (Quick B). Easy to send upstream, because it is only one more protocol. |
| `internal/cis/aproto` | A protocol (low priority) |
| `internal/cis/core` | The CompuServe domain on top of `store`/`bbs`: page tree and GO resolver, forums, libraries, mail address forms (`INTERNET:` and other forms), CB model, billing meter, PPN allocation. **It knows nothing about bytes or screens.** |
| `internal/cis/ascii` | Terminal front end: line input with ^U/^V/^O/^C/^P semantics, pager, `!` prompts, BRIEF/PAGED/COMMAND/MENU modes, DEFAULT settings, forum command language (doc 02 §4.5), EDIT/LINEDIT, Mail, CB slash-commands, download protocol menu |
| `internal/cis/hmi` | GUI front end: `ESC [ >` handshake, HMI message codec (u16/u24/varint/text/DOS date), streams, dispatcher onto `cis/core` |
| `internal/cis/wiretap` | A byte log of the two directions for each session, with B+/HMI decode. You can see it through the existing HTTP API. **This is the reverse-engineering tool.** |
| `content/` | The page tree (YAML): page IDs, GO aliases, menus, articles, help text, the forum list |

### 3.2 Changes to upstream files (keep this list short)

| Upstream file | Change |
|---|---|
| `internal/server/server.go` | In `handle()`, dispatch `Listener.Kind == "cis"` and `"cim"` to `cis/login.Run(...)`. Add the two kinds to the config parser (`config.go`). |
| `internal/xfer/xfer.go` | Add `B` and `Q` to `Protocols` (or register them from the `init` of `cisb.go`). |
| `internal/store/schema.go` | New tables, and additive columns through the existing `migrate` list (§5). |
| `cmd/bbs/main.go` | Load `content/`, and add a `cis` subcommand (seed forums, allocate IDs). |
| `bbs.conf.sample` | `[listen cis]` and `[listen cim]` examples, and a `[cis]` section. |

## 4. Fork workflow

- Fork `D-Modem/bbs` to our GitHub and clone it into this project. Add `upstream` as a remote.
- Our work is in `internal/cis/**`, `internal/xfer/cisb*.go`, `content/` and `docs/cis/`. Upstream files contain only the hooks in §3.2.
- Rebase or merge `upstream/main` each week, with CI that runs `go vet` and `go test ./...` (upstream already has `.github/workflows/ci.yml`).
- Possible upstream PRs, if we sign their CLA: `xfer/cisb.go` (B+ is useful to all BBSes) and a generic listener-kind hook.

## 5. Data mapping onto `store`

| CompuServe | Stored as |
|---|---|
| User ID `70000,123` | `users.name = "70000,123"`. Thus, the full login, access and who's-online functions of the BBS operate on the ID with no change. The display name goes in a new `users.real_name`. New `users.slp_key` (doc 04 §2.3). Allocate octal-only IDs from 70000,100. Sysops from 76701,x. |
| Forum | New `cis_forums(id, go_word, name, welcome, version_tag, sysop_ids)`. |
| Forum section 0–17 | One `store.Area` for each section: `Group = forum`, and new `areas.cis_forum`, `areas.cis_section`. |
| Forum message number | CIS gives message numbers for each forum (`#: 210094`), but `store.Message.ID` is global. Add `messages.cis_num` (unique for each forum, monotonic) and map `ReplyTo`/`Thread` directly. |
| Membership | New `cis_members(forum, user, name, joined, high_msg, options)`. The name for each forum comes from JOIN, and the options from OPT (mode, prompt char, sections). |
| Library n | `store.FileArea` and new `file_areas.cis_forum` / `cis_lib`. |
| File entry | `store.File` and new `title`, `keywords`, `dtype` (A/B). `Downloads` is "Accesses". The uploader PPN is the name of the user. |
| Mail | Existing private mail. Add `from_addr`/`to_addrs` (address forms), `expire`, `binary`. |
| CB | Chat hub rooms with the names `cb:<band>:<ch>`, forum conferences as `co:<forum>:<room>`. Handles are in `cis_handles`, and the job number is `bbs.Node.Num`. |
| DEFAULT settings | `users.prefs` (it already holds `key=value` preferences): `brief=yes paged=no top=command editor=edit width=79 …` |
| Billing | Session times are already in the calls log. A new `cis_charges` view calculates their prices with the rate tables of the period (doc 02 §7). |

## 6. Connection lifecycle (on `cis` and `cim` listeners)

```
accept (server.handle) → Kind cis: telnet.New + StartNVT(binary) WITHOUT the VT/ENQ probes
                         Kind cim: raw TCP
  ▼
login.Run   "\r\nCONNECT 9600\r\n\r\nHost Name: " → CIS|CPS|CISAGREE
            "User ID: " → parse → SLP (if + or /secure) or "Password: " (echo off)
            fail → "?LOG INE" / "?? LOGINE - Invalid entry - try again", 3 tries
  ▼
termid      ESC I → "#…" reply (≤10 tries; none in ~2 s = dumb terminal)
  ▼
dispatch    Caps.AppProto (AP) → hmi.Run   (ESC [ > 0 q / 3;3 p, ACK, ENQ, B+ with TL=1)
            otherwise          → ascii.Run (banner, "Last access", mail notice, first page)
  ▼
session     a bbs.Node is registered for its life: who's online, chat delivery, /UST job #
  ▼
logoff      "Thank you for using CompuServe!" / "Off at …" / "Connect time = h:mm"; calls log
```

The B-protocol `ENQ` test runs immediately before the server sends a download or graphic, not at login. The server caches the result in `Caps`.

## 7. Interaction walkthroughs

### 7.1 Terminal user: forum read and B+ download

```
H: CONNECT 9600 / Host Name:          U: CIS
H: User ID:                           U: 70000,123
H: Password:                          U: ********
H: ESC I                              T: (none → dumb terminal)
H: banner … "Enter choice !"
U: GO IBMCOM     → core.Pages.Resolve → forum page → ascii forum UI
H: "Welcome to IBM Communications Forum, V. 4D(…)" … "Forum !"
U: LIB 10        H: "LIB !"           U: BRO/LONG
H: "[76701,250]" / "BPLUS.ZIP/binary  12-Mar-93 41234  Accesses: 990" / "Keywords: …"
U: DOW BPLUS.ZIP /PROTO:B
H: ENQ           T: DLE + + DLE 0     → xfer B+ ('+' exchange, TDB, N…, TC)
H: "Forum !"     U: OFF  → "Thank you for using CompuServe!" …
```

### 7.2 WinCIM: the Forums page

```
TCP :4144   H: CONNECT 9600 / Host Name:    C: CPS
H: User ID:              C: +70000,123/INT:60 /secure:1
H: RA:…   C: RB:… UR:…   H: HR:…
H: ESC I                 C: #WinCIM:3.0.1:WN6,…,AP,…,+4657
H: ESC [ > 0 q           C: #1,2,3,6,8,10
H: ESC [ > 3;3 p         C: #3,2,1,4,2,0;614
H: ACK, ENQ              C: DLE + + DLE 0
H: '+' 04 04 02 00 00 01 C: '+' …
── HMI in L packets ──
C: 0x0B                  H: 0x01 ×2 (client drops the first one)
C: 0x20                  H: 0x21
C: 0x24 "CIS:FORUMS"     → core.Pages.Resolve("FORUMS") → hmi.EncodePage → 0x27
C: 0x0F keepalive        H: 0x01          …   C: 0x07 logoff
```

### 7.3 One world

- A WinCIM user posts in IBMCOM section 4. `core.Forums.Post` stores the post in the `store.Area` of the section, and the `REA NEW` of a terminal user shows it.
- The upstream **NNTP server and web forum** show the same messages, with no extra work.
- CB:
  - A terminal user on `/TUN 12` and a WinCIM chat user share the hub room `cb:A:12`.
  - ASCII renders `(12,Handle) text`. HMI renders its chat message (we must reverse-engineer this).
  - IRC users can join the same room through the ircd of upstream, if it is enabled.

## 8. Concurrency

The upstream model is one goroutine for each caller, with node registration in `bbs`. We keep this model. Additions:
- `link` reads with deadlines for each byte (`xfer.Port` already has `SetReadDeadline`) for B+ timeouts.
- During a transfer, the ASCII front end gives the port to `xfer`, and takes it back after the transfer.
- The HMI session runs a reader goroutine (B+ Transport Layer → messages) and a writer that merges replies with async events (chat, mail-waiting).
- Chat and CB fan-out uses the upstream hub. Thus, slow clients never block a room.

## 9. Testing

| Layer | How |
|---|---|
| `xfer/cisb` | Specification vectors (checksum `0x2A`, CRC `0x57FF`, doc 03). Fuzz the decoder. Loopback host↔client over `net.Pipe` with noise, drops and duplicates. Test against the B+ of OzCIS in DOSBox-X. |
| `termid` | Real ID strings: WinCIM, OzCIS, Wilhite. |
| `cis/ascii` | Golden transcripts from the captures in doc 02, as exact bytes. The command strings of TAPCIS (doc 02 §4.5) as acceptance tests. |
| `login/SLP` | Test vectors from the 24high emulator, checked independently. |
| `hmi` | Replay recorded WinCIM sessions from the wire tap. |
| End to end | `telnet localhost 2323`. DOSBox-X `serial1=modem` (dial `ATDT host:2323`) with TAPCIS, OzCIS and AutoSIG. WinCIM 2.x/3.x under Wine (`CommType=WINSOCK`, port 4144). **Real modems through D-Modem.** |

## 10. Milestones

0. **Fork and setup:** fork, build, run the standard BBS, add a CI job, add `[listen cis]` and dispatch to a stub `login.Run`.
1. **Login and top menu:** Host Name, User ID, password, banner, page tree from `content/`, `GO`/`T`/`M`/`B`/`F`/`R`/`H`, pager, `OFF`/`BYE`/`LOG`.
2. **DEFAULT settings:** BRIEF/PAGED/COMMAND/MENU in `users.prefs`. `SET`. `;TTY`.
3. **Forums, ASCII:** JOIN, sections, `REA`/`SCA`/`BRO` command language, headers, post/reply, EDIT, OPTIONS, exit stats.
4. **Libraries and transfers:** BRO/DIR/DOW/UPL, protocol menu (XMODEM and Kermit from upstream), then **CIS B/B+** in `xfer`.
5. **Mail:** menu and `Mail!` command modes, address forms, `INTERNET:` gateway (the SMTP work of upstream in TODO.md can help).
6. **CB Simulator** and forum conferences on the chat hub.
7. **CIM:** `cim` listener, SLP, `ESC I`/AP handshake, B+ Transport Layer, HMI session, pages, menus, articles. Get WinCIM to the main menu.
8. **HMI features:** forums, mail read → send → libraries → chat (reverse engineering at each step. Refer to doc 04 §5).
9. **Offline-reader pass:** TAPCIS, AutoSIG and OzCIS run their scripts with no errors.
10. **Polish:** billing display, RLE/GIF pages, sysop tools in the upstream config UI, content import.
