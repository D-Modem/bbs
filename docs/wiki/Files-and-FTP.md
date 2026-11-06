File areas hold files for callers to download and upload. Callers browse
them on the BBS (*F* on the main menu), and can also use an FTP client,
logging in with their BBS name and password; each area is a directory
there.

## On the BBS

| Action | Key | PF | What it does |
|---|---|---|---|
| an area | its number | `S` by it | its files, by name |
| new files | `N` | PF5 | the files added since your last call (the last 30 days on a first call), in every area, newest first |
| find | `F` | PF10 | files whose name or descriptions have the words (in any case) |
| batch | `B` | | the files tagged, to download together (text terminals) |

A file's details show its description and, for an archive, a longer
description. If an archive is uploaded with a `FILE_ID.DIZ` at its top
level, that file becomes the long description, and its first meaningful
line becomes the description unless the uploader gave one. (CP437 art is
decoded, colours and control characters are removed, and at most 20
lines are kept.) From the details, a caller can:

| Action | Key | PF | Notes |
|---|---|---|---|
| download | `D` | | by a transfer protocol ([File transfers](File-Transfers)) |
| tag | `T` | | into the batch, or out of it |
| view | `V` | PF6 | what is in a ZIP, tar or tar.gz: names, sizes, dates |
| describe, remove | `E`, `R` | PF5, PF9 | its uploader's or a sysop's |

In an area, `T` tags files by number (`1 3 5-7`), and `*` marks the
tagged ones. The batch is sent all at once by YMODEM, ZMODEM or Kermit
(XMODEM sends only one file). A file counts as downloaded, and leaves the
batch, once the protocol has moved on to the next file or the transfer
has ended successfully. The day's downloads and the
[ratio](Access-Levels) are checked for the whole batch before it starts.
On a 3270, files are uploaded and downloaded with IND$FILE on the area's
command line (see [TN3270](TN3270)).

```
; bbs.conf
[general]
files_dir = /var/lib/bbs/files
max_file_size = 64M
upload_quota = 256M        ; a user's uploads a day; sysops have none

[ftp]
address = :2121
passive_ports = 50000-50100
public_host = 203.0.113.7  ; behind NAT: the address passive replies give
tls = required             ; explicit FTPS: AUTH TLS before the login
cert_file = /etc/bbs/cert.pem
key_file = /etc/bbs/key.pem
advertise = bbs.example.org:2121
```

```
bbs filearea add Games "DOS games"
bbs filearea add Official "From the sysop" -sysop-upload
bbs file add Games ./doom.zip "Shareware Doom"
bbs file list Games
```

## The FTP server

The FTP server is built on
[ftpserverlib](https://github.com/fclairamb/ftpserverlib) with the BBS's
own file system: `/` lists the areas, each area lists its files, and
nothing else on the server's disk can be reached. Files are stored under
their record's number (`files_dir/1`, `files_dir/2` and so on), so a name a
user gives never becomes a path. Users can:

- download anything (resuming too);
- upload a new file into an area (not a sysop-only one), whose name is
  letters, digits and a few marks (no dots in front, no slashes, nothing a
  shell or Windows minds), up to `max_file_size` and what is left of
  `upload_quota`; an upload is written to a temporary file, hashed, and
  only then recorded and put in place;
- delete their own files (sysops any).

Everything else (overwriting, appending, resuming uploads, renaming,
directories, SITE, MFMT, CHMOD) is refused. Active mode is off, to prevent
FTP bounce attacks, and data connections must come from the same address
as the control connection. Logins are checked like any BBS login, with
its failure delay and throttling by address.

`tls = required` (the default) refuses `USER` before TLS, so no password
crosses the network in the clear. `implicit` uses TLS from the first byte.
`optional` and `no` are there for old clients, and `bbs check` warns about
them. `anonymous = yes` lets `anonymous` and `ftp` log in to download
only.

Open the FTP port and the passive port range in the firewall. An FTP
session is not a node: it does not appear in Who's online or count against
`max_nodes`. `[ftp] max_clients` and `max_per_ip` limit FTP sessions.

**See also:** [File transfers](File-Transfers), [Security](Security)
