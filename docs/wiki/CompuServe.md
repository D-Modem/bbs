# CompuServe

A listener with `frontend = compuserve` serves a recreation of the
CompuServe Information Service instead of the BBS's menus. It runs on the
BBS's own users, and it aims to work with the real CompuServe software:
terminal programs, offline readers (TAPCIS, AutoSIG, OzCIS) and the
CompuServe Information Manager (WinCIM, DOSCIM, MacCIM). Their
scripts look for the period's exact prompts, so the service keeps them.

```ini
[listen cis]
type = telnet
address = :2324
frontend = compuserve
```

A `raw` listener works too: modem callers through app_dmodem dial
straight into CompuServe. `connect = 9600` sets the `CONNECT` line shown
before `Host Name:`. It is on by default for telnet, which is what CIM's
scripts expect over TCP, and off for raw listeners, where the modem has
already said it.

## Logging on

```
Host Name: CIS
User ID: 72000,1
Password:
```

- **Host Name:** `CIS`, `CPS` or `CISAGREE`, the names CompuServe's own
  scripts send.
- **User ID:** the account's number written as a CompuServe ID: account 1
  is `72000,1`, using octal digits only, like CompuServe's own. A caller
  can also type their BBS name; in that case the ID is shown at logon.
  The ID may carry the usual options, such as `/INT:60`, `;TTY` and
  `/GO:word`.
- **Password:** the BBS password.
- **Bad logon:** a wrong ID or password gets
  `?? LOGINE - Invalid entry - try again`. Locked accounts and throttled
  addresses get CompuServe's `?LOG` codes.

After the logon the host sends `ESC I`. Terminals that know it answer
with their identification, which shows in who's online. A CIM client
(`AP` in its reply) gets the graphical service instead; see below.

## CIM (WinCIM)

CIM clients speak HMI, a binary protocol inside B Plus packets. The
service follows what 24high/CompuServe-Emulator recovered from WinCIM
3.0.2: the Secure Login, the terminal-mode trade after it, the B Plus
Transport Layer, and the HMI messages for pages. WinCIM can show the main
menu, the menus and articles of `pages.txt`, What's New and the Table of
Contents, and use Find. Forums, Mail and the CB are not in CIM yet; their
pages say to use a terminal program for now.

Point WinCIM at the listener over TCP. In CIS.INI, the connector's
`CommType=WINSOCK`, `WinsockCommType=Local` and `HostIPAddress` set
this. CompuServe's own port for that was 4144:

```ini
[listen wincim]
type = raw
address = :4144
frontend = compuserve
connect = 9600
```

**Secure Login.** CIM logs on with the Secure Login Protocol (`+` before
the User ID), which never sends the password. The host checks it against
a key it keeps from the password. That key is kept only while a
`compuserve` listener runs, the next time the user logs on with their
password or sets one. Until then a Secure Login fails with `No.`, and the
log says why. Log on once from a terminal, or set
`DisableSecureLogin=ON` in CIS.INI for one plain login.

None of this has met a real WinCIM yet; that is the next round of
testing.

## Commands

| Command | Does |
|---|---|
| a number | that menu choice |
| `T` | the TOP menu |
| `M` | the previous menu |
| `B` | back a page |
| `F` | forward a page |
| `R` | resend the page |
| `N` / `P` | next or previous item of the menu |
| `H` or `?` | help |
| `GO word` | go to a page by GO word or page ID |
| `FIND topic` | list the pages that mention a topic |
| `SET BRIEF ON` | brief `!` prompts |
| `SET PAGED ON` | stop at each screen |
| `SET WIDTH n` / `SET LINES n` | screen size |
| `OFF` or `BYE` | sign off |
| `LOG` | sign off and go back to `User ID:` |

Commands chain with `;`, as in `SET BRI ON;GO RATES`.

## Forums

`GO FORUMS` lists the forums. Each forum is a group of the BBS's message
areas, and each area in it is a section (S0, S1, ...). Areas in no group
make up a forum named after the BBS. A group with more than 18 areas is
split into several forums, because CompuServe's forums had at most 18
sections. A forum's GO word is its name in capitals, letters and digits
only (`Amiga Tech` is `GO AMIGATECH`). Message numbers are the BBS's own,
so they rise across the forum with gaps.

The forum works in command mode, at `Forum !`, which is the mode TAPCIS
and the other offline readers need:

| Command | Does |
|---|---|
| `REA [NEW\|ALL\|WAI\|REV\|THR] [FROM:x] [TO:x] [SUB:x] [SEC:n,n] [DAYS:n] [STA:n] [n]` | read (NEW by default); `Read action !` after each: `RE` reply, `RER` reread, `P` parent, CR next |
| `SCA ...` / `SCA QUI ...` | the headers, or one line each |
| `BRO ...` | a header at a time, read on CR |
| `POST [SEC:n] [TO:x] [SUB:x]` | write a message (EDIT: `/EX` then `S` to store, or `/POST`) |
| `DES` / `NEW` | the sections; the new messages in each |
| `OPT;TYP N` | read nonstop (`TYP P` pauses again); the rest of OPT is taken |
| `MES` / `M` | the `Messages !` prompt and back |
| `EXIT`, `M`, `T`, `GO x`, `OFF` | leave the forum |

Headers follow forum software 4's:

```
#: 2 S1/Software
    08-Nov-26  11:30:05
Sb: #1-Kickstart 1.3
Fm: sysop 72000,2
To: alice 72000,1
```

Membership (JOIN) and conferencing are not there yet.

## Libraries

A forum's libraries are the file areas in its group, numbered from 0 like
its sections. File areas in no group are the libraries of the forum named
after the BBS. Put a file area in a group with
`bbs filearea group AREA GROUP`.

`LIB n` from `Forum !` opens a library, at `LIB !`:

| Command | Does |
|---|---|
| `DIR [name] [/DES] [KEY:x] [AGE:n]` | the files, newest first; with `/DES` or a name, their title, keywords and description |
| `BRO ...` | a file at a time, with a disposition menu |
| `SCA [/DES] ...` | a line or two each (`NAME/Bin  Bytes: n`) |
| `DOW name [/PROTO:B\|QB\|XMODEM\|KERMIT\|CAPTURE]` | download; without `/PROTO:`, CompuServe's protocol menu |
| `UPL name [/PROTO:...] [/TYPE:ASC\|BIN]` | upload, then the title, keywords and description (465 characters at most) |
| `LIB n`, `M` | another library; back to the forum |

The B and Quick B choices run the B Plus engine. The host sends ENQ, and
the terminal's answer decides between Classic B, Quick B and B Plus.
XMODEM and Kermit print CompuServe's start message and wait for a CR when
the transfer is complete. Downloads count against the BBS's daily limits
and ratios, the same as on the BBS itself. Entries look like this:

```
[72000,2]
BPLUS.ZIP/binary          08-Nov-26 8700         Accesses: 1

    Title   : B Plus for everyone
    Keywords: BPLUS PROTOCOL
```

## Mail

`GO MAIL` (or `EASY`, `EASYPLEX`) enters CompuServe Mail. It is the BBS's
private mail: what is sent here arrives in the BBS's mailbox, and the
other way round. A message is *pending* until it is read. Read messages
stay in the mailbox, as if saved, until they are deleted.

Mail starts in menu mode, as CompuServe's did. `SET` opens the Options
Menu, where option 2 changes the mode to PROMPT or COMMAND, and the
answer to `Make changes permanent for future Mail sessions ? (Y or N) !`
keeps it with the account. TAPCIS's setup script (`PARAMS.SCR`, run by
Alt-P) does exactly this, then `set edit noline` and `set page off`, which
are kept too. In command mode the prompt is `Mail!`.

| Command | Does |
|---|---|
| `READ [n\|ALL\|SAVED]` | read the pending messages (or the saved ones); `Action!` after each: CR next, `DELETE`, `SAVE`, `REREAD`, `REPLY`, `FORWARD [address]`, `DOWNLOAD [/PROTO:x]` |
| `SCAN [ALL\|SAVED]` | one line each: ` 1 alice/Testing` |
| `COMPOSE [address]` | write (EDIT, ending with `/EX`), then `Send to (Name or User ID):`, `Subject:` and `Send? (Y or N)!` |
| `UPLOAD [/PRO:B] [/TYP:ASCII\|LATIN1]` | a message's text by a protocol, then the same questions (TAPCIS's `UPL/PRO:B/TYP:ASCII MAIL%.TMP`) |
| `DOWNLOAD [n\|ALL\|SAVED] [/PROTO:x]` | messages as one text file, by a protocol |
| `DELETE n` | delete a message |
| `ADDRESS` | the address book, and `NAME` for your Mail name (both for this session only) |
| `SET`, `SET MODE x`, `SET EDIT x`, `SET PAGE x`, `MODE` | the options |
| `EXIT`, `T`, `GO x`, `OFF` | leave Mail |

Addresses take CompuServe's forms: a BBS name, a User ID (`72000,1`), a
name and an ID (`Sysop 72000,2`, `Sysop [72000,2]`), or a name and a
network address after `>`. `INTERNET:72000.1@compuserve.com` is a member
here. Other Internet addresses, FAX, POSTAL, MCIMAIL, X.400 and MHS are
refused, with a message saying so. A FidoNet address (`Bob@1:234/5`) goes
out as netmail when the BBS is on FidoNet. An address ending in `;` asks
for another, and a `/R` (return receipt) is accepted but not acted on.

A message reads like this:

```
Date:  08-Nov-26 12:40 UTC
From:  alice [72000,1]
Subj:  Testing

Hello from Mail.
```

## CB Simulator

`GO CB` enters the CB Simulator. Its channels are the BBS's chat rooms
`cb-1` to `cb-36`, so callers in the BBS's own chat, or on IRC, can join a
channel too. A CB caller picks a handle, which can't be another member's
name, and a channel. After that, whatever they type is said on that
channel. A line from the tuned channel shows as `(Handle) text`, and a
line from a monitored channel shows as `(17,Handle) text`. Others see
the handle, and the BBS's other chat programs see the caller's name.

| Command | Does |
|---|---|
| `/TUN n` | tune to channel n (1-36) |
| `/MON 1,4`, `/UNM 1,4` | listen to other channels too, or stop |
| `/STA` | how many are on each channel |
| `/UST [n]` | who is on CB: job, User ID, channel (`17s` scrambled, `Tlk` talking), node, handle |
| `/WHO` | the User ID of the last talker |
| `/HAN name` | change your handle |
| `/SCR key`, `/SMC key`, `/XCL key`, `/UNS` | scramble what you send and unscramble what you get, one of the two, or neither |
| `/SQU handle` | squelch a handle (again to unsquelch) |
| `/TALK job` | talk with one caller alone (`/EXIT` back to the channel) |
| `/ROLL [3D6]` | roll dice for the channel |
| `/TIM`, `/JOB`, `/HELP` | the time, your job number (the BBS node), the commands |
| `/EXI`, `/OFF` | leave CB, or log off |

Scrambling works the way CompuServe's did. Everything you send goes out
permuted by the key, and everything you receive is unpermuted with it,
so only callers using the same key read each other.

## What is there and what is coming

| Done | Coming, in order |
|---|---|
| The logon, `ESC I`, the menus and articles (`internal/cis/pages.txt`), GO, FIND, SET | Forum JOIN and member options |
| Forums on the message areas, with the forum command language | |
| The CompuServe B, Quick B and B Plus protocols (`internal/xfer/cisb.go`) | B Plus resume (Tr) and file information (TI) |
| Libraries on the file areas, with B+ downloads and uploads | |
| CompuServe Mail on the BBS's private mail | A kept address book, binary mail |
| The CB Simulator on the chat rooms | Forum conferences (CO) |
| CIM: Secure Login, the B Plus Transport Layer, HMI pages, menus, articles and Find | Forums, Mail, libraries and CB in CIM |

The research behind all this, with sources, is in
[docs/cis](https://github.com/D-Modem/bbs/tree/main/docs/cis).
