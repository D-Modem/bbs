# Future work

Ideas not yet started, with what is known of them.

## An MSN Classic front end

*Status: started; the link, the RPC, sign-in, the directory, bulletin
boards, e-mail and its address book, chat, Find, file downloads and
the 1996 client's NTLM sign-in (`internal/marvel`, `[marvel]`,
`frontend = marvel`) are done.  Not yet tried with a real client.*

For the MSN client of Windows 95 (MSN 1.0 to 1.3), over TCP (port 569) and
by modem, from the marvel-msn project's protocol notes.  Next:

- **Uploads**: a file library as an MSN BBS whose messages carry the
  files as attachments (MOSAF objects in a rich-text body, MOS2 packed),
  which also lets a file be saved where the caller says rather than
  opened from the temporary folder.
- **MSN Today**: the Media Viewer title (MEDVIEW), a page of news.

## MSN 2.x

*Status: started; the NTLM ("MSN" security package) sign-in on mail,
news and the web forum (`internal/ntlm`, `[security] ntlm`) is done.*

For MSN 2.0 to 2.6 (Windows 95 and 98), whose programs are internet
clients.  Next:

- **The Program Viewer**: what it loads at sign-in (the start page, its
  channel list), and the MSN Sign-In dialer, found from the client.
- **DPA**: the Distributed Password Authentication package of the later
  Windows 98 mail, if it can be learned.

## An AOL front end

*Status: started; FDO91 (`internal/aol/fdo`), P3 (`internal/aol/p3`) and
the host's sign-on (`internal/aol`, `[aol]`) are done.*

For AOL 3.0 and 4.0 for Windows, the real clients, over TCP (port 5190, the
client's `AmericaOnline.aol.com`) and by modem.

- **FDO91** (done): atom streams read and written, all eight encoding
  styles and large atoms; a compiler from FDO91 source that matches
  Ada32.dll byte for byte on AtomForge's corpus (1,889 forms, 74,369
  single atoms, 44 live captures); a decompiler whose text compiles back
  to the same bytes.
- **P3** (done): packets (CRC-16/ARC, AOL 4.0's `**`), and the host's
  end of a link: INIT acknowledged, DATA in order in and within a window
  of 16 out, ACKs every 8 or soon after, NAKs and go-back-N resends,
  heartbeats after 12 seconds, 10 of them and the link is down.
- **Host** (started): `[aol]`, sharing 5190 with `[oscar]` through its P3
  hook; `Dd` signs on with a BBS account (a node, as any caller), then
  `async_online`, the client's main window (32-296) and the BBS's own
  Welcome; `pE` answered with `XS`; other tokens with `uni_wait_off`.
  Streams go as `aT`, or `at` pieces when long; the client's long ones
  are put together.  Not yet tried with a real client.
- **Chat and IMs** (done, untried with a real client): `CJ` into the
  main room in the client's room window (32-117, 4.0's 43-22783), lines
  as `AB`, comings and goings as `CA`/`CB`, `Aa` said; `iS`/`is` IMs as
  BBS private messages (kept for one away), shown in 32-223 (4.0's
  32-6736); `iO` asks who is on.
- **Keywords and mail** (done, untried with a real client): `Kk` from
  the Keyword window (de_data) or a host button (the word as it is) to
  Help, Chat, Mail, Who; `m4` MIP mail (header and text in one stream or
  two) to BBS mail; the mailbox, a mail and Who are host windows whose
  buttons send keywords; new mail counted on the Welcome window.
- **Rooms and boards** (done, untried with a real client): `LP` and
  the keywords Rooms and Room *name* to any chat room; message areas as
  boards (topics, posts, a post window whose Send is `de_ez_send_form`
  with token `Zp`), all host windows.
- **Read Mail** (done, untried with a real client): the client's own
  New, Old and Sent Mail lists (`m7`, `m9`, `m8`) as MIP lists, `m3`
  opens one, `m2` Ignore / Keep As New / Delete. The order of the atoms
  in a list entry is a guess from the mail tool's code.
- **Modem callers** (done in menu, untried with a real modem): modemmenu
  plays AOLnet, SprintNet or Tymnet as the client's CCL script expects,
  then joins the line to the host.
- **Tokens**, next: the client asks for art it lacks (`fh`): none is
  served yet.
- **To decide**: AOL 4.0's RSA handshake (revival servers turn it off in
  the client); which client forms (32-xxxx) are in its own database and
  which the host must send.

## Doors for 3270 callers: an ANSI to 3270 layer

*Status: being thought about; other ways to solve it may win.*

Doors are ANSI programs, so 3270 callers do not see the Doors menu.  A
layer between a door and a 3270 could show them:

- **Emulate, then draw.**  A small ANSI-BBS emulator (80x25: cursor
  movement, ED/EL, SGR, save/restore cursor, and answers to `ESC[6n`, which
  many doors use to detect ANSI) keeps the door's screen; every ~50 ms the
  cells that changed go to the 3270 as SBA/SA orders and characters
  (Write with keyboard restore).  go3270-sec already has the raw writes,
  the query replies and Load PS.
- **Characters.**  ASCII and Latin-1 letters are in EBCDIC CP037.  What
  CP437 has beyond them (box drawing single/double/mixed, ░▒▓█ ▀▄▌▐,
  ☺♥♦♣♠•◘○ and arrows: about 70 glyphs) goes in one programmed-symbol set,
  drawn by program at the terminal's cell size (from its query reply), as
  the logo is: no font file.  Each cell selects the set with an SA
  character-set attribute, which takes no screen position.  Without
  programmed symbols (x3270, wc3270, Vista): GE / CP310 line drawing and
  ASCII for the blocks.
- **Colours.**  Seven 3270 colours for ANSI's eight; bright and normal the
  same (or the intensified attribute for bold); ANSI backgrounds by the SA
  background-colour attribute where the emulator has it (PCOMM, it
  seems), else reverse video.  Blink and underscore map directly.
- **Input: the hard part.**  A 3270 sends nothing until Enter, a PF/PA key
  or Clear.  An unformatted screen lets the caller type at the door's
  cursor; on Enter the BBS diffs the screen against what it drew to find
  the keys, and sends them and a CR.  PF keys for arrows, PgUp/PgDn,
  Home/End; PA1 for Esc; an empty Enter for a lone CR.  Menu-driven doors
  (LORD, BRE, Usurper, TradeWars) would be fine; hotkey and real-time ones
  sluggish.
- **Size.**  Doors want 25 lines: a model 3 (32x80) or bigger fits, with
  room for a help line; a model 2 (24x80) loses or scrolls one.
- **Testing.**  The emulator, diffing and data stream by unit tests; the
  fallback path with s3270; a whole DOS door session by a scripted 3270
  client.  Programmed symbols only against a fake terminal here: PCOMM to
  be tried by hand.
- **Shape.**  An option on a `[door]` (`tn3270 = yes`) that shows the door
  on the 3270 Doors menu and puts this layer between them.
