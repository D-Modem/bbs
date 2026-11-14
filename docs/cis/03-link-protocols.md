# 03 — Link Protocols: A, B, B Plus, ESC I, VIDTEX

These protocols run *inside* the character stream of a terminal session. The host changes to them for file transfers, terminal identification and graphics. They also carry the HMI traffic of the GUI clients (refer to [04](04-cim-hmi.md)). A new implementation must have a correct B+ engine. Without it, no downloads operate, and CIM does not operate at all.

Confidence tags: **[C]** confirmed from a specification or reference code · **[I]** inferred.

## Sources

| Key | Source |
|---|---|
| R | Russ Ranshaw, *The CompuServe B Plus Protocol*, 18 Nov 1993. The specification from CompuServe. <https://discmaster.textfiles.com/view/16904/SOS-DFUE.ISO/programm/texte/bplus.doc> |
| W | Steve Wilhite, `bp.c` / `dte.c` (1985), Classic B reference code. comp.sources.unix, Mar 1986. <https://www.tuhs.org/Usenet/comp.sources.unix/1986-March/002804.html> |
| O | Steve Sneed, OOBPLUS 1.0 (1991), the Pascal B+ that OzCIS uses. <https://discmaster.textfiles.com/view/16184/MAX_PROGRAMMERS.iso/PASCAL/OOBPLS10.ZIP> |
| A | *Protocol Notes: CIS A Protocol* (1982/83). <https://discmaster.textfiles.com/view/19793/2015.02.ftp.barnyard.co.uk.tar/ftp.barnyard.co.uk/cpm/walnut-creek-CDROM/CPM/CIS/PROTCISA.TXT> |
| T | Tandy, *TRS-80 Videotex Software Specifications (26-5000)*. <https://colorcomputerarchive.com/repo/Documents/Manuals/Hardware/TRS-80%20Videotex%20Software%20Specifications%20(Tandy).pdf> |
| M | *Antic*, Aug 1986, "Going Online" (verbatim download transcript). <https://www.atarimagazines.com/v5n4/GoingOnline.html> |
| P | HP Palmtop Paper CIS forum guide. <https://www.palmtoppaper.com/ptphtml/1/ptp10025.htm> |
| Wk | Wikipedia, "B protocol". <https://en.wikipedia.org/wiki/B_protocol> |

We calculated the two check values of the specification again, and they agree with R. The sample packet `10 42 37 54 44 41 53 2E 43 03` gives the standard checksum **0x2A** and the XMODEM-CRC (init 0xFFFF) **0x57FF**. Use them as unit-test vectors.

Byte names in this document: `SOH`=01, `ETX`=03, `EOT`=04, `ENQ`=05, `SI`=0F, `SO`=0E, `DLE`=10, `DC1`=11, `DC3`=13, `NAK`=15, `ESC`=1B, `RS`=1E.

---

## 1. Lineage

| Protocol | Era | Notes |
|---|---|---|
| A | 1980–82 | The first protocol. Related to the AgVision/VideoTex projects [Wk]. B replaced it in 1982 [R]. |
| Classic B | 1982– | Stop-and-wait, 512-byte buffer, 8-bit additive checksum. |
| Interim B / "Quick B" | mid-80s | Adds `+` parameter negotiation and send-ahead. [O] calls a session "Quick B" after it sees a `+` packet. [I] |
| B Plus (B+) | late 80s–90s | Full negotiation: windows, 2 KB blocks, CRC-16/32, configurable quoting, download resume, file info, transport layer. **WinCIM and OzCIS use this.** |

---

## 2. A protocol [C, A]

- 8N1. The host goes into protocol mode with **SI** and leaves it with **SO**. The terminal removes parity when it looks for SI.
- Host commands inside protocol mode:
  - `ESC I`: identify (§5)
  - `ESC A`: start a transfer
  - `ESC L`: CP/M memory load
- ACK = `.` (2E), NAK = `/` (2F). If no answer comes back, the host sends ETX again up to 4 times, then aborts. Ctrl-U from the terminal aborts.
- Header record: `SOH <rec#> <U|D> <A|B> <filespec> CR [LF] ETX <ck>`. Data records: `SOH <rec#> <data> ETX <ck>`. The last data record has `EOT` immediately before ETX. SO ends the session.
- Record numbers are ASCII `1`…`9`,`0`,`1`… [I]
- Masking: the sender sends a data byte < 0x20 as `DLE, b+0x40`. It sends framing characters raw.
- Checksum: the same rotate-add-carry as B (§3.3). It covers the bytes after SOH, without ETX, and is calculated on unmasked values. A checksum < 0x20 is masked.

Priority for us: **low**. Implement it only for completeness (8-bit clients of the period).

---

## 3. Classic B

### 3.1 Initiation [C, R §D/§L, W]

The host always starts. While the terminal is in plain terminal mode, the host sends **ENQ**. The reply of the terminal tells the host which B version the terminal uses:

| Client | Reply to ENQ | Bytes |
|---|---|---|
| Classic B | `DLE '0'` | `10 30` |
| Interim B | `DLE '+' DLE '0'` | `10 2B 10 30` |
| B Plus | `DLE '+' '+' DLE '0'` | `10 2B 2B 10 30` |
| B Plus, relaxed timing | `DLE '+' '+' 'R' DLE '0'` | `10 2B 2B 52 10 30` (host doubles its timeouts) |

On ENQ, the terminal resets to seq `'0'`, a 512-byte buffer, the standard checksum and the default quote set, and **stays in terminal mode**. It goes into protocol mode only when it sees `DLE 'B'`.

Host loop: send ENQ, a maximum of 5 tries. Parse the reply:
- Each `+` before the `DLE digit` sets `PlusSeen`, then `PlusPlus`.
- `R` sets relaxed timing.
- The digit is the start value of the sequence numbers.

Then, if the host saw a `+`, it sends a `+` packet (§4). If not, it goes directly to the `T` packet.

### 3.2 Packet framing [C, R §C1, W]

```
DLE 'B' <seq> <type> <body…quoted> ETX <check…quoted> [RS]
10  42  30–39
```

- **seq**: one ASCII digit. After `'9'`, it goes back to `'0'`. The first packet of the host after the ENQ reply `DLE '0'` is `'1'`. The two directions share the counter: the receiver uses the received seq, and its next sent packet uses seq+1 [I from W/O]. The Transport Layer (§4.6) keeps separate counters.
- **check**: covers seq + type + body + ETX, calculated on the **unquoted** bytes (the quoting DLEs are not included). If the check bytes are in the quote set, they are also quoted.
- **size**: Classic B has a 512-byte buffer that *includes* the type byte. Thus, there is a maximum of 511 data bytes for each `N`. B+ permits 0–2048 data bytes.
- **RS** follows the check value only when CM is 2 or 3 (§4.3).

### 3.3 Standard checksum [C, R §M, W]

```go
func bChecksum(p []byte) byte {
    ck := 0
    for _, b := range p {
        ck <<= 1
        if ck > 0xFF { ck = (ck & 0xFF) + 1 } // 8-bit rotate
        ck += int(b)
        if ck > 0xFF { ck = (ck & 0xFF) + 1 } // end-around carry
    }
    return byte(ck)
}
```

### 3.4 Quoting [C, R §C2, W]

- Encode 0x00–0x1F as `DLE, c+0x40` (ETX becomes `10 43`).
- Encode 0x80–0x9F as `DLE, (c&0x1F)+0x60` (0x93 becomes `10 73`).
- Decode: after a DLE, if the next byte < 0x60, the value is `c & 0x1F`. If not, it is `(c & 0x1F) | 0x80`. **Decoders must accept a quoted byte also if it is not in the negotiated set.**

Default sets:
- The Classic sender of Wilhite quotes `03 05 10 11 13 15`.
- R §L lists all of 0x00–0x1F as the Classic default.
- The B+ set of the CIS host is `03 05 10 11 13 15 1E 91 93`.

**Host policy [I]:** quote all of 0x00–0x1F for Classic clients. That is always safe.

### 3.5 Packet types [C, R §C4, W, O]

| Type | Body | Meaning |
|---|---|---|
| `T` | `<dir><dtype><filename>` | Transfer control. dir: `D` download (host→term), `U` upload, `C` close/EOF. dtype: `A` 7-bit ASCII (EOL conversion permitted), `B` binary, `I` image. Examples: `TDBFOO.ZIP`, `TUAREADME.TXT`, `TC`. |
| `N` | data | File data. A zero-length `N` is permitted and is **not** EOF. |
| `F` | `<code>[text]` | Failure. The receiver ignores the seq. Codes: `A` user abort, `C` capacity, `E` processing error, `I` I/O, `M` upload file missing, `N` unimplemented packet type, `S` sequence, `r` resume failure. The receiver ACKs it. Then the two sides leave protocol mode. |
| `+` | parameters | B+/Interim negotiation (§4.1). |
| `M` / `L` | data | Transport Layer: `M` more data follows, `L` last packet of a block. |
| `Tr` `Tf` `TI` | — | B+ resume and file info (§4.5). |

### 3.6 Control sequences [C, R §D, W]

| Sequence | Bytes | Meaning |
|---|---|---|
| ACK | `DLE <digit>` | Last good seq received |
| NAK | `15` | Bad check or unexpected seq |
| WACK | `DLE ';'` (`10 3B`) | Wait. The sender pauses (W sleeps 5 s). |
| ENQ in session | `05` | Peer replies `DLE <last seq>` |
| Duplicate packet | — | ACK it again, discard it |
| Panic abort | `10 10 10 10` | Four DLEs from the terminal |
| Fallback detect | `DLE '+'` while the sender waits for an ACK | The terminal went back to terminal mode: abort |

- Response to a NAK: Classic sends the last packet again. Interim and B+ send `ENQ ENQ`, wait for two identical ACKs, then send again all packets that are not acknowledged.
- Timeouts: 10 s for each character, a maximum of 10 errors. On a slow link, the receiver must not send NAK on timeout.

### 3.7 Flows [I from W/O, consistent with R]

Download:
```
H→T  05
T→H  10 30                         (or 10 2B 2B 10 30 → §4.2 negotiation first)
H→T  DLE B '1' "TDB" name ETX ck
T→H  10 31                         client opened the file (or sends F)
H→T  DLE B '2' 'N' data… ETX ck
T→H  10 32                         … repeat …
H→T  DLE B 'n' "TC" ETX ck
T→H  DLE 'n'                       done; client is back in terminal mode
```

Upload: the host sends `TU<A|B><name>`. The **client does not ACK `TU`**: its first `N` packet (seq+1) is the implicit acknowledgement. The host ACKs each `N`. The client ends with `TC`, and the host ACKs it.

---

## 4. B Plus extensions [C, R unless noted]

### 4.1 Transport Parameters packet (`+`)

- The sender always sends it with the standard checksum.
- While it is in transit, B+ quotes 0x00–0x1F **and** 0x80–0x9F (Interim quotes 0x00–0x1F only).
- Body = binary bytes in this sequence. Missing fields at the end have the default 0. The receiver ignores extra fields.

| # | Field | Values |
|---|---|---|
| 1 | WS | Window Send: 0 = one outstanding, 1 = two … |
| 2 | WR | Window Receive, same encoding |
| 3 | BS | Block size ÷ 128: 0 or 4 = 512, 8 = 1024, 16 = 2048. The text of R says "0x16" for 2048. That is a typing error. |
| 4 | CM | Check method: 0 standard, 1 XMODEM CRC-16, 2 CCITT CRC-16, 3 CRC-32 |
| 5 | DQ | Obsolete quote level, ignored if Q1–Q8 are present. 0 = {00 03 05 10 11 13 15}; 1 = {03 05 10 11 13 15}; 2 = level 1 + {91 93}; 3 = all of 0x00–0x1F and 0x80–0x9F |
| 6 | TL | 1 = Transport Layer supported |
| 7–10 | Q1–Q4 | Bitmask for 0x00–0x1F. Q1 bit7 = 0x00 … bit0 = 0x07; Q2 = 08–0F; Q3 = 10–17; Q4 = 18–1F |
| 11–14 | Q5–Q8 | Same bitmask for 0x80–0x9F |
| 15 | DR | Download resume: 0 none, 1 `Tr`, 2 `Tr` + `Tf` |
| 16 | UR | Upload resume. Not described. [O] says "not supported by CIS". |
| 17 | FI | 1 = `TI` file-info packet supported |

- **CIS host values:** Q1–Q4 = `14 00 D4 02`, Q5–Q8 = `00 00 50 00`. That decodes to {03 05 10 11 13 15 1E 91 93}.
- **OzCIS client values [O]:** WS=1 WR=2 BS=4 CM=1 DQ=1 TL=0 Q=`14 00 D4 00 00 00 00 00` UR=0 FI=1.

### 4.2 Negotiation

```
H→T  ENQ
T→H  10 2B 2B 10 30
H→T  DLE B '1' '+' <host params> ETX ck
T→H  DLE B '2' '+' <client params> ETX ck     (implicit ACK of '1') [I]
H→T  10 32
```

Then the two sides calculate the same result:
- `sendWin = min(ownWS, peerWR)` and `recvWin = min(ownWR, peerWS)` (note the cross).
- `min()` of BS, CM, TL, DR, UR and FI.
- **Bitwise OR** of the quote masks.

The next packet already uses the negotiated values.

### 4.3 Check methods [C, R §C3/§M]

| CM | Algorithm | Wire |
|---|---|---|
| 0 | §3.3 checksum | 1 byte |
| 1 | CRC-16 poly 0x1021, MSB-first, **init 0xFFFF**. `crc = tab[((crc>>8)^b)&0xFF] ^ (crc<<8)`. CRC over data + received CRC = 0. | 2 bytes, high byte first |
| 2 | CCITT CRC-16, reflected poly 0x8408, preset 0xFFFF. `crc = tab[(crc^b)&0xFF] ^ (crc>>8)`. Send the complement, low byte first. Receiver residue 0xF0B8. | 2 bytes + RS |
| 3 | CRC-32 (zlib/PKZIP, 0xEDB88320), preset −1, complemented, LSB first. Residue 0xDEBB20E3. | 4 bytes + RS |

In Go: CM=3 is `hash/crc32.ChecksumIEEE`. CM=1 is CRC-16/CCITT-FALSE. CM=2 is CRC-16/X-25.

### 4.4 Sliding window [C, R §H/§I]

- The sender keeps one buffer for each packet that is not acknowledged. If the window is full, it waits for an ACK.
- An ACK for an older packet means that ACKs were lost: release all packets up to and including that packet.
- On a NAK: `ENQ ENQ` resync, then send again all packets that are not acknowledged.
- Back-off: +3 for each retransmit, −1 for each ACK. At ≥ 12, change to stop-and-wait.
- Recommended block size by speed: 300 bps → 128, 1200 → 512, 2400 → 1024, 9600 → 2048 with a window of 4–5. Over telnet, use 2048 with the window that the client offers.

### 4.5 Resume and file info

- **`Tr<len> <crc> `**: the client sends it as the reply to `TD` when it has a partial file. The fields are ASCII decimal, each followed by a space. The CRC uses the algorithm of the session, or XMODEM CRC-16 init −1 if the session uses the plain checksum.
  - If the CRC of the host over its first `len` bytes agrees, the host continues with `N` packets from that offset. If the file is already complete, it sends `TC`.
  - If it does not agree: `Fr` if DR=1, or `Tf` if DR=2 (the client ACKs, then the full file follows).
- **`TI`**: the file sender sends it when FI=1. Body: `TI <dtype> <compression=0x00> <length> <tz> <cdate yyyymmdd> <ctime secs> <mdate> <mtime> <namelen as a binary byte> <truename>`, ASCII decimal with spaces between the fields. Example from the specification: `A\x0055387 300 19880422 52480 0 0 \x09BPLUS.DOC`.

### 4.6 Transport Layer (TL=1)

- An application escape sequence starts it. **R does not document that escape sequence.**
- It uses separate send and receive sequence counters, carries data in `M`/`L` packets, and ends with `F`.
- This is the carrier for CIM/HMI. A reverse-engineered WinCIM 3 server reports host params `04 04 02 00 00 01` (TL=1), and HMI messages in `L` packets, started through `ESC [ > … p`. Refer to [04](04-cim-hmi.md) §2.4–§4.

### 4.7 Line requirements

- An 8-bit clean path is necessary. The client must obey XON/XOFF from the host. For this reason, 11, 13, 91 and 93 are always quoted.
- **Over telnet [I]:**
  - Negotiate `WILL/DO BINARY` in the two directions.
  - Double each 0xFF data byte (0xFF is not in a quote set).
  - Remove the NUL from CR-NUL on input.
  - If not, B+ streams become corrupted.

---

## 5. Terminal identification: ESC I

### 5.1 Original VIDTEX [C, T]

The host sends `ESC I`. A Tandy VIDTEX terminal answers `RS4` + CR.

### 5.2 CIS 8-bit era [C, A]

Example reply: `#CPM MYSYSTEM,CC,HC,PA,PL,` + CR.
- `CC` = cursor control, `HC` = hard copy, `PA` = A protocol, `PL` = L protocol.
- The older MNEXEC form was `<lo page>,<hi page>,<system>,<&CPM>,<D>`.

### 5.3 B Plus era [C, R §N]

Sequence: the host usually does the **ENQ test first**. Then it sends `ESC I` if the client uses B.

```
'#' <PID>:<Ver>:<OS> ',' {Fcode ','}* '+' <checksum decimal> CR
e.g.  #WINCIM:1.0.b7:W31,…   (PID ≤6, Ver ≤12, OS ≤3 chars)
```

| Group | Codes |
|---|---|
| Screen | `AC` ANSI colour · `CA` ANSI/VT100 cursor · `CC` VIDTEX/VT52 cursor · `CW` wide characters (`ESC m` on / `ESC l` off) · `SSyx` screen size (y = lines+31, x = cols+31, for example `SS7o` = 24×80. CIS does not use it) |
| Graphics | `GF` GIF · `GH` hi-res RLE · `GM` med-res RLE · `GJ` JPEG · `NF` NAPLPS · `GIvcpwwwhhh` GIF capabilities |
| Protocols | `AP` Application Protocol (**the HMI flag** [I]) · `CB` capture buffer · `PB` B protocol · `PX` XMODEM |
| International | `CHd` charset · `COddd` country · `LAd` language |
| Misc | `HC` hard copy · `XX` ignored · `DT` seen in real replies, meaning not documented |

**Checksum:** the 16-bit sum of all characters from `#` through `+`, sent as ASCII decimal. If the sum does not agree, the host sends `ESC I` again and compares consecutive replies, up to 10 tries. The example of the specification (`#PC3:3.2a:D,CC,GH,GM,GF,PB,+1035`) does **not** agree: the plain sum is 1680. OzCIS uses the plain sum.

**Host policy [I]:**
- Accept the reply if the sum agrees, or if two consecutive replies are identical.
- Use the ENQ reply as the authoritative B test. `PB` is only advisory.
- Parse leniently: `#`, then tokens with commas between them, with the product id first.

Real replies:
- Wilhite (before B+): `#IB1,PB,DT\r`
- OzCIS: `#OZ3,OzCIS1,AC,CA,SSxx,GF,PB,DT,+<sum>\r`

### 5.4 GIF interrogation [C as client behaviour, O]

- The host sends `ESC [ > 0 g`. The client replies with a capability string, for example `#89a;1;0,320,200,4,0;0,640,200,2,2;…\r`.
- `ESC [ > 1 g` tells the client that a GIF comes. The GIF then arrives as a B+ download (`+`, then `T…`) directly into the viewer. The host waits for a CR after the client shows the image.

---

## 6. VIDTEX host escape sequences [C, T]

| Bytes | Function |
|---|---|
| `ESC A` / `ESC B` / `ESC C` / `ESC D` | Cursor up / down / right / left |
| `ESC H` | Home |
| `ESC J` | Clear to end of page |
| `ESC K` | Clear to end of line |
| `ESC Y <row+0x20> <col+0x20>` | Position cursor (`1B 59 24 26` = row 4, col 6) |
| `ESC j` | Clear page, cursor stays |
| `` ESC ` <c> `` | Change cursor symbol |
| `ESC G 4` | Semigraphics-4 mode |
| `ESC G M` | Medium-res RLE graphics |
| `ESC G H` | High-res RLE (256×192) (secondary source) |
| `ESC G N` | Back to alphanumeric mode |
| `0C` / `0D` / `0A` / `09` / `08` | Clear screen / CR / LF / tab / backspace |

**RLE graphics:**
- After `ESC G M` or `ESC G H`, the data is byte pairs `(count_off+0x20, count_on+0x20)`. They are a run-length encoding of a monochrome image.
- In graphics mode, the terminal recognises only `ESC G N`.

The VIDTEX set is almost the same as VT52. Thus, the host can use the same cursor codes for clients that report `CC`.

---

## 7. How the host triggers a transfer (terminal UI)

### 7.1 Verbatim 1986 forum download [C, M]

```
 1 XMODEM (MODEM7) protocol
 2 CompuServe 'B' protocol
 3 CompuServe 'A' protocol
 4 DC2/DC4 CAPTURE protocol
 0 Abort transfer request
Enter choice: 1
Starting XMODEM transfer
Enter a carriage return when
transfer is complete
```

### 7.2 Later additions [C, P]

By about 1991, command mode offered Xmodem, Kermit and CIS-B (`LIB n`, `BRO`, `DIR`, `DOW <file>`). YMODEM probably came later. [I]

### 7.3 Host mechanics

| Choice | Host action |
|---|---|
| CIS B | Immediately ENQ → (`+` negotiation) → `TD…`. The user does not have to do an action: clients start automatically on `DLE B`. |
| XMODEM / Kermit | Print the start message, run the standard protocol, then wait for CR. |
| DC2/DC4 capture | `DC2` (12), raw text, `DC4` (14). Conventional capture on/off. [I] |
| A | `SI ESC A …` as in §2 |

---

## 8. Gaps

- We found no public modern B+ implementation. A FreeBSD port `bpl+` (1989) exists, but we could not get it. **[O] and [W] are the best references to port into Go.**
- R does not document the start escape of the Application Protocol / Transport Layer. A 2026 reverse-engineering project reports `ESC [ > 0 q` / `ESC [ > 3;3 p`, then ACK (doc 04 §2.4). Confirm this on the wire.
- The meaning of `DT` is not known. The full VIDTEX 4.x escape set is also not known.
- Not retrieved: the Dr. Dobb's article on B, Jul 1986 (Levi Thomas & Nick Turner, vol. 11 no. 7, pp. 54–59).
