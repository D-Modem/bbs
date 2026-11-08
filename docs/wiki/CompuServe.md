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

## What is there and what is coming

| Done | Coming, in order |
|---|---|
| The logon, `ESC I`, the menus and articles (`internal/cis/pages.txt`), GO, FIND, SET | Forums on the message areas, with the forum command language |
| The CompuServe B, Quick B and B Plus protocols (`internal/xfer/cisb.go`) | Libraries on the file areas, with B+ downloads |
| | CompuServe Mail, the CB Simulator, then CIM's HMI service |

The research behind all this, with sources, is in
[docs/cis](https://github.com/D-Modem/bbs/tree/main/docs/cis).
