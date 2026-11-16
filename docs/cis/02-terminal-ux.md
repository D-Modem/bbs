# 02 — The Terminal (ASCII) Experience

This document records what a caller with a plain terminal saw. It is as near to byte-exact as the session captures that exist permit.

Offline readers (TAPCIS, AutoSIG, OzCIS, Navigator) **pattern-match these strings**. Thus, the verbatim text is an interface contract. Do not change it.

Tags: **[V]** verbatim from a capture or period document (blank-line runs and ANSI clear codes stripped) · **[P]** paraphrased from a period source · **[I]** inferred.

## Sources

| ID | Source |
|---|---|
| S1 | 1989 logon→logoff via Telepac. <https://discmaster.textfiles.com/view/12465/floppyshareware.zip/floppyshareware/GLEN/TMC.ZIP/89-08-09.L0G> |
| S2 | 1987 CIS Navigator (Mac) session log. <https://discmaster.textfiles.com/view/11599/www.compmuseum.org.tar/www.compmuseum.org/upload/mac/diskimages/Utils%2012.image.hqx/Utils%2012.image/Navig/CIS> |
| S3–S5 | 1988 menu-mode forum, BROWSE and EasyPlex captures. <https://discmaster.textfiles.com/view/18187/Wonky_Flux_Batch_2019-02.zip/Wonky%20Flux%20Batch%202019-02/091%20-%20Compuserve%20EBU%202.dsk/JFORUM15TH.txt> (and `BROwse.DL11.txt`, `EASYPLEX.JAN.txt` in the same folder) |
| S6–S8 | 1993–94 Mac captures (logon, Mail, forum JOIN, RATES, BILLING, command summary). Discmaster item 19202 (`King.img`, `Alexander.img`). |
| S9 | 1995 Navigator session. Discmaster item 20868. |
| S10 | TAPCIS 5.3 (1990): manual TAPDOC.1–10, PARAMS.SCR, CIS.SCR, TYMNET.SCR, SPRINT24.SCR, strings in TAPCIS.EXE. <https://discmaster.textfiles.com/browse/14533/Megaware%20Volume%201.iso/communic/tapcis> |
| S19 | TAPCIS 5.42 (1994): PARAMS.SCR (5.4, 12/10/91), FRONTEND.SCR, TAPCIS.HLP, strings in TAPCIS.EXE. <https://archive.org/details/TAP542_ZIP> |
| S11 | *Forum Software Version 4D Complete Command Listing*, May 1989. <https://discmaster.textfiles.com/view/17620/archives.tar/archives/compuserve-file-archive/02%20Help%20%26%20Utilities/CMDLST.TXT> |
| S12 | IBM PC SIG help text, Nov 1983. <https://discmaster.textfiles.com/view/10621/The%20PC-SIG%20Library%20on%20CD%20ROM%20-%20Ninth%20Edition.iso/001_100/DISK0099/DISK0099.ZIP/CS-IBM.SIG> |
| S13 | Conference/CB help, 1982–84. <https://discmaster.textfiles.com/view/6428/C.A.R.R.S.%20CD-ROM%20(1990).ISO/020a/symp_6.zip/CONFER.HLP> |
| S14 | 1989 forum conference log. <https://discmaster.textfiles.com/view/1969/RBBSIABOX31.cdr/zzap/zipcis.zip/ZIP.LOG> |
| S15 | 1996 forum conference log. <https://discmaster.textfiles.com/view/32223/CD_shareware_12-96.iso/WIN/Juegos/CIV2CON.ZIP/CIV2.TXT> |
| S16 | 1992 forum thread with exact headers. <http://www.textfiles.com/messages/zachman1.msg> |
| S17 | 1980s text files on IDs, CB, logon. <http://www.textfiles.com/hacking/cisinfo4.txt>, `compusrv`, `cis.phk`, `compusin.txt` |
| S18 | Early-80s GO page index. <http://www.textfiles.com/hacking/compuser.txt> |

Local copies of the captures are in the session scratchpad. They are not in the repository. For golden-transcript tests, download them again from the URLs above.

---

## 1. Connecting and logging in

### 1.1 Getting to the host

1. The modem connects: `CONNECT 2400`.
2. On the CompuServe network, the caller sends **Ctrl-C**, or a CR on "picky PADs" [V, S10 CIS.SCR].
3. The node shows its ID (` 02FMA`, ` 0003PAZ`) [V, S6, S9] and then `Host Name:`.
4. The caller types `CIS`.

Some nodes of 1987 went directly from ^C to `User ID:` [V, S2].

Third-party networks:
- Tymnet: wait for ` in:`, send `CPS`, wait for `ERVE` [V, S10].
- Sprintnet: `@`, `D`, `C 614227` [V, S10].
- Doc 04 §2.2 also gives the two networks.

### 1.2 1989 full session [V, S1]

```
Host Name:  cis

User ID: 73237,341
Password:

CompuServe Information Service
16:11 EDT Wednesday 09-Aug-89 P
 Last access: 08:01 07-Aug-89
     Copyright (c) 1989
   CompuServe Incorporated
     All Rights Reserved
GO RATES for information on new
connect time FREE services and
the Membership Support Fee
What's New This Week
 1 New Databases Added to Reference Menu
 ...
 9 Uploads: New Forum Files
10 Online Today Product Reviews
Enter choice!off

Thank you for using CompuServe!
Off at 16:12 EDT 9-Aug-89
Connect time = 0:01
^D
```

### 1.3 1993 banner [V, S6]

```
CompuServe Information Service
 11:44 EDT Monday 10-May-93 P
      (Executive Option)
 Last access: 08:27 28-Apr-93
     Copyright (c) 1993
   CompuServe Incorporated
     All Rights Reserved
You have Electronic Mail waiting.
GO RATES for current information
What's New This Week
 1 X.400 Link With MCI Mail Added
 ...
Enter choice !
```

1995 variant [V, S9]:

```
 Last access: ...
Connected to port CIS T03PAZ @19200
There is a What's New menu
No electronic mail is waiting.
```

### 1.4 Formatting rules

- **Banner date:** `HH:MM TZ Weekday DD-Mon-YY` and then `P`. The `P` probably marks prime time [I]. Show it from 08:00 to 19:00 on weekdays.
- **Logoff date:** `Off at HH:MM TZ D-Mon-YY` has **no leading zero on the day**. The next line is `Connect time = h:mm`.
- **Plan line:** `(Executive Option)` shows when the account has this option.
- **User ID suffixes:**
  - `*NAVIG` (1987) identifies the client [V, S2].
  - `;TTY` sets the default terminal settings [V, S10].
  - `/INT`, `/INT:60`, `/GO:x` and `/HOST` are 90s options (doc 04 §2.2).
  - `/INT` (with `+`/`/secure`) changes to HMI [I, S9].
- **Bad login:** `?? LOGINE - Invalid entry - try again` [V-quoted, S10]. The 1996 scripts expect the `?LOG INE` family (doc 04). For terminals, send the first form. The code is the same.
- **Sign-up IDs:** IntroPak IDs were similar to `177000,1147`. The caller then got the Agreement and Serial number prompts [P].

### 1.5 Logging off

- `OFF` or `BYE` disconnects the line.
- `LOG` logs off and goes back to `User ID:`. TAPCIS uses it [V, S11].
- Logoff text (the same from 1987 to 1994) [V]:

```
Thank you for using CompuServe!
Off at 18:49 EDT 15-Sep-90
Connect time = 0:01
```

After this text, the network sends `^D` and a new `Host Name:`. Our server shows the text and closes the connection.

---

## 2. User IDs

- The ID is "a project number and a programmer number. An example would be 70007,1460" [V, S17]. The format is `7xxxx,xx`, later `1xxxxx,xxxx`.
- All IDs in the captures use only the digits 0–7: `73237,341`, `76174,2404`, `100073,1227`, `104616,2705`. A file from the 1980s says "the second number is from '0-7'". **Use the IDs as octal numbers**: give only the digits 0–7.
- Internet form: `76174.2404@compuserve.com` [V, S7].
- Folklore project ranges [P, S17]: 70000 security, 70003/70004 staff, 70006 customer service, 70007 complimentary, **76701–76704 sysops**, 177000 IntroPak. The sysops in the captures are in 7670x. Thus, use these ranges.

---

## 3. Navigation

### 3.1 Menu pages

1987 [V, S2]:
```
CompuServe                   TOP

 1 Subscriber Assistance
 2 Find a Topic
 3 Communications/Bulletin Bds.
 4 News/Weather/Sports
 5 Travel
 6 The Electronic MALL/Shopping
 7 Money Matters/Markets
 8 Entertainment/Games
 9 Home/Health/Family
10 Reference/Education
11 Computers/Technology
12 Business/Other Interests


Enter choice number !
```

1994 [V, S8]. The title is left-aligned. The page ID is right-aligned to column 80. Free pages add `(FREE)`:
```
CompuServe                                                                  TOP

 1 Access Basic Services
 2 Member Assistance (FREE)
 3 Communications/Bulletin Bds.
 ...
12 Business/Other Interests
Enter choice !
```

**Page IDs:**
- Older pages used `PREFIX-n` (`Page PCS-50` [V, S12]; `CIS-177`, `HOM-24`, `GAM-11` [S18]).
- Later pages use named IDs (`TOP`, `RATES`, `BILLING`) and numbered IDs (`CIS-162`, `BIL-512`, `LGN-1`, `CMS-138`) together.
- **These IDs are the keys of the page tree. GO words are aliases for them.** HMI uses the same IDs (`CIS:FORUMS`, `HOM:…`; doc 04).

### 3.2 Command summary, page CIS-162 [V, S8]

```
 T - TOP menu page
 M - previous MENU
 H or ? - HELP
 GO word or GO page - GO directly to a service
 FIND topic - FIND all references to topic
 OFF or BYE - sign off
 S n - SCROLL from n
 R - RESEND a page
 F - FORWARD a page
 B - BACK a page
 N - display NEXT menu item
 P - display PREVIOUS menu item
 SET option - SET terminal option
 PER - exit to Personal File Area
```

- To give a sequence of commands, put `;` between them (`OP;LL 32;T;RR 9900`) [V, S12].
- The `SET` options include `SET BRI ON`, `SET PAG OFF`, `SET WID 80` [V, S2].

### 3.3 Prompts and messages [V]

| String | Where |
|---|---|
| `Enter choice !` | Standard (1993–94) |
| `Enter choice!` | 1989 variant (no space) |
| `Enter choice number !` | 1987 |
| `Press <CR> for more !` | Pager |
| `Enter choice or <CR> for more !` | Long menus |
| `Last page !` / `Last page, enter choice !` | End of an article or menu |
| `One moment please...` | Before slow services |
| `BILLS is unrecognized !` | Unknown GO word |
| `!` | **Brief mode** (expert) prompt, also `More!` |
| `OK` | Command-mode prompt when "TOP goes to COMMAND" |

From the DEFAULT screen [V, S10]: "1 YES - expert mode (You will receive abbreviated prompts such as ! or More!.) 2 NO - novice/intermediate mode (You will receive full prompts such as Enter choice! or Enter <CR> for more!.)"

PARAMS.SCR calls `!` "CompuServe's almost-universal prompt that it's awaiting user input". **All prompts must end with `!`** (or the prompt character that the forum uses).

### 3.4 Control keys [V, S12, S13, S10]

| Key | Behaviour |
|---|---|
| Ctrl-C | Stops the output and exits immediately. In a conference, goes back to the forum `Function:` prompt. |
| Ctrl-P | Stops the output and goes back to the command level |
| Ctrl-S / Ctrl-Q | Stops / starts the output again |
| Ctrl-O | Skips the remaining text of a message during retrieval |
| Ctrl-U | Deletes the line that the user types. **TAPCIS sends ^U before each command. The host echoes ^U only if the buffer had unwanted characters** [V, S10]. |
| Ctrl-V | Shows again the line that the user typed until now |

### 3.5 Terminal settings: GO DEFAULT / GO TERMINAL [V, S10 PARAMS.SCR, 1990]

Time in these menus is free [V].

```
TERMINAL SETTINGS/SERVICE OPTIONS
 1 Instructions
 2 Change permanent settings
 3 Explanation of session vs. permanent
 4 Show session vs. permanent
 5 Change current session settings

PERMANENT SETTINGS
 1 Explanation
 2 Logon/Service options
 3 Display options
 4 Terminal type/parameters
 5 Transfer protocol/graphic support
 6 Make session settings permanent
```

| Group | Settings (defaults in [ ]) |
|---|---|
| Display | PAGED display [NO], BRIEF prompts [YES], CLEAR screen between pages, BLANK lines sent, Line feeds sent |
| Terminal | TERMINAL type [OTHER]: VIDTEX, ANSI, VT100, VT52, Heath, ADM, CRT, Other. Screen WIDTH [79], LINES per page [0], Form FEEDS [SIMULATED], Horizontal TABS, CASE [U/L], Chars sent in CAPS, PARITY [ZERO], Output DELAYS [0], ERASE when backspacing [YES], Micro inquiry sequence at logon [NO] |
| Logon/Service | First service at logon, Mail Waiting [NOTIFY ONLY], Personal menu, TOP goes to [COMMAND] (COMMAND/MAIN), Online editor [EDIT] (EDIT/LINEDIT), Forum presentation mode [COMMAND] |
| Protocol | B, Quick B, XMODEM, KERMIT, A, SHOW MENU |

After each change, a confirmation screen shows: `CHANGE PERMANENT SETTINGS 1 Confirm 2 Cancel`.

**As built** (`internal/cis/profile.go`, test `TestCompuServeDefault`, which sends and waits for the same text as PARAMS.SCR):
- The menus, their order, the bracketed values (right-aligned at column 40) and the setting screens come from PARAMS.SCR.
- We wrote the screens that the script does not show: Instructions, the two Explanations, Show session vs. permanent, Change current session settings, CLEAR/BLANK/LF, FEEDS/TABS, the GIF/NAPLPS/RLE screens, and the number prompts. The number prompts end in `:`, because the script waits for this character.
- `<CR>` goes up one level. If the user types a command at a prompt, the host exits DEFAULT without a save of the changes. Then it runs the command.
- After Confirm or Cancel, the host shows `GO PROFILE`, as in the script.
- When the user selects a terminal type, the host sets WIDTH 80, LINES 24, PARITY EVEN and ERASE NO, as the transcript shows.
- The host keeps the values as prefs: `cis-paged`, `cis-width` and other prefs.

Other notes:
- "Micro inquiry sequence at logon" is the setting that sends `ESC I` at login (doc 03 §5).
- **Compatibility minimum** [V, S10]: TAPCIS must have PAGED=NO and COMMAND mode in forums and Mail ("COMMAND mode is essential. TAPCIS will not work in any other"). Navigator for the Mac must have "TOP goes to MAIN".

---

## 4. Forums

### 4.1 Entering a forum (1988, forum software V.4C) [V, S3]

```
Online CAMPUS             JFORUM
One moment please...
Welcome to Journalism Forum, V. 4C(232)
Hello, JOHN EDWARDS
Last visit:  30-Dec-87  14:04:33
Forum messages:  33295 to  34948
Last message you've read:      0
Subtopic(s) Selected:
 All Accessible
No members are in conference.
Short bulletin:
 ...
Press <CR> : 
Journalism Forum
FUNCTIONS
 1 (L)  Leave a Message
 2 (R)  Read Messages
 3 (CO) Conference Mode
 4 (DL) Data Libraries
 5 (B)  Bulletins
 6 (MD) Member Directory
 7 (OP) User Options
 8 (IN) Instructions
Enter choice !
```

### 4.2 1994 forum menu and JOIN (software V.3B) [V, S7]

```
Welcome to Amiga Tech Forum, V. 3B(64)

Forum messages:  39470 to  40332

Amiga Tech Forum                                                     Forum Menu

 1 INSTRUCTIONS
 2 MESSAGES
 3 LIBRARIES (Files)
 4 CONFERENCING (0 participating)

 5 ANNOUNCEMENTS from sysop
 6 MEMBER directory
 7 OPTIONS for this forum

 8 JOIN this forum
Enter choice !2
Non-members cannot access this feature.
Do you wish to join (Y or N)? y

Please enter your name: don hicks

don hicks
Is this correct (Y or N)? y
Inserting name and ID...
Thank You For Joining Amiga Tech Forum !
Press <CR> !
```

Sub-menus [V, S7]:
- **Messages Menu:** `Message age selection = [All]`, `1 SELECT (Read by section and subject)`, `2 READ or search messages`, `3 CHANGE age selection`, `4 COMPOSE a message`, `5 UPLOAD a message`.
- **Read Menu:** `1 [ALL] messages`, `2 Message NUMBER`, `3 WAITING messages for you (0)`, then `Search [all] messages` with `4 FROM (Sender)`, `5 SUBJECT`, `6 TO (Recipient)`.
- **After each message:** `Press <CR> for next or type CHOICES !`. CHOICES goes to the **Read Action Menu**: `1 REPLY with same subject`, `2 COMPOSE with new subject`, `3 REREAD this message`, `4 SKIP reply`, `5 SKIP ALL`, `6 READ reply`.
- **Other strings:** `Message #:`, `Can't find message #40300`, `There is 1 Reply.`, `Press <CR> for more:`.

### 4.3 Leaving a message (1988) [V, S3]

```
Enter choice !L
To: ALL
Subject: SUBJECTS
Enter /EX to exit EDIT
[ New message ready ]
 ...text...
/ex
LEAVE ACTIONS
 1 (S)  Store the Message
 2 (SU) Store Unformatted
 3 (MA) Mail via Easyplex
 4 (MU) Mail Unformatted
 5 (C)  Continue Entering Text
 6 (A)  Abort the Leave Function
Enter choice !s
SUBTOPIC # REQUIRED
 0 General Information
 ...
17 DeskTop Publishing
Enter choice : 5
Message # 34949 Stored
```

The sections are 0–17. Thus, the **section limit is 18 for each forum**.

### 4.4 Message header [V, S16 1992; S7 1994]

```
#: 210094 S10/Views:Zachmann [E]
    04-Jul-92  12:33:25
Sb: #210070-#Independence Declaration
Fm: Bruce Biermann [ZiffNet] 72241,261
To: William F. Zachmann 72241,43 (X)

...body...

* Reply:  210146
```

| Line | Rule |
|---|---|
| 1 | `#: <num> S<sec>/<section name>`, and then ` [E]` as an option. TAPCIS shows `#:  55848` (two spaces). |
| 2 | Four spaces, then `DD-Mon-YY  HH:MM:SS` (two spaces between date and time). |
| Sb | `#Subj` = the root of a thread that has replies. `#<parent>-#Subj` = a reply that also has replies. `#<parent>-Subj` = a leaf. |
| Fm / To | `Name ID`. `(X)` after To means that the addressee has read the message. Sysops show as `Steve Pietrowicz/SYSOP 76701,250`. |
| Footer | `* Reply:  n` or `* Replies:  n, n, …`, wrapped with an indent of 12 spaces (V.4). V.3B shows `There is 1 Reply.` |

### 4.5 Command mode (forum software 4D, 1989) [V, S11]

**Prompts:**
`Forum !`, `Messages !`, `LIB !`, `Member Directory !`, `Read action !`, `Enter <CR> for more or type CHOICES !`, `Post action !`, `Edit !`.

**Commands at all prompts:**
`CON FIND G/GO HELP LIB LOG M MES OFF OPT PER PRO QUO R S SET T UST WEA WHO ?`

**At `Forum !`:**
`A`nnouncements, `COM`pose, `CO#`, `DE`scriptions, `EX`it, `I`nstructions, `LIB#`, `MEM`bership, `NA`mes, `NE`w, `OP`tions, `REA`d, `WH`o, `U`log.

**At `Messages !`:**
`BR`owse, `CH`ange, `HI`gh (sets the high-message pointer), `SE`lect, `SCA`n, `U`pload.

**Read, browse and scan:**
```
READ|BROWSE [FORWARD|THREAD|REVERSE|WAITING|MARKED] [NEW|DAYS:##|ALL|STA:##]
            [FROM:x|TO:x|SUB:x] [SEC:#,#]
```
Examples: `REA WAI`, `REA MAR`, `REA ALL SEC:2,9,10`, `REA NEW FRO:76703,4363`, `REA REV LAST`. `SCA` accepts the same arguments and also `PRO`.

**After a message:**
`CH`oices, `D`elete, `MA`rk, `NEX`, `NEX SUB`, `P`arent, `REA` (replies), `RE`ply, `RER`ead, `RO`ot, `SC`roll.

**Posting:**
`POST [UNF] [PRI] [SEC:##] [TO:x] [SUB:x]`.

**Editor (EDIT):**
`/A /B /C/old/new /D# /EXIT /HELP /L/text /N# /P# /POS# /POST /T /TYPE /UPLOAD /W`. The alternative editor is LINEDIT. It gives a number to each line.

**OPTIONS:**
`CH`aracter (prompt character), `E`ditor, `HI`gh, `I`nitial, `MO`de, `N`ame, `P`ermanent, `PA`use, `RE`plies, `S`ession, `SEC`tions, `SK`ip, `TY`pe.

**Commands TAPCIS sends** (from TAPCIS.EXE strings) [V, S10]. **These are the acceptance tests.**
```
OPT;TYP N;SKI N;MOD COM;INI;1;CHA;P
MESS;SCA QUI NEW;M
REA NEW        REA WAI        MESS;SCA WAI      REA THR NUM:U     RS
SCA /DES/LONG  BRO/LONG       DOW /PROTO:B
```
TAPCIS waits for these prompt tails: `Forum !`, `um !`, `es !`, `ce !`, `lex!`.

Abbreviations: the host identifies commands by unique prefixes (`REA`, `SCA`, `MESS`). Accept all prefixes of a minimum of 2–3 characters [I].

Earlier styles:
- The SIGs of 1983 used a `Function:` prompt.
- The Navigator of 1987 sent `OP;SM N;RE L;ED SED;UM OFF;S`, then `SSA`, `RM` (→ `No marked messages present`) and `DL1` [V, S2].

No primary source confirms `RS`/`RT`/`RN`/`RI`/`RTH` as standalone shortcuts. Only the `RS` of TAPCIS has a record [I].

### 4.6 Leaving a forum [V, S3]

```
Exiting at 15-Jan-88  03:54:57
Last message in forum:     34949
Last message you've read:      0
Thank you for visiting Journalism Forum
```

### 4.7 Libraries [V, S2 1987; S4 1988; S10]

```
DL 1 !S [74065,16]OASIS /DES

[74065,16]
OASIS/binary              03-Oct-87 73856              160

    Keywords: OASIS FINDER SHAREWARE

    Here it is, the latest version of Oasis...

DL 1 !DOW [74065,16]OASIS /proto:xmodem
Starting XMODEM transfer

Please initiate XMODEM transfer
and press <CR> when the transfer
is complete.
```

BROWSE (1988):
```
[76703,242]
TAKJOB.HOM                05-Dec-87 1972               Accesses: 42
    Keywords: JOB WORK HOME
    ...
Enter command, N for next file
or <CR> for disposition menu !
```

**File entries:**
- Name: 6.3 format, owned by the PPN of the uploader: `[ppn]FILENM.EXT`, with a `/binary` tag for binary files.
- Fields: date, size, accesses, keywords, description (a maximum of **465 chars** [V, S10]).
- The prompt shows the library: `DL 1 !` (1987) or `LIB !` (1989).

**Uploading:**
1. `UPL name/PRO:B/TYP:ASC`.
2. Prompts end in `ter:`, `hed:`, `rds:` (Keywords:) and `tle:` (Title).
3. Then `Is this okay (Y or N)?` [V, S10 UPL.SCR].

**Download protocol menus:**
- 1986 menu: doc 03 §7.1.
- 1993 Mail menu: `1 XMODEM (MODEM7) protocol 2 CompuServe 'B' protocol 5 Kermit 6 CompuServe Quick 'B' protocol` [V, S6]. The gaps in the numbers are in the original.

**Library announcement layout (1994)** [V, S7]:
```
GUILIB.LHA/Bin  Bytes: 175965
  Title   : ...
  Keywords: ...
```

### 4.8 Conference (CO) [V]

- Line format, 1989 [S14]: `(2,Irv) Hi Phil, nice to see you on CIS...`, meaning `(room,name)`.
- Line format, 1996 [S15]: `(19-11,Paladin) ...`, meaning `(room-job,name)`.
- `/users 2` [S14]:

```
User  User ID       Nod  Rm   Name / Forum Area
----  ------------  ---  ---  -----------------
 12   70003,4205    CSD   2   Steve L.
```

---

## 5. CompuServe Mail (EasyPlex)

### 5.1 EasyPlex 1988 [V, S5]

```
EasyPlex
Date:  13-Jan-88 20:18 PST
From:  Tom Sponheim [73557,3105]
Subj:  Hello
```

Address book entries are similar to `1 JOHN EDWARDS         71251,2457`.

### 5.2 CompuServe Mail 1993, menu mode [V, S6]

```
CompuServe Mail  Main Menu
 1 READ mail, 23 messages pending
 2 COMPOSE a new message
 3 UPLOAD a message
 4 USE a file from PER area
 5 ADDRESS Book
 6 SET options
 9 Send a CONGRESSgram ($)
Enter choice !1
CompuServe Mail  Read Menu
  1 INTERNET:paulg@tplrd.tpl.oz.au/comp.sys.amiga.misc #24609 - to be or not to be
    * Expire:(+78) *
  4 John Steiner/Bug Bytes #66
 ...
 0 READ ALL 23 messages
Enter choice or <CR> for more !
```

**Reading a message:**
- Header: `Date:  18-Apr-93 22:19 EDT` / `From:  Deron Kazmaier [76317,3633]` / `Subj:  …`.
- Internet senders show as `From:  INTERNET:user@host`.
- At the end: `Last page.  Enter command or <CR> to continue !`.

**Action Menu:**
`1 DELETE this message`, `2 FILE in PER area`, `3 FORWARD`, `4 REREAD message`, `5 REPLY`, `6 SAVE in mailbox`, `7 DOWNLOAD message`.

**Downloading mail:**
`1 messages and 227840 characters ready for download`, then the protocol menu, then `*** File Transfer Completed! ***`.

**Binary items** have the tag `* Binary * Expire:(+5) *`.

**End of the read list:**
`Last page.  Enter choice or M for CompuServe Mail main menu !`.

### 5.3 Command mode [V, S10]

- Prompt: `Mail!`. Entry text: `CompuServe Mail` / `No messages pending`.
- Error: `% Invalid command`.
- Mode menu: `Mail  Mode Menu 1 MENU 2 PROMPT 3 COMMAND`, and then `Make changes permanent for future Mail sessions ? (Y or N)`.
- Settings: `set edit noline`, `set page off`.

**The Mail setup of TAPCIS 5.42, verbatim from PARAMS.SCR** [V, S19]. Before TAPCIS sends each line, it waits for `!`:

```
S go mail          (the Mail Main Menu or "Mail!"; "No messages pending")
S no               ("% Invalid command": an answer to "first visit in 30 days?")
S set edit noline
S set page off
S set retain YES   (RETAIN: UNDELETE within 24 hours)
S set              Mail  Options Menu
                   [ ] Represents Current Setting
                       (yes/no options toggle)
                    1 EDITOR uses line numbers [NO]
                    2 MODE of operation is [MENU]
                      (MENU, PROMPT, COMMAND)
                    3 Output is PAGED [NO]
S 2                Mail  Mode Menu
                    1 MENU  - (Novice User)
                      List of choices in menu form.
                    2 PROMPT - (Intermediate User)
                      Brief list of commands.
                    3 COMMAND - (Expert User)
                      One word prompt.
S 3                (the Options Menu again, MODE [COMMAND])
S <CR>             Make changes permanent for
                   future Mail sessions ? (Y or N)
S Y
S ADD              (the Address Book)
S NAME             Enter a period to erase any information,  or <CR> to leave it unchanged.
                   Typing "CANCEL" cancels all changes made and leaves editing mode.
                   Your CompuServe Mail name
                     Old: Harold Bender
                     New: (TAPCIS sends the name)
                   Do you want to enter your return address? (Y or N) !
S ^C
```

The comments of the script show the Address Book menu as `1 INSERT an entry 2 CHANGE an entry 3 DELETE an entry 4 LIST Address Book 5 Enter/Change your NAME`. They also say that NOLINE and PAGE OFF "are essential settings for TAPCIS".

**What TAPCIS.EXE waits for and sends in Mail** [V, S19, strings only, thus the order is inferred]:

- It waits for `lex!` or `ail!` (EasyPlex! and Mail!), `ID):` (`Send to (Name or User ID):`), `ect:` (`Subject:`), `N)!` / `N)?`, `Action!` and `on!`.
- It reads with `read all` and `REA WAI`. It finds `ate:`, `rom:` and `ubj:` in headers.
- It downloads binary items with `dow/pro:b`, and deletes with `DEL`.
- It sends with `Compose` and `/EX`, or with `UPL/PRO:B/TYP:ASCII MAIL%.TMP` (`/TYP:LATIN1` with its /LATIN1 option; "Be patient after UPL after Send? Y answer").
- Addresses accept `>INTERNET:`, `>FAX`, `>POSTAL`, `>X400`, `>MCIMAIL:`, `>MHS:` and `>TLX:`, a `/R` at the end for a return receipt, and `;` for one more address.

**Our choices where the sources give no data** [I]: the send confirmation is `Send? (Y or N)!`, the result is `Message sent to Name [ppn]`, and the read prompt in command mode is `Action!`.
- Return receipts have a surcharge of $0.25 [P].

**Address forms:**

| Form | Meaning |
|---|---|
| `70000,123` | Member |
| `INTERNET:user@host` | Internet (inbound mail also comes as `user.id@compuserve.com`) |
| `FAX:301-3877322` | Fax gateway |
| `POSTAL` | Laser-printed postal letter |
| `MCIMAIL:` / X.400 (`ems`) | Other mail networks [P] |
| `MHS:` | Real, but the syntax is not verified [I] |

Write the name and address as `Name >ADDRESS` [V, S10].

---

## 6. CB Simulator

### 6.1 Commands, from `/HEL` (1982) [V, S13]

```
  /TUN # - Tunes channel # (1-36)
  /MON 1,4 - Listen to extra chnls
  /UNM 7,3 - Unmonitor channels
  /STA - Type channel status
  /TIM - Type time, day, and date
  /UST - User STatus typeout
  /EXI - Exit CB Simulator
  /WHO - Type PPN of last talker
  /HAN - Change handle
  /SCR xyz - Scramble on key "xyz"
  /SMC xyz - Scr & Monitor Clear
  /XCL xyz - Xmt CLear; unscr rcvr
  /UNS - Unscramble (both clear)
  /SQU abc - Squelch handle "abc"
  /JOB - your job #
  /HELP - Type this message
```

The sources also give `/TALK` (private chat), `/ROLL` (dice, shows `5,1`), `/GAG` (staff only), and `exceeding job limit` when the same ID enters two times [P, S17].

### 6.2 Output samples [V, S13, S17]

```
/TIM
11:09:43 PM EDT Friday, October 22, 1982

/UST
Job   User ID   Nod Ch Tlk Handle
---   -------   --- -- --- ------
 88 70015,727   NYC 30     SYSOP/Ron
```

The later `/UST` format (1986):

```
   Job   User Id   Ch.  Node  Handle
   ---  ---------- ---  ----  ------
1)  12  70003,1295  17  CSG   Red Leather
2) 133  70006,1293  1s  BAF   Surf's Up!
3)  69  76703,1211 Tlk  BOO   JOE CUFFS
```

In that format, `s` means scrambled, and `Tlk` means in a `/TALK`.

### 6.3 Model

- **Bands and channels:** channels 1–36 for each band. There was a General band and, later, more bands (an Adult band; a modern recreation uses bands A/B).
- **Users:** each user has a **handle** and a **job number**. The job number is the job number of the session on the host.
- **Scrambling:** an "encryption" for each key. Users with the same key see clear text. All other users see unwanted characters [I: implement as a deterministic scramble].
- **Line format:**
  - Forum conferences use `(room,name) text` [V].
  - A magazine of 1985 quotes CB as `(Old Man) Does anyone know what time it is?`.
  - **The CB format is not known.** Use `(ch,Handle)`, the same as forums [I].

**As built** [I]:

- `(Handle) text` for the tuned channel (the 1985 quote).
- `(ch,Handle) text` for a monitored channel.
- `** Handle tuned in to channel n **` and `** Handle left channel n **` when users come and go.
- `(Talk job,Handle) text` in a `/TALK`.
- At the entry, the host asks `What's your handle?` and `What channel (1-36)?`. `/TUN` answers `(Tuned to channel n, k others here)`.

---

## 7. Billing (for display only)

| Year | Rates |
|---|---|
| 1981 | 300 baud: $5.00/hr non-prime, $22.50/hr prime |
| 1986 | Non-prime $6 (300) / $12.50 (1200); prime $12.50 / $15 |
| 1987 | $6 (300) / $12.50 (1200/2400); no prime surcharge |
| ~1990 | $6.50 / $12.75 |
| Feb 1994 | Standard plan: $8.95/month for basic services, connect $4.80/hr (≤2400) and $9.60/hr (9600/14400). Alternative plan: $2.50/month plus $6.30 / $12.80 / $22.80 per hour. Non-prime is 1900–0800 weekdays and all weekend. |

Sources: trs-80.org, Compute! #78, START v2n3, S8.

**Menu markers:**
- `+` marks extended (hourly) services, and `$` marks premium surcharges.
- `(FREE)` marks free pages.
- The Executive Option added a minimum of $10/month, file storage for 6 months and exclusive databases.

**GO BILLING (1988)** [V, S3]:
```
CompuServe               BILLING
BILLING INFORMATION
 1 Current Rates
 2 Reviewing Your Charges
 ...
 6 Order Executive Option
Enter choice !2

CompuServe CHARGES
 1 Explanation
 2 Account Balance
 3 Billing History
USAGE DETAILS
 4 Current activity
 5 Previous activity
 6 Mail Hardcopy ($)
Enter Choice !2
CompuServe CHARGES
Searching Billing Files ...
        ACCOUNT BALANCE
Balance as of 12/26/87      $.00
 Activity since 12/26/87:
  Payments                  -.00
  Charges                  $1.11
 ...
 Balance as of 1/12/88     $1.11
                        ========
Last page !
```

The 1994 `NEWBIL` "Enhanced Billing Display" menu: `1 Account Balance`, `2 Pricing Plans & Clubs`, `3 Mail and Other Allowances`, `4 Account History`, `5 Detailed Session Activity`, `6 Hardcopy Requests`.

**For the revival:** show bills in fake period dollars from the recorded connect time. This adds charm, and it costs nothing.

---

## 8. GO words seen in sources

| Category | GO words |
|---|---|
| Navigation and help | `CIS` `TOP` `NEW` `FIND` `HELP` `MEMBER` `FREE` `BASIC` |
| Account | `RATES` `BILLING` `CHARGES` `NEWBIL` `EXECUTIVE` `PHONES` `DEFAULT` `TERMINAL` `PROFILE` `PASSWORD` |
| Mail | `MAIL` `EASY` `MAILRATES` |
| Chat and forums | `CB` `CB-1` `FORUMS` `PRACTICE` `HELPFORUM` `CHALLENGE` |
| News and markets | `WEA` `QUO` `BASICQUOTE` `ENS` (Executive News Service, $) `OLT-90` (Online Today) `APN`/`APV` (AP) |
| Travel | `EAASY` (EAASY SABRE) `OAG` (Official Airline Guide) `PDG` `AWX` (aviation weather RLE maps) |
| Shopping | `MALL` |
| Reference | `PHONEFILE` `DIRECTORY` |
| Personal | `PER` (Personal File Area) |
| Forums | `MACUS` (MAUG) `JFORUM` `TAPCIS` `AMIGATECH` `AMIGAUSER` `CIMSUP` `WCIMSUP` `MCIMSUP` `NAVSUP` `ATARI8` `ATARIPRO` `IBMCOM` `PCMAGNET` |
| Games | `GAM-11` (Adventure), MegaWars |

---

## 9. Gaps

- There is no verbatim capture of the CB band selection, or of the CB line format with channel numbers.
- The `MHS:` address syntax.
- The exact text for a bad password (two possible texts in §1.4).
- `RS`/`RT`/`RN` shortcuts and QUICKSCAN output format (TAPCIS uses `SCA QUI`).
- **Next sources to mine:**
  - Computer History Museum scans of CompuServe manuals (<https://archive.computerhistory.org/resources/access/text/2024/06/102751645-05-01-acc.pdf>, also 102751668, 102805861; they need OCR).
  - *The Complete Handbook of CompuServe*.
  - The 32 GB host disk images (doc 01 §2).
  - <https://compuserveweb.com/>, a live recreation, for comparison.
