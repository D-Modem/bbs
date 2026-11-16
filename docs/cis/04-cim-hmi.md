# 04 — The GUI Service: CIM clients and HMI

The graphical clients (DOSCIM, WinCIM, MacCIM, OS/2 CIM, CSNAV/MacNAV, OzWIN, TAPCIS 6.1) did not scrape text. They used **HMI (Host-Micro Interface)**, a binary request/response protocol that runs **inside B Plus Transport Layer packets** (refer to [03](03-link-protocols.md) §4.6).

CompuServe never published the HMI specification. All data below the B+ layer comes from reverse engineering.

Tags: **[C]** confirmed (document of the period or primary document) · **[S]** secondary (Wikipedia, recollections) · **[RE]** from one reverse-engineering source only, not yet checked by us · **[GAP]** unknown.

## Sources

| Key | Source |
|---|---|
| E | `24high/CompuServe-Emulator`, a C# server (Sep 2026) that claims to bring WinCIM 3.x online. Its author recovered the data with a disassembly of WinCIM 3.0.2 (`CCTN240C.DLL`, `CS3COR.DLL`, `CS3.EXE`). Notes in German: `HMI-PROTOKOLL.md`, `SECURE-LOGIN.md`, `NAECHSTER-SCHRITT.md`, `POST.md`, `SUCHE.md`. <https://github.com/24high/CompuServe-Emulator> |
| F | CompuServe HMI developer FAQ, Nov 1994. <https://discmaster.textfiles.com/view/21536/03-Comm.zip/hmidev.txt> |
| N | Rick Wilkes, CIS HMI/NISA migration FAQ, Dec 1995. <https://discmaster.textfiles.com/view/6764/CDA1_96.ISO/novell/cishmi.faq> |
| R | Ranshaw, B Plus specification (1993). Refer to doc 03. |
| P | US Patent 5,737,538 (HMI "protocol data units"). <https://patents.justia.com/patent/5737538> |
| Wc | Wikipedia, "CompuServe Information Manager". <https://en.wikipedia.org/wiki/CompuServe_Information_Manager> |

> **About source E.** It is new (0 stars when we found it), and it is the only detailed source for the HMI layer. Use it as a strong lead, not as a proven fact: check each claim against a real WinCIM under Wine or in a VM. The repository also redistributes the 1996 connection scripts of CompuServe and decompiler output. **Cite it. Do not copy its files into this project.**

---

## 1. What is publicly confirmed

From the developer FAQ [F]:
- HMI is "a set of protocols" with "an application-level error-correcting transport layer".
- It does not use "pattern-matching", and it permits asynchronous host data.
- The toolbox was available free of charge, under NDA and licence. CompuServe "will not release the spec or source to the public".
- The FAQ sent developers to the IBM Communications Forum Lib 10 for the B Protocol, and to the Graphics Support Forum Lib 16 for GIF.

From the B Plus specification [R]:
- The 6th field of the `+` packet, **TL**, is 1 when an application runs under the Transport Layer.
- Transport data goes in **`M`** (more follows) and **`L`** (last of block) packets. An `F` packet ends the transport session.
- The host starts the Transport Layer with a "CompuServe Application Protocol escape sequence", and the terminal sends an escape reply. **The specification does not give the bytes.**
- A client tells that it supports this with the `AP` F-code in its `ESC I` reply.

From the 1995 migration FAQ [N]:
- In 1995–96, forums moved from the 36-bit hosts to new **"NISA" hosts (Windows NT on Intel)**. These were **HMI-only**: no ASCII, and telnet terminal sessions were blocked.
- HMI clients listed: WinCIM, CSNAV, DOSCIM, OS2CIM, MacCIM, MacNAV, OzWIN, CISCOMM, TAPCIS 6.1.
- Some forums (Palmtop, Apple II, Amiga, Atari, C64) stayed on the 36-bit hosts.
- Mail got an optional, one-way HMI conversion.

Thus, there were two backends: the PDP-10 hosts (ASCII + HMI) and the NISA hosts (HMI). **Our design merges them**: one core, two front ends (doc 05).

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
- **Our server must listen on 4144 for CIM and on 23 (or 2323) for terminals.** The two use the same login state machine. Refer to doc 05.
- Host banner: `\r\nCONNECT 9600\r\n\r\nHost Name: `.
- The connection scripts of the client control the dial-up login. Thus, the host prompts must be exactly the same.

### 2.2 The script dialogue [C: CompuServe's own CSERVE.SCR v3.9.1, 1996, hosted in E]

| Host prints | Script sends |
|---|---|
| `Host Name:` | **`CPS`** by default. **`CISAGREE`** when Network = Internet. Manuals say **`CIS`**. **Accept all three.** |
| `User ID:` | `+<id><params>/INT:60 /secure:1` for secure login. The `+` at the start requests SLP. Plain login: `<id>/INT:60`. Options: `/HOST` (stop at host), `/GO:PPPCONNECT` (PPP). |
| `Password:` | the password (plain login only) |
| `UIC:` | `^C` |
| `XPAD:` / `ITI:` | an X.121 address (specific to the network) |

The script accepts the login as **successful** on one of these items: `CompuServe`, `ESC [ >`, `^F` (ACK), `ESC I` (it answers with its ID string). `PPP:` sends it to the PPP path.

**Host error grammar.** The host prints `? LOG` or `??LOG`, followed by a code. The script recognises these codes. Thus, send them verbatim:

| Code | Meaning |
|---|---|
| `INE` | Bad ID or password |
| `ISX`, `INS` | ID syntax error |
| `CAI` | Cannot auto-IRUN HMI server |
| `SIL`, `SIU`, `SNA` | System unavailable |
| `STU` | Check user ID |
| `UTL` | Too many simultaneous users |

Other messages: `? IPX` = InfoPlex unavailable, `? ICD` = PPP/gateway failure, ` NTW` = remote busy.

Network scripts: Tymnet answers its `log in:` prompt with `CPS` (`CNS` for AlaskaNet, `CNSG` for PacNet). TCP does not need this. But it is useful if we emulate a network node in the future.

### 2.3 Secure Login Protocol (SLP) [RE, E: SECURE-LOGIN.md, which has test vectors]

```
H→C  "RA:" HostChallenge
C→H  "RB:" MicroChallenge
C→H  "UR:" UserResponse
H→C  "HR:" HostResponse        (or "No." on failure)
```

- `K  = MD5( UTF-16BE( lowercase(password) ) )`
- `UserResponse = MD5( K ‖ 48×0x00 ‖ H ‖ M ‖ K )`
- `HostResponse = MD5( K ‖ Z ‖ M ‖ H ‖ K )`. It is necessary to check the meaning of `Z` in E.
- Values are Base64 plus a 1-byte folded ones'-complement checksum at the end, wrapped at 76 columns with CR.
- `DisableSecureLogin=ON` in CIS.INI changes back to plain `Password:`.

**Result for storage:** the server must have K, not the plaintext password. Store K (an MD5 of the password) for SLP, together with a modern hash (bcrypt/argon2) for terminal logins. The server makes the two at signup or at a password change. K is equivalent to the password. Thus, protect it as a secret.

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

`ESC [ >` is almost certainly the "Application Protocol escape sequence" that [R] does not specify. The `AP` code in the ID string is the capability flag of the client.

Server rule:
- **After login, a client whose ID string includes `AP` gets the HMI handshake.**
- A client without `AP` gets the ASCII top menu.
- The checksum of the ID string follows the same rule as doc 03 §5.3.

---

## 3. B Plus as WinCIM uses it [RE, E]

- The body of the `+` packet of the host is `04 04 02 00 00 01` (after the `+` type byte). In the field sequence of Ranshaw, this is:

  | WS | WR | BS | CM | DQ | TL |
  |---|---|---|---|---|---|
  | 4 | 4 | 2 (256-byte blocks) | 0 (checksum) | 0 | **1** |

  This agrees with [R]: windows can be more than 1, and **TL=1 must be set for HMI**. The field labels of E are different. Trust the sequence of Ranshaw and check on the wire.
- E recommends BS = 8 (1024-byte blocks). This server sends that value (§4a).
- Each direction has its own sequence counter. That agrees with the rule of [R] for the Transport Layer. Classic B has one shared counter.
- **Quirk:** WinCIM drops the first host message after the B+ setup. Thus, E sends the first ack two times.
- `HMIMaxTime` in CIS.INI has the default 120 s. Send a keepalive or ack before then.

---

## 4. HMI message layer [RE, E: HMI-PROTOKOLL.md]

Each HMI message goes in B+ `L` (or `M`…`L`) packets:

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

**Page names** use the old page IDs of the service: `CIS:NEWPAGE`, `CIS:CONTENTS`, `CIS:CHAT`, `CIS:FORUMS`, `HOM:MAILHMI`. The client puts `HOM:` before a GO word that the user types.

This is important for the design: **HMI and ASCII share one page/menu tree.** HMI asks for the same pages by ID, and gets structured data back in place of text.

**State of E:**
- Operates: login, SLP, ESC I, B+, pages and menus, article text, mail *read*, forums, partial Find.
- **Not done:** mail send, CB/chat, file libraries (streams 2/3/6/8), the forms layer (`FORMCB.DLL`), news (`NEWS.DLL`), and message types `0x2D`/`0x2E`.
- E counts about 190 `StartMessage` call sites in the client. Thus, the catalogue is far from complete.

---

## 4a. What this server does (as built, milestone 7) [RE, E; tested against a fake CIM only]

Code: `internal/cis/slp.go`, `internal/cis/hmi.go`, `internal/xfer/cisbtl.go`. Test: `TestCompuServeCIM` in `internal/server`. We did not try any of it with a real WinCIM yet. That is milestone 9.

- **Secure Login.** `+<id>` at `User ID:` starts it. The host sends a random challenge of 64 to 512 bytes. It checks UR against the kept K of the account, and answers `HR:`, or `No.` on failure. The server keeps K (bound to the password hash, as the OSCAR keys are) each time that the user logs on with a password or sets one, while a `compuserve` listener runs. A user with no kept K must log on one time with a password (WinCIM: `DisableSecureLogin=ON` in CIS.INI, or any terminal). The log records this.
- **After the login:** `ESC I`, the banner (which contains `CompuServe`), `ESC [ > 0 q`, then `ESC [ > n ; n p` with mode 3 if the client offers it (then 2, 1, 6, 8, 10). If the sum of a parameter line is incorrect, the host sends NAK and the `p` again, up to three times. A correct line gets ACK.
- **Transport Layer:** ENQ (up to 5 times) until `DLE + + DLE 0`. Then the `+` packet of the host, `04 04 08 00 00 01` (BS 8, 1 KB blocks, as E recommends, and TL=1). Each direction has its own counter. Messages of more than 1023 bytes go as `M` packets and a final `L`. An `F` packet ends the session.
- **HMI:**
  - `0x20` gets `0x21`. The date is in standard DOS format. The packing of E is different. Thus, check it on the wire.
  - `0x24` and `0x25` give a menu as `0x27`, with item kind 0 and no title or extra blocks. An article goes as `0x28`. The client gets its text with `0x29`/`0x2A`: 700-byte `0x04` pieces, then `0x2B`. Each line starts with a space, and CR LF joins the lines.
  - `0x34` gives the remaining items, 15 at a time.
  - `0x22` (Find) gives `0x23`: up to 12 pairs of (title, page name) from the FIND of the page tree.
  - `0x0D` gets empty `0x0E` records.
  - `0x07` hangs up.
  - All other messages get an ack on stream 1.
  - The host sends the first message after the start of the Transport Layer two times, 300 ms apart. Before each answer, the host waits 120 ms, as E does.
- **Page names:** `CIS:<ID>` and `HOM:<word>` both resolve through the page tree of the ASCII service (`pages.txt`, IDs and GO words). Aliases: `NEWPAGE` → `NEW`, `CONTENTS` → `TOP`, `MAILHMI` → `MAIL`. Pages go out as `CIS:<ID>`, with a 24-bit hash of the ID as the number. An unknown name gets an article that tells this. It never gets nothing.
- **Forums and Mail (milestone 8), as pages, in the way that E measured the requests of WinCIM** (`internal/cis/hmiforum.go`):
  - `CIS:FORUMS` lists the forums.
  - `CIS:<GO>` is one forum: its sections, then its libraries.
  - `CIS:<GO>/S<n>` lists the messages of a section, newest first, up to 200.
  - `CIS:<GO>/M<number>` is one message as an article, in the header format of forum software 4. When the user reads it, the server sets the last-read mark.
  - `CIS:<GO>/L<n>` is the file list of a library, as an article.
  - `CIS:MAIL` is the mailbox: `HOM:MAILHMI` from Get Mail, newest first, with the mark `*` on unread letters.
  - `CIS:MAIL/<id>` is one letter. When the user reads it, the server marks it as read.
  - All items have kind 0. E found that kind 1 draws a label that the client will not select.
  - The server makes the pages again for each request.
- **Not yet:** mail send. E found that it is the `IInMailSendTV` transaction, sent at Send/Retrieve All. Its bytes are not recovered. Forum posts (`IFoMsgSendTV`), library transfers (`IFoLibSendTV`, streams 2/3/6/8), the CB, and the forms layer (`FORMCB.DLL`) are also not done.

## 5. Reverse-engineering plan for the gaps

1. **Rig.** Run WinCIM 2.x and 3.0.x under Wine or in a Win9x VM. Set `CommType=WINSOCK` and point it at our server. Log each byte in the two directions, with B+ decoded (doc 05 §7 asks for a wire tap in the server).
2. **Cross-check E** on the wire, message by message, before you depend on it. When you confirm an item, change its tag in this document from [RE] to [C-wire].
3. **Disassemble** (Ghidra) `CCTN240C.DLL` (transport) and `CS3COR.DLL` (HMI messages) to find the stream/type table: the `StartMessage` call sites.
4. **Next features, in this sequence:** mail send → file libraries (B+ downloads inside HMI) → forum message post/reply → CB chat → forms.
5. **Older clients** (DOSCIM, WinCIM 1.x, MacCIM) can be older than SLP and can use other mode numbers. Test them separately.

## 6. Out of scope (for now)

- The HTML/web content panes of CS 3.0, which used ordinary HTTP. Add them later with a small web server.
- CompuServe 2000 / 7.0, which use AOL protocols.
- PPP dial-up (`/GO:PPPCONNECT`), which is not applicable over TCP.
