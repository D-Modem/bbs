# 04 — The GUI Service: CIM clients and HMI

The graphical clients (DOSCIM, WinCIM, MacCIM, OS/2 CIM, CSNAV/MacNAV, OzWIN, TAPCIS 6.1) did not scrape text. They spoke **HMI (Host-Micro Interface)**, a binary request/response protocol that runs **inside B Plus Transport Layer packets** (see [03](03-link-protocols.md) §4.6).

CompuServe never published the HMI spec. Everything below the B+ layer comes from reverse engineering.

Tags: **[C]** confirmed (period or primary document) · **[S]** secondary (Wikipedia, recollections) · **[RE]** from one reverse-engineering source only, not yet checked by us · **[GAP]** unknown.

## Sources

| Key | Source |
|---|---|
| E | `24high/CompuServe-Emulator`, a C# server (Sep 2026) that claims to bring WinCIM 3.x online. It was recovered by disassembling WinCIM 3.0.2 (`CCTN240C.DLL`, `CS3COR.DLL`, `CS3.EXE`). Notes in German: `HMI-PROTOKOLL.md`, `SECURE-LOGIN.md`, `NAECHSTER-SCHRITT.md`, `POST.md`, `SUCHE.md`. <https://github.com/24high/CompuServe-Emulator> |
| F | CompuServe HMI developer FAQ, Nov 1994. <https://discmaster.textfiles.com/view/21536/03-Comm.zip/hmidev.txt> |
| N | Rick Wilkes, CIS HMI/NISA migration FAQ, Dec 1995. <https://discmaster.textfiles.com/view/6764/CDA1_96.ISO/novell/cishmi.faq> |
| R | Ranshaw, B Plus spec (1993). See doc 03. |
| P | US Patent 5,737,538 (HMI "protocol data units"). <https://patents.justia.com/patent/5737538> |
| Wc | Wikipedia, "CompuServe Information Manager". <https://en.wikipedia.org/wiki/CompuServe_Information_Manager> |

> **About source E.** It is new (0 stars when found) and is the only detailed source for the HMI layer. Treat it as a strong lead, not ground truth: check each claim against a real WinCIM under Wine or in a VM. The repo also redistributes CompuServe's 1996 connection scripts and decompiler output. **Cite it; do not copy its files into this project.**

---

## 1. What is publicly confirmed

From the developer FAQ [F]:
- HMI is "a set of protocols" with "an application-level error-correcting transport layer".
- It avoids "pattern-matching" and allows asynchronous host data.
- The toolbox was available free, under NDA and licence. CompuServe "will not release the spec or source to the public".
- Developers were pointed to the IBM Communications Forum Lib 10 for the B Protocol, and the Graphics Support Forum Lib 16 for GIF.

From the B Plus spec [R]:
- The `+` packet's 6th field, **TL**, is 1 when an application runs under the Transport Layer.
- Transport data travels in **`M`** (more follows) and **`L`** (last of block) packets. An `F` packet ends the transport session.
- The Transport Layer is entered with a "CompuServe Application Protocol escape sequence" from the host and an escape reply from the terminal. **The bytes are not given.**
- A client advertises support with the `AP` F-code in its `ESC I` reply.

From the 1995 migration FAQ [N]:
- In 1995–96 forums moved from the 36-bit hosts to new **"NISA" hosts (Windows NT on Intel)**. These were **HMI-only**: no ASCII, and telnet terminal sessions were blocked.
- HMI clients listed: WinCIM, CSNAV, DOSCIM, OS2CIM, MacCIM, MacNAV, OzWIN, CISCOMM, TAPCIS 6.1.
- A few forums (Palmtop, Apple II, Amiga, Atari, C64) stayed on the 36-bit hosts.
- Mail got an optional, one-way HMI conversion.

So historically there were two backends: the PDP-10 hosts (ASCII + HMI) and the NISA hosts (HMI). **Our design merges them**: one core, two front ends (doc 05).

Client timeline [S, Wc]: CIM 1989 → WinCIM 1.x (1993) → 2.0.1 (1994, bundled Mosaic) → CompuServe for Windows / CS 3.0 (late 1996, the "CSi" rebrand, partly HTML) → AOL-based CompuServe 2000 / 7.0 (out of scope). **Target WinCIM 2.x and 3.0.x first.**

---

## 2. Connection and login

### 2.1 Transport [RE, E]

- WinCIM can connect over **TCP port 4144**. CIS.INI settings:
  ```
  [Connector (CIS Connection)]
  CommType=WINSOCK
  WinsockCommType=Local
  HostIPAddress=…
  ```
  SANS lists port 4144 as "wincim" (<https://isc.sans.edu/port/4144>).
- **Our server should listen on 4144 for CIM and on 23 (or 2323) for terminals.** Both run the same login state machine; see doc 05.
- Host banner: `\r\nCONNECT 9600\r\n\r\nHost Name: `.
- The client's connection scripts drive the dial-up login, so the host prompts must match exactly.

### 2.2 The script dialogue [C: CompuServe's own CSERVE.SCR v3.9.1, 1996, hosted in E]

| Host prints | Script sends |
|---|---|
| `Host Name:` | **`CPS`** by default. **`CISAGREE`** when Network = Internet. Manuals say **`CIS`**. **Accept all three.** |
| `User ID:` | `+<id><params>/INT:60 /secure:1` for secure login. The leading `+` requests SLP. Plain login: `<id>/INT:60`. Options: `/HOST` (stop at host), `/GO:PPPCONNECT` (PPP). |
| `Password:` | the password (plain login only) |
| `UIC:` | `^C` |
| `XPAD:` / `ITI:` | an X.121 address (network-specific) |

The script treats login as **successful** on any of: `CompuServe`, `ESC [ >`, `^F` (ACK), `ESC I` (answered with its ID string). `PPP:` sends it down the PPP path.

**Host error grammar.** The host prints `? LOG` or `??LOG` followed by a code. The script recognises these, so emit them verbatim:

| Code | Meaning |
|---|---|
| `INE` | Bad ID or password |
| `ISX`, `INS` | ID syntax error |
| `CAI` | Cannot auto-IRUN HMI server |
| `SIL`, `SIU`, `SNA` | System unavailable |
| `STU` | Check user ID |
| `UTL` | Too many simultaneous users |

Other messages: `? IPX` = InfoPlex unavailable, `? ICD` = PPP/gateway failure, ` NTW` = remote busy.

Network scripts: Tymnet answers its `log in:` prompt with `CPS` (`CNS` for AlaskaNet, `CNSG` for PacNet). That is not needed for TCP, but useful if we ever emulate a network node.

### 2.3 Secure Login Protocol (SLP) [RE, E: SECURE-LOGIN.md, which has test vectors]

```
H→C  "RA:" HostChallenge
C→H  "RB:" MicroChallenge
C→H  "UR:" UserResponse
H→C  "HR:" HostResponse        (or "No." on failure)
```

- `K  = MD5( UTF-16BE( lowercase(password) ) )`
- `UserResponse = MD5( K ‖ 48×0x00 ‖ H ‖ M ‖ K )`
- `HostResponse = MD5( K ‖ Z ‖ M ‖ H ‖ K )`. The meaning of `Z` must be checked in E.
- Values are Base64 plus a trailing 1-byte folded ones'-complement checksum, wrapped at 76 columns with CR.
- `DisableSecureLogin=ON` in CIS.INI falls back to plain `Password:`.

**Storage consequence:** the server needs K, not the plaintext password. Store K (an MD5 of the password) for SLP alongside a modern hash (bcrypt/argon2) for terminal logins. Both are derived at signup or password change. K is password-equivalent, so protect it like a secret.

### 2.4 Terminal negotiation, then HMI [RE, E; checksum rule cross-checked against R]

```
H→C  ESC I
C→H  #WinCIM:3.0.1:WN6,PB,DT,CA,GF,GJ,GP,GJP,GI…,+,MS,AP,PB,LA1,CH1,CO0,+4657 CR
H→C  ESC [ > 0 q
C→H  #1,2,3,6,8,10                      (supported application modes)
H→C  ESC [ > 3;3 p                      (select mode 3)
C→H  #3,2,1,4,2,0;614
H→C  ACK (0x06)                         → client switches to HMI over B+
```

`ESC [ >` is very probably the "Application Protocol escape sequence" that [R] leaves unspecified. The `AP` code in the ID string is the client's capability flag.

Server rule:
- **After login, a client whose ID string includes `AP` gets the HMI handshake.**
- A client without `AP` gets the ASCII top menu.
- The ID string's checksum follows the same rule as doc 03 §5.3.

---

## 3. B Plus as WinCIM uses it [RE, E]

- The host's `+` packet body is `04 04 02 00 00 01` (after the `+` type byte), read in Ranshaw's field order:

  | WS | WR | BS | CM | DQ | TL |
  |---|---|---|---|---|---|
  | 4 | 4 | 2 (256-byte blocks) | 0 (checksum) | 0 | **1** |

  This is consistent with [R]: windows can exceed 1, and **TL=1 must be set for HMI**. E's own field labels differ; trust Ranshaw's order and check on the wire.
- E recommends BS = 8 (1024-byte blocks), and this server sends that (§4a).
- Sequence counters are separate per direction. That matches [R]'s rule for the Transport Layer, as opposed to Classic B's single shared counter.
- **Quirk:** WinCIM drops the first host message after B+ setup, so E sends the first ack twice.
- `HMIMaxTime` in CIS.INI defaults to 120 s. Send a keepalive or ack before then.

---

## 4. HMI message layer [RE, E: HMI-PROTOKOLL.md]

Each HMI message rides in B+ `L` (or `M`…`L`) packets:

```
'L' <stream:byte> <type:byte> <fields…>
```

Field encodings:

| Kind | Encoding |
|---|---|
| byte | 1 byte |
| u16 | little-endian, 2 bytes |
| u24 | little-endian, 3 bytes |
| varint | 7 bits per byte, 2 bytes |
| text | NUL-terminated |
| date/time | DOS packed date and time |

Known request/response pairs:

| Client → host | Meaning | Host → client |
|---|---|---|
| `0x0B` | session request | `0x01` ack |
| `0x0F` | keepalive | `0x01` ack |
| `0x20` | open session | `0x21` |
| `0x24` | get page | `0x27` page, or `0x09` error |
| `0x25` | select menu item | `0x27` |
| `0x34` | continue list | `0x35` |
| `0x29` / `0x2A` | article text chunks | `0x04`… then `0x2B` at end |
| `0x0D` | directory lookup | `0x0E` |
| `0x22` | service search ("Find") | ? |
| `0x07` | logoff | — |

**Page names** use the old service page IDs: `CIS:NEWPAGE`, `CIS:CONTENTS`, `CIS:CHAT`, `CIS:FORUMS`, `HOM:MAILHMI`. A GO word the user types is prefixed with `HOM:`.

This matters for the design: **HMI and ASCII share one page/menu tree.** HMI just asks for the same pages by ID and gets structured data back instead of text.

**State of E:**
- Working: login, SLP, ESC I, B+, pages and menus, article text, mail *reading*, forums, partial Find.
- **Not done:** mail send, CB/chat, file libraries (streams 2/3/6/8), the forms layer (`FORMCB.DLL`), news (`NEWS.DLL`), and message types `0x2D`/`0x2E`.
- E counts about 190 `StartMessage` call sites in the client, so the catalogue is far from complete.

---

## 4a. What this server does (as built, milestone 7) [RE, E; tested against a fake CIM only]

Code: `internal/cis/slp.go`, `internal/cis/hmi.go`, `internal/xfer/cisbtl.go`. Test: `TestCompuServeCIM` in `internal/server`. None of it has met a real WinCIM yet; that is milestone 9.

- **Secure Login.** `+<id>` at `User ID:` starts it. The host sends a random 64 to 512-byte challenge. It checks UR against the account's kept K and answers `HR:`, or `No.` on failure. K is kept (bound to the password hash, like the OSCAR keys) whenever the user logs on with a password, or sets one, while a `compuserve` listener runs. A user with no kept K logs on once with a password (WinCIM: `DisableSecureLogin=ON` in CIS.INI, or any terminal), and is logged as such.
- **After the login:** `ESC I`, the banner (which has `CompuServe` in it), `ESC [ > 0 q`, then `ESC [ > n ; n p` with mode 3 if offered (then 2, 1, 6, 8, 10). A parameter line whose sum is wrong gets NAK and the `p` again, up to three times. A good one gets ACK.
- **Transport Layer:** ENQ (up to 5 times) until `DLE + + DLE 0`. Then the host's `+` packet `04 04 08 00 00 01` (BS 8, 1 KB blocks, as E recommends, and TL=1). The counters are separate per direction. Messages over 1023 bytes go as `M` packets and a final `L`. An `F` packet ends the session.
- **HMI:**
  - `0x20` gets `0x21`. The date is in standard DOS format; E's own packing differs, so check it on the wire.
  - `0x24` and `0x25` give a menu as `0x27`, with item kind 0 and no title or extra blocks. An article goes as `0x28`, and its text is fetched with `0x29`/`0x2A`: 700-byte `0x04` pieces, then `0x2B`. Every line starts with a space and lines are joined with CR LF.
  - `0x34` returns the rest of the items, 15 at a time.
  - `0x22` (Find) returns `0x23`: up to 12 pairs of (title, page name) from the page tree's FIND.
  - `0x0D` gets empty `0x0E` records.
  - `0x07` hangs up.
  - Everything else gets an ack on stream 1.
  - The first message after the Transport Layer starts is sent twice, 300 ms apart. Every answer waits 120 ms first, as E's does.
- **Page names:** `CIS:<ID>` and `HOM:<word>` both resolve through the ASCII service's page tree (`pages.txt`, IDs and GO words). Aliases: `NEWPAGE` → `NEW`, `CONTENTS` → `TOP`, `MAILHMI` → `MAIL`. Pages go out as `CIS:<ID>`, numbered with a 24-bit hash of the ID. `CIS:FORUMS` is the forum list. A forum, `MAIL` and `CB` are, for now, articles that say to use a terminal (milestone 8). An unknown name gets an article saying so, never nothing.

## 5. Reverse-engineering plan for the gaps

1. **Rig.** Run WinCIM 2.x and 3.0.x under Wine or in a Win9x VM, set `CommType=WINSOCK` and point it at our server. Log every byte in both directions, with B+ decoded (doc 05 §7 calls for a wire tap in the server).
2. **Cross-check E** on the wire, message by message, before relying on it. Each confirmed item goes from [RE] to [C-wire] in this doc.
3. **Disassemble** (Ghidra) `CCTN240C.DLL` (transport) and `CS3COR.DLL` (HMI messages) to find the stream/type table: the `StartMessage` call sites.
4. **Next features, in order:** mail send → file libraries (B+ downloads inside HMI) → forum message post/reply → CB chat → forms.
5. **Older clients** (DOSCIM, WinCIM 1.x, MacCIM) may predate SLP and use other mode numbers. Test them separately.

## 6. Out of scope (for now)

- CS 3.0's HTML/web content panes, which used ordinary HTTP. Add later with a small web server.
- CompuServe 2000 / 7.0, which are AOL protocols.
- PPP dial-up (`/GO:PPPCONNECT`), which is irrelevant over TCP.
