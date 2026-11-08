# 05 — Server Design: CompuServe on a D-Modem/bbs Fork

**Decision:** build the CompuServe backend as a **fork of [D-Modem/bbs](https://github.com/D-Modem/bbs)**, a Go BBS (AGPL-3.0) for telnet, SSH, TN3270 and modem callers.

The fork adds two CompuServe front ends:
- **ASCII**: the classic terminal service.
- **HMI**: the WinCIM/CIM GUI service.

Both run on top of the BBS's existing users, message base, mail, file areas, chat hub, transfers and listeners.

Protocol and UX details are in docs [02](02-terminal-ux.md), [03](03-link-protocols.md) and [04](04-cim-hmi.md).

## 1. Why D-Modem/bbs fits

Reviewed at commit `b9a9acc` (7 Nov 2026): about 87k lines of Go, Go 1.24, created 10 Oct 2026.

| CompuServe needs | D-Modem/bbs already has |
|---|---|
| Telnet and modem callers, 8-bit clean | `internal/telnet` (BINARY, ECHO, SGA, NAWS, TTYPE, IAC doubling). Modem callers come via the D-Modem softmodem, so **real dial-up into CompuServe** is possible. |
| Listeners, limits, TLS, bans, panic isolation | `internal/server` (`Listener{Kind: telnet\|raw\|rlogin\|ssh}`, per-IP limits, `handle()`), `internal/guard` |
| One core with several front ends | `internal/app` defines a `Frontend` interface. `textui` and `ui3270` are two front ends over the same screens. **This is the shape we want.** |
| Users, sessions, who's online, "jobs" | `store.User`, `bbs.Node` (node number ≈ CIS job number), calls log |
| Forum messages with threads | `store.Message{AreaID, ReplyTo, Thread, …}`, areas with groups and read/post access |
| CompuServe Mail | Private mail in `store` (`AreaID = 0`, inbox per user) |
| Forum libraries | `store.FileArea` / `store.File` (downloads count, uploader, descriptions), file store, archive readers |
| XMODEM / YMODEM / Kermit | `internal/xfer`, a protocol registry (`xfer.Protocols`) with `Send(Port, files)` / `Receive(Port, sink)` |
| CB Simulator and forum conferences | Chat hub with rooms, permanent rooms, paging (`bbs/chat*.go`) |
| DB, migrations, backups | SQLite, PostgreSQL or MySQL. Additive `ALTER TABLE` migrations (`store/schema.go migrate`). Backups. |
| Bonus | HTTP API, web terminal (xterm.js), web forum, NNTP: the CIS forums for free in a browser or newsreader |

Building all of this from scratch would take months. The CompuServe-specific work (login, page tree, the ASCII command language, B+, HMI) is about the same either way.

## 2. Constraints and risks

1. **We must fork; a library import won't work.** Everything useful is under `internal/`, and Go forbids importing `internal` packages from another module. Keep the fork mergeable (§4).
2. **The licence is AGPL-3.0-only**, with a commercial licence as the alternative. Our fork must stay AGPL. Anyone using the service over the network must be offered the source (a `GO SOURCE` page linking the repo covers this). Sending changes upstream requires their CLA (CONTRIBUTING.md).
3. **The project is young:** about 4 weeks old, about 62 commits, one org. Expect API churn. Mitigate by keeping our code in new packages and touching few upstream files.
4. **Probe collisions.** For ANSI callers the BBS sends `ENQ` (answerback) and `CSI > 0 q` (XTVERSION) at connect. Both mean something to CompuServe clients:
   - `ENQ` starts the B-protocol handshake (doc 03 §3.1).
   - `ESC [ > 0 q` is CIM's application-mode query (doc 04 §2.4).

   **CIS listeners must skip the BBS's terminal probing** and run their own identification (`ESC I`).
5. **The app screens are BBS-shaped.** `internal/app` defines BBS menus (message areas, profile…). CompuServe's page tree, `!` prompts and forum command language are different. **We reuse the services under `app`, not its screens.** Our front ends are new packages.

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
| `internal/cis/login` | Network dialogue: `CONNECT` banner, `Host Name:` (CIS/CPS/CISAGREE), `User ID:` parsing (`+`, `/INT:60`, `/secure:1`, `/GO:x`, `;TTY`, `*NAVIG`), password or SLP (doc 04 §2.3), `?LOG xxx` codes, banner and logoff text |
| `internal/cis/termid` | `ESC I` reply parser and checksum, the `ENQ` B-version test, `Caps` struct, GIF caps (`ESC [ > 0 g`) |
| `internal/xfer/cisb.go` | CIS B / Interim / B+ engine: quoting, checksum and CRCs, packets, window, `Tr`/`TI`, and **Transport Layer** `M`/`L` for HMI. Registered in `xfer.Protocols` as `B` and `Q` (Quick B). Upstream-friendly, since it is just another protocol. |
| `internal/cis/aproto` | A protocol (low priority) |
| `internal/cis/core` | CompuServe's domain on top of `store`/`bbs`: page tree and GO resolver, forums, libraries, mail address forms (`INTERNET:` etc.), CB model, billing meter, PPN allocation. **It knows nothing about bytes or screens.** |
| `internal/cis/ascii` | Terminal front end: line input with ^U/^V/^O/^C/^P semantics, pager, `!` prompts, BRIEF/PAGED/COMMAND/MENU modes, DEFAULT settings, forum command language (doc 02 §4.5), EDIT/LINEDIT, Mail, CB slash-commands, download protocol menu |
| `internal/cis/hmi` | GUI front end: `ESC [ >` handshake, HMI message codec (u16/u24/varint/text/DOS date), streams, dispatcher onto `cis/core` |
| `internal/cis/wiretap` | Per-session byte log of both directions, with B+/HMI decode. Viewable through the existing HTTP API. **This is the reverse-engineering tool.** |
| `content/` | The page tree (YAML): page IDs, GO aliases, menus, articles, help text, the forum list |

### 3.2 Changes to upstream files (keep this list short)

| Upstream file | Change |
|---|---|
| `internal/server/server.go` | In `handle()`, dispatch `Listener.Kind == "cis"` and `"cim"` to `cis/login.Run(...)`. Add the two kinds to the config parser (`config.go`). |
| `internal/xfer/xfer.go` | Append `B` and `Q` to `Protocols` (or register them from `cisb.go`'s `init`). |
| `internal/store/schema.go` | New tables, plus additive columns via the existing `migrate` list (§5). |
| `cmd/bbs/main.go` | Load `content/`, and add a `cis` subcommand (seed forums, allocate IDs). |
| `bbs.conf.sample` | `[listen cis]` and `[listen cim]` examples, plus a `[cis]` section. |

## 4. Fork workflow

- Fork `D-Modem/bbs` to our GitHub and clone it into this project. Add `upstream` as a remote.
- Our work lives in `internal/cis/**`, `internal/xfer/cisb*.go`, `content/` and `docs/cis/`. Upstream files carry only the hooks in §3.2.
- Rebase or merge `upstream/main` weekly, with CI running `go vet` and `go test ./...` (upstream already has `.github/workflows/ci.yml`).
- Candidates for upstream PRs, if we sign their CLA: `xfer/cisb.go` (B+ is useful to any BBS) and a generic listener-kind hook.

## 5. Data mapping onto `store`

| CompuServe | Stored as |
|---|---|
| User ID `70000,123` | `users.name = "70000,123"`, so the whole BBS login, access and who's-online machinery works unchanged on the ID. The display name goes in a new `users.real_name`. New `users.slp_key` (doc 04 §2.3). Allocate octal-only IDs from 70000,100; sysops from 76701,x. |
| Forum | New `cis_forums(id, go_word, name, welcome, version_tag, sysop_ids)`. |
| Forum section 0–17 | One `store.Area` per section: `Group = forum`, plus new `areas.cis_forum`, `areas.cis_section`. |
| Forum message number | CIS numbers messages per forum (`#: 210094`), while `store.Message.ID` is global. Add `messages.cis_num` (unique per forum, monotonic) and map `ReplyTo`/`Thread` directly. |
| Membership | New `cis_members(forum, user, name, joined, high_msg, options)`. The per-forum name comes from JOIN, and options from OPT (mode, prompt char, sections). |
| Library n | `store.FileArea` plus new `file_areas.cis_forum` / `cis_lib`. |
| File entry | `store.File` plus new `title`, `keywords`, `dtype` (A/B). `Downloads` is "Accesses". The uploader PPN is the user's name. |
| Mail | Existing private mail. Add `from_addr`/`to_addrs` (address forms), `expire`, `binary`. |
| CB | Chat hub rooms named `cb:<band>:<ch>`, forum conferences as `co:<forum>:<room>`. Handles live in `cis_handles`, and the job number is `bbs.Node.Num`. |
| DEFAULT settings | `users.prefs` (it already holds `key=value` preferences): `brief=yes paged=no top=command editor=edit width=79 …` |
| Billing | Session times are in the calls log already; a new `cis_charges` view prices them with the period rate tables (doc 02 §7). |

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

The B-protocol `ENQ` test runs when a download or graphic is about to be sent, not at login. The result is cached in `Caps`.

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

- A WinCIM user posts in IBMCOM section 4. `core.Forums.Post` stores it in the section's `store.Area`, and a terminal user's `REA NEW` sees it.
- The upstream **NNTP server and web forum** expose the same messages, without extra work.
- CB:
  - A terminal user on `/TUN 12` and a WinCIM chat user share the hub room `cb:A:12`.
  - ASCII renders `(12,Handle) text`; HMI renders its chat message (to be reverse-engineered).
  - IRC users can join the same room through upstream's ircd, if enabled.

## 8. Concurrency

The upstream model is one goroutine per caller with node registration in `bbs`, and we keep it. Additions:
- `link` reads with per-byte deadlines (`xfer.Port` already has `SetReadDeadline`) for B+ timeouts.
- During a transfer, the ASCII front end hands the port to `xfer` and takes it back afterwards.
- The HMI session runs a reader goroutine (B+ Transport Layer → messages) and a writer that merges replies with async events (chat, mail-waiting).
- Chat and CB fan-out uses the upstream hub, so slow clients never block a room.

## 9. Testing

| Layer | How |
|---|---|
| `xfer/cisb` | Spec vectors (checksum `0x2A`, CRC `0x57FF`, doc 03). Fuzz the decoder. Loopback host↔client over `net.Pipe` with noise, drops and duplicates. Test against OzCIS's B+ in DOSBox-X. |
| `termid` | Real ID strings: WinCIM, OzCIS, Wilhite. |
| `cis/ascii` | Golden transcripts from the captures in doc 02, as exact bytes. TAPCIS's command strings (doc 02 §4.5) as acceptance tests. |
| `login/SLP` | Test vectors from the 24high emulator, checked independently. |
| `hmi` | Replay recorded WinCIM sessions from the wire tap. |
| End to end | `telnet localhost 2323`. DOSBox-X `serial1=modem` (dial `ATDT host:2323`) with TAPCIS, OzCIS and AutoSIG. WinCIM 2.x/3.x under Wine (`CommType=WINSOCK`, port 4144). **Real modems via D-Modem.** |

## 10. Milestones

0. **Fork and setup:** fork, build, run the stock BBS, add a CI job, add `[listen cis]` and dispatch to a stub `login.Run`.
1. **Login and top menu:** Host Name, User ID, password, banner, page tree from `content/`, `GO`/`T`/`M`/`B`/`F`/`R`/`H`, pager, `OFF`/`BYE`/`LOG`.
2. **DEFAULT settings:** BRIEF/PAGED/COMMAND/MENU in `users.prefs`; `SET`; `;TTY`.
3. **Forums, ASCII:** JOIN, sections, `REA`/`SCA`/`BRO` command language, headers, post/reply, EDIT, OPTIONS, exit stats.
4. **Libraries and transfers:** BRO/DIR/DOW/UPL, protocol menu (XMODEM and Kermit from upstream), then **CIS B/B+** in `xfer`.
5. **Mail:** menu and `Mail!` command modes, address forms, `INTERNET:` gateway (upstream's SMTP work in TODO.md may help).
6. **CB Simulator** and forum conferences on the chat hub.
7. **CIM:** `cim` listener, SLP, `ESC I`/AP handshake, B+ Transport Layer, HMI session, pages, menus, articles. Get WinCIM to the main menu.
8. **HMI features:** forums, mail read → send → libraries → chat (reverse-engineering at each step; doc 04 §5).
9. **Offline-reader pass:** TAPCIS, AutoSIG and OzCIS run their scripts cleanly.
10. **Polish:** billing display, RLE/GIF pages, sysop tools in the upstream config UI, content import.
