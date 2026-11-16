# CompuServe

A listener with `frontend = compuserve` serves a recreation of the
CompuServe Information Service in place of the menus of the BBS. It uses
the users of the BBS. Its objective is to operate with the real
CompuServe software: terminal programs, offline readers (TAPCIS,
AutoSIG, OzCIS) and the CompuServe Information Manager (WinCIM, DOSCIM,
MacCIM). The scripts of these programs look for the exact prompts of
that period. Thus, the service keeps these prompts.

```ini
[listen cis]
type = telnet
address = :2324
frontend = compuserve
```

A `raw` listener also operates: modem callers through app_dmodem dial
directly into CompuServe. `connect = 9600` sets the `CONNECT` line that
the service shows before `Host Name:`. It is on by default for telnet,
because the scripts of CIM expect it over TCP. It is off for raw
listeners, because the modem already sent it.

## Logging on

```
Host Name: CIS
User ID: 72000,1
Password:
```

- **Host Name:** `CIS`, `CPS` or `CISAGREE`, the names that the scripts
  of CompuServe send.
- **User ID:** the account number written as a CompuServe ID: account 1
  is `72000,1`. It uses only octal digits, as the IDs of CompuServe did.
  A caller can also type the BBS name. In that case, the service shows
  the ID at logon. The ID can have the usual options, for example
  `/INT:60`, `;TTY` and `/GO:word`.
- **Password:** the BBS password.
- **Bad logon:** an incorrect ID or password gets
  `?? LOGINE - Invalid entry - try again`. Locked accounts and throttled
  addresses get the `?LOG` codes of CompuServe.

After the logon, the host sends `ESC I`. Terminals that know it answer
with their identification, which shows in who's online. A CIM client
(`AP` in its reply) gets the graphical service. Refer to the text below.

## CIM (WinCIM)

CIM clients use HMI, a binary protocol inside B Plus packets. The
service follows the data that 24high/CompuServe-Emulator recovered from
WinCIM 3.0.2: the Secure Login, the terminal-mode exchange after it, the
B Plus Transport Layer, and the HMI messages for pages. In WinCIM, you
can do these actions:

- Browse the main menu, the menus and articles of `pages.txt`, What's
  New and the Table of Contents, and use Find.
- Read the forums: the forum list, the sections and libraries of a
  forum, the messages of a section (newest first) and each message. A
  message that you read counts as read.
- See the files of a library (downloads still must have a terminal
  program).
- Read your mail with **Get Mail**: the mailbox, newest first, with the
  mark `*` on unread letters. When you read a letter, the service marks
  it as read.

CIM cannot yet send mail, post to forums, download, or use the CB.
WinCIM sends mail and forum messages through its own transactions, which
nobody has recovered yet. For now, use a terminal program for those
tasks.

Point WinCIM at the listener over TCP (with the [DNS
server](DNS) of the BBS, `HostIPAddress` can be `compuserve.com`). In
CIS.INI, the `CommType=WINSOCK`, `WinsockCommType=Local` and
`HostIPAddress` of the connector set this. The port of CompuServe for
this was 4144:

```ini
[listen wincim]
type = raw
address = :4144
frontend = compuserve
connect = 9600
```

**Secure Login.** CIM logs on with the Secure Login Protocol (`+` before
the User ID). This protocol never sends the password. The host checks
it against a key that it keeps from the password. The host keeps that
key only while a `compuserve` listener runs. It makes the key the next
time that the user logs on with the password or sets a password. Until
then, a Secure Login fails with `No.`, and the log gives the reason. Log
on one time from a terminal, or set `DisableSecureLogin=ON` in CIS.INI
for one plain login.

We did not try any of this with a real WinCIM yet. That is the next set
of tests.

## Commands

| Command | Does |
|---|---|
| a number | selects that menu item |
| `T` | the TOP menu |
| `M` | the previous menu |
| `B` | back one page |
| `F` | forward one page |
| `R` | sends the page again |
| `N` / `P` | the next or previous item of the menu |
| `H` or `?` | help |
| `GO word` | goes to a page by GO word or page ID |
| `FIND topic` | lists the pages that contain a topic |
| `SET BRIEF ON` | brief `!` prompts |
| `SET PAGED ON` | stops at each screen |
| `SET WIDTH n` / `SET LINES n` | screen size |
| `OFF` or `BYE` | signs off |
| `LOG` | signs off and goes back to `User ID:` |

You can join commands with `;`, as in `SET BRI ON;GO RATES`.

## Terminal settings (GO DEFAULT)

`SET` changes the settings of the session. `GO DEFAULT` (or `TERMINAL`,
or item 1 of `GO PROFILE`) keeps them with your User ID, through the
menus of CompuServe: TERMINAL SETTINGS/SERVICE OPTIONS, then PERMANENT
SETTINGS with its Logon/Service, Display, Terminal and Transfer protocol
groups. When you leave, you confirm the changes. The screens follow the
ALT-P script of TAPCIS (PARAMS.SCR, 1991), which configures a new member
through them.

The kept settings do these functions:

- PAGED, BRIEF, WIDTH and LINES apply at each logon.
- **Mail waiting: Go to MAIL** goes directly into Mail when letters wait.
- **Online editor** sets the line numbers of Mail when Mail has no
  setting of its own.
- **Protocol preference** (B, Quick B, XMODEM or Kermit) skips the
  protocol menu.

The service keeps and shows the other settings, as scripts expect, but
they do nothing here: terminal type, parity, delays, first service, TOP
goes to, forum mode and graphics support. Forums are always in command
mode. To log on with the defaults one time, add `;TTY` to the User ID
(`72000,1;TTY`).

## Forums

`GO FORUMS` lists the forums. Each forum is a group of the message areas
of the BBS. Each area in it is a section (S0, S1, ...). Areas that are
not in a group make a forum with the name of the BBS. The service
divides a group with more than 18 areas into more than one forum,
because the forums of CompuServe had a maximum of 18 sections. The GO
word of a forum is its name in capitals, with letters and digits only
(`Amiga Tech` is `GO AMIGATECH`). Message numbers are the numbers of the
BBS. Thus, they increase across the forum, with gaps.

The forum operates in command mode, at `Forum !`. TAPCIS and the other
offline readers must have this mode:

| Command | Does |
|---|---|
| `REA [NEW\|ALL\|WAI\|REV\|THR] [FROM:x] [TO:x] [SUB:x] [SEC:n,n] [DAYS:n] [STA:n] [n]` | reads (NEW by default). `Read action !` after each: `RE` reply, `RER` read again, `P` parent, CR next |
| `SCA ...` / `SCA QUI ...` | the headers, or one line for each |
| `BRO ...` | one header at a time, read on CR |
| `POST [SEC:n] [TO:x] [SUB:x]` | writes a message (EDIT: `/EX` then `S` to store, or `/POST`) |
| `DES` / `NEW` | the sections. The new messages in each |
| `OPT;TYP N` | reads with no stop (`TYP P` stops again). The service accepts the other OPT settings |
| `MES` / `M` | the `Messages !` prompt and back |
| `EXIT`, `M`, `T`, `GO x`, `OFF` | leaves the forum |

Headers follow those of forum software 4:

```
#: 2 S1/Software
    08-Nov-26  11:30:05
Sb: #1-Kickstart 1.3
Fm: sysop 72000,2
To: alice 72000,1
```

Membership (JOIN) and conferences are not available yet.

## Libraries

The libraries of a forum are the file areas in its group, with numbers
from 0, as its sections have. File areas that are not in a group are the
libraries of the forum with the name of the BBS. To put a file area in a
group, use `bbs filearea group AREA GROUP`.

`LIB n` from `Forum !` opens a library, at `LIB !`:

| Command | Does |
|---|---|
| `DIR [name] [/DES] [KEY:x] [AGE:n]` | the files, newest first. With `/DES` or a name, their title, keywords and description |
| `BRO ...` | one file at a time, with a disposition menu |
| `SCA [/DES] ...` | one or two lines for each (`NAME/Bin  Bytes: n`) |
| `DOW name [/PROTO:B\|QB\|XMODEM\|KERMIT\|CAPTURE]` | downloads. Without `/PROTO:`, the protocol menu of CompuServe |
| `UPL name [/PROTO:...] [/TYPE:ASC\|BIN]` | uploads, then the title, keywords and description (a maximum of 465 characters) |
| `LIB n`, `M` | a different library. Back to the forum |

The B and Quick B selections run the B Plus engine. The host sends ENQ,
and the answer of the terminal selects Classic B, Quick B or B Plus.
XMODEM and Kermit print the start message of CompuServe, and wait for a
CR when the transfer is complete. Downloads count against the daily
limits and ratios of the BBS, as on the BBS. Entries look like this:

```
[72000,2]
BPLUS.ZIP/binary          08-Nov-26 8700         Accesses: 1

    Title   : B Plus for everyone
    Keywords: BPLUS PROTOCOL
```

## Mail

`GO MAIL` (or `EASY`, `EASYPLEX`) opens CompuServe Mail. It is the
private mail of the BBS. Mail sent here arrives in the mailbox of the
BBS, and mail sent on the BBS arrives here. A message is *pending* until
you read it. Read messages stay in the mailbox, as if saved, until you
delete them.

Mail starts in menu mode, as the Mail of CompuServe did. `SET` opens the
Options Menu. There, option 2 changes the mode to PROMPT or COMMAND. The
answer to `Make changes permanent for future Mail sessions ? (Y or N) !`
keeps it with the account. The setup script of TAPCIS (`PARAMS.SCR`, run
by Alt-P) does exactly this, then `set edit noline` and `set page off`.
The service also keeps these. In command mode, the prompt is `Mail!`.

| Command | Does |
|---|---|
| `READ [n\|ALL\|SAVED]` | reads the pending messages (or the saved ones). `Action!` after each: CR next, `DELETE`, `SAVE`, `REREAD`, `REPLY`, `FORWARD [address]`, `DOWNLOAD [/PROTO:x]` |
| `SCAN [ALL\|SAVED]` | one line for each: ` 1 alice/Testing` |
| `COMPOSE [address]` | writes (EDIT, with `/EX` at the end), then `Send to (Name or User ID):`, `Subject:` and `Send? (Y or N)!` |
| `UPLOAD [/PRO:B] [/TYP:ASCII\|LATIN1]` | the text of a message by a protocol, then the same questions (the `UPL/PRO:B/TYP:ASCII MAIL%.TMP` of TAPCIS) |
| `DOWNLOAD [n\|ALL\|SAVED] [/PROTO:x]` | messages as one text file, by a protocol |
| `DELETE n` | deletes a message |
| `ADDRESS` | the address book, which the service keeps (it is the same one that the Exchange of MSN Classic uses), and `NAME` for your Mail name (for this session only) |
| `SET`, `SET MODE x`, `SET EDIT x`, `SET PAGE x`, `MODE` | the options |
| `EXIT`, `T`, `GO x`, `OFF` | leaves Mail |

Addresses use the forms of CompuServe: a BBS name, a User ID
(`72000,1`), a name and an ID (`Sysop 72000,2`, `Sysop [72000,2]`), or a
name and a network address after `>`.
`INTERNET:72000.1@compuserve.com` is a member here. The service refuses
other Internet addresses, FAX, POSTAL, MCIMAIL, X.400 and MHS, with a
message that tells this. A FidoNet address (`Bob@1:234/5`) goes out as
netmail when the BBS is on FidoNet. An address that ends in `;` asks for
another address. The service accepts a `/R` (return receipt), but does
not do it.

A message looks like this:

```
Date:  08-Nov-26 12:40 UTC
From:  alice [72000,1]
Subj:  Testing

Hello from Mail.
```

## CB Simulator

`GO CB` opens the CB Simulator. Its channels are the chat rooms `cb-1`
to `cb-36` of the BBS. Thus, callers in the chat of the BBS, or on IRC,
can also join a channel. A CB caller selects a handle, which cannot be
the name of a different member, and a channel. Then all text that the
caller types goes to that channel. A line from the tuned channel shows
as `(Handle) text`. A line from a monitored channel shows as
`(17,Handle) text`. Other CB callers see the handle. The other chat
programs of the BBS see the name of the caller.

| Command | Does |
|---|---|
| `/TUN n` | tunes to channel n (1-36) |
| `/MON 1,4`, `/UNM 1,4` | also listens to other channels, or stops |
| `/STA` | the number of users on each channel |
| `/UST [n]` | the users on CB: job, User ID, channel (`17s` scrambled, `Tlk` talking), node, handle |
| `/WHO` | the User ID of the last user who sent a line |
| `/HAN name` | changes your handle |
| `/SCR key`, `/SMC key`, `/XCL key`, `/UNS` | scrambles the text that you send and unscrambles the text that you get, one of the two, or neither |
| `/SQU handle` | squelches a handle (again to unsquelch) |
| `/TALK job` | talks with one caller only (`/EXIT` back to the channel) |
| `/ROLL [3D6]` | rolls dice for the channel |
| `/TIM`, `/JOB`, `/HELP` | the time, your job number (the BBS node), the commands |
| `/EXI`, `/OFF` | leaves CB, or logs off |

Scrambling operates as it did on CompuServe. The key permutes all text
that you send, and the key reverses the permutation on all text that you
get. Thus, only callers who use the same key can read each other.

## What is there and what is coming

| Done | Coming, in order |
|---|---|
| The logon, `ESC I`, the menus and articles (`internal/cis/pages.txt`), GO, FIND, SET, permanent settings (GO DEFAULT) | Forum JOIN and member options |
| Forums on the message areas, with the forum command language | |
| The CompuServe B, Quick B and B Plus protocols (`internal/xfer/cisb.go`) | B Plus resume (Tr) and file information (TI) |
| Libraries on the file areas, with B+ downloads and uploads | |
| CompuServe Mail on the private mail of the BBS | Binary mail |
| The CB Simulator on the chat rooms | Forum conferences (CO) |
| CIM: Secure Login, the B Plus Transport Layer, HMI pages, menus, articles and Find | |
| CIM: reading forums and Mail, library lists | CIM: sending mail, posting, downloads, CB |

The research for all of this, with sources, is in
[docs/cis](https://github.com/D-Modem/bbs/tree/main/docs/cis).
