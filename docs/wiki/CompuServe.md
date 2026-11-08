# CompuServe

A listener with `frontend = compuserve` serves a recreation of the
CompuServe Information Service instead of the BBS's menus. It runs on the
BBS's own users, and it aims to work with the real CompuServe software:
terminal programs, offline readers (TAPCIS, AutoSIG, OzCIS) and, later,
the CompuServe Information Manager (WinCIM, DOSCIM, MacCIM). Their
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
with their identification, which shows in who's online. CIM clients
(`AP` in their reply) are told `?LOG CAI` for now, because the graphical
(HMI) service is not built yet.

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

## What is there and what is coming

| Done | Coming, in order |
|---|---|
| The logon, `ESC I`, the menus and articles (`internal/cis/pages.txt`), GO, FIND, SET | Forum JOIN and member options |
| Forums on the message areas, with the forum command language | |
| The CompuServe B, Quick B and B Plus protocols (`internal/xfer/cisb.go`) | B Plus resume (Tr) and file information (TI) |
| Libraries on the file areas, with B+ downloads and uploads | |
| | CompuServe Mail, the CB Simulator, then CIM's HMI service |

The research behind all this, with sources, is in
[docs/cis](https://github.com/D-Modem/bbs/tree/main/docs/cis).
