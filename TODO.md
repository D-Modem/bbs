# Future work

This file lists ideas that are not started yet, with the known facts
about them.

## An MSN Classic front end

*Status: started. These parts are complete: the link, the RPC, sign-in,
the directory, bulletin boards, e-mail and its address book, chat, Find,
file downloads and the NTLM sign-in of the 1996 client (`internal/marvel`,
`[marvel]`, `frontend = marvel`). We did not try it with a real client
yet.*

This front end is for the MSN client of Windows 95 (MSN 1.0 to 1.3), over
TCP (port 569) and by modem. It uses the protocol notes of the marvel-msn
project. The next tasks are:

- **Uploads**: a file library as an MSN BBS. Its messages contain the
  files as attachments (MOSAF objects in a rich-text body, MOS2 packed).
  This also lets the caller save a file in a location that the caller
  selects. Now the client opens the file from the temporary folder.
- **MSN Today**: the Media Viewer title (MEDVIEW), a page of news.

## MSN 2.x

*Status: started. The NTLM ("MSN" security package) sign-in on mail,
news and the web forum (`internal/ntlm`, `[security] ntlm`) is complete.*

This work is for MSN 2.0 to 2.6 (Windows 95 and 98). The programs of
these versions are internet clients. The next tasks are:

- **The Program Viewer**: find from the client what the Program Viewer
  loads at sign-in (the start page and its channel list), and the MSN
  Sign-In dialer.
- **DPA**: the Distributed Password Authentication package of the later
  Windows 98 mail, if we can learn how it operates.

## An AOL front end

*Status: started. FDO91 (`internal/aol/fdo`), P3 (`internal/aol/p3`) and
the sign-on of the host (`internal/aol`, `[aol]`) are complete.*

This front end is for AOL 3.0 and 4.0 for Windows, the real clients. It
operates over TCP (port 5190, the `AmericaOnline.aol.com` of the client)
and by modem.

- **FDO91** (complete): bbs reads and writes atom streams, with all eight
  encoding styles and large atoms. A compiler from FDO91 source gives the
  same bytes as Ada32.dll on the corpus of AtomForge (1,889 forms, 74,369
  single atoms, 44 live captures). The text from the decompiler compiles
  back to the same bytes.
- **P3** (complete): packets (CRC-16/ARC, the `**` of AOL 4.0), and the
  host end of a link. bbs acknowledges INIT. DATA comes in, in sequence,
  and goes out in a window of 16. bbs sends ACKs after each 8 packets or
  after a short time. It sends NAKs and does go-back-N resends. It sends
  heartbeats after 12 seconds. After 10 of them, the link is down.
- **Host** (started): `[aol]` shares 5190 with `[oscar]` through its P3
  hook. `Dd` signs on with a BBS account (a node, as for all callers).
  Then bbs sends `async_online`, the main window of the client (32-296)
  and the Welcome of the BBS. bbs answers `pE` with `XS`. It answers
  other tokens with `uni_wait_off`. Streams go as `aT`, or as `at` pieces
  when they are long. bbs joins the long streams of the client. We did
  not try it with a real client yet.
- **Chat and IMs** (complete, not tried with a real client): `CJ` puts
  the caller into the main room, in the room window of the client
  (32-117, 43-22783 in 4.0). Lines go as `AB`. Arrivals and departures go
  as `CA`/`CB`. bbs says `Aa`. `iS`/`is` IMs become BBS private messages
  (kept for one caller who is away). They show in 32-223 (32-6736 in
  4.0). `iO` asks which callers are on.
- **Keywords and mail** (complete, not tried with a real client): `Kk`
  comes from the Keyword window (de_data) or from a host button (the word
  as it is). It goes to Help, Chat, Mail or Who. `m4` MIP mail (header
  and text in one stream or two) becomes BBS mail. The mailbox, a mail
  and Who are host windows. Their buttons send keywords. The Welcome
  window shows the count of new mail.
- **Rooms and boards** (complete, not tried with a real client): `LP`
  and the keywords Rooms and Room *name* go to a chat room. Message areas
  show as boards (topics, posts, and a post window whose Send is
  `de_ez_send_form` with token `Zp`). All of these are host windows.
- **Read Mail** (complete, not tried with a real client): bbs sends the
  New, Old and Sent Mail lists of the client (`m7`, `m9`, `m8`) as MIP
  lists. `m3` opens one mail. `m2` does Ignore / Keep As New / Delete.
  The sequence of the atoms in a list entry is a guess from the code of
  the mail tool.
- **Modem callers** (complete in menu, not tried with a real modem):
  modemmenu emulates AOLnet, SprintNet or Tymnet, as the CCL script of the
  client expects. Then it connects the line to the host.
- **Tokens**, the next task: the client asks for art that it does not
  have (`fh`). bbs does not serve art yet.
- **Open decisions**: the RSA handshake of AOL 4.0 (revival servers
  disable it in the client). Also, which client forms (32-xxxx) are in
  the database of the client, and which forms the host must send.

## Doors for 3270 callers: an ANSI to 3270 layer

*Status: under study. Other solutions are possible.*

Doors are ANSI programs. Thus 3270 callers do not see the Doors menu. A
layer between a door and a 3270 can show the doors:

- **Emulate, then draw.**  A small ANSI-BBS emulator keeps the screen of
  the door. It has 80x25 cells, and it does cursor movement, ED/EL, SGR,
  save/restore cursor, and answers to `ESC[6n`. Many doors use this request
  to detect ANSI. At intervals of about 50 ms, the changed cells go to
  the 3270 as SBA/SA orders and characters (Write with keyboard restore).
  go3270-sec already has the raw writes, the query replies and Load PS.
- **Characters.**  ASCII and Latin-1 letters are in EBCDIC CP037. CP437
  has more characters than these: box drawing (single, double and mixed),
  ░▒▓█ ▀▄▌▐, ☺♥♦♣♠•◘○ and arrows. These are about 70 glyphs. They go in
  one programmed-symbol set. A program draws them at the cell size of
  the terminal (from its query reply), as it draws the logo. No font
  file is necessary. Each cell selects the set with an SA character-set
  attribute. This attribute does not use a screen position. If the
  terminal has no programmed symbols (x3270, wc3270, Vista), use GE /
  CP310 line drawing, and ASCII for the blocks.
- **Colours.**  3270 has seven colours for the eight colours of ANSI.
  Bright and normal are the same (or bold uses the intensified
  attribute). ANSI backgrounds use the SA background-colour attribute if
  the emulator has it (we think that PCOMM has it). If not, they use
  reverse video. Blink and underscore map directly.
- **Input: the difficult part.**  A 3270 sends nothing until the caller
  pushes Enter, a PF/PA key or Clear. An unformatted screen lets the
  caller type at the cursor of the door. On Enter, the BBS compares the
  screen with the screen that it drew, to find the keys. Then it sends
  the keys and a CR. PF keys are for arrows, PgUp/PgDn and Home/End. PA1
  is for Esc. An empty Enter is for a CR alone. Doors that use menus
  (LORD, BRE, Usurper, TradeWars) will operate correctly. Doors that use
  hotkeys and real-time doors will be slow.
- **Size.**  Doors use 25 lines. A model 3 (32x80) or larger has
  sufficient space, with space for a help line. A model 2 (24x80) loses
  one line or scrolls it.
- **Tests.**  Unit tests for the emulator, the comparison and the data
  stream. Tests with s3270 for the fallback path. A test of a full DOS
  door session with a scripted 3270 client. Tests of programmed symbols
  only against a simulated terminal here. Someone must try PCOMM
  manually.
- **Shape.**  An option on a `[door]` (`tn3270 = yes`). This option shows
  the door on the 3270 Doors menu and puts this layer between them.
