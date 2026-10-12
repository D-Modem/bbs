File areas hold files; callers browse them from the BBS (*F* on the main
menu) and move them with an FTP client, logging in with their BBS name
and password: each area is a directory.

## On the BBS

| | key | PF | |
|---|---|---|---|
| an area | its number | `S` by it | its files, by name |
| new files | `N` | PF5 | the files added since your last call (the last 30 days on a first call), in every area, newest first |
| find | `F` | PF10 | files whose name or descriptions have the words (in any case) |
| batch | `B` | | the files tagged, to download together (text terminals) |

A file's details show its description and, for an archive, its longer
description: an archive uploaded with a `FILE_ID.DIZ` (in the archive's
top level) is described by it, the first line that says something
becoming its description unless the uploader gave one (CP437 art is
decoded, colors and control characters left out, at most 20 lines).
From the details:

| | key | PF | |
|---|---|---|---|
| download | `D` | | by a transfer protocol ([File transfers](File-Transfers)) |
| tag | `T` | | into the batch, or out of it |
| view | `V` | PF6 | what is in a ZIP, tar or tar.gz: names, sizes, dates |
| describe, remove | `E`, `R` | PF5, PF9 | its uploader's or a sysop's |

In an area, `T` tags by numbers (`1 3 5-7`); `*` marks those tagged.
The batch goes by YMODEM, ZMODEM or Kermit (XMODEM sends one file), all
at once; a file counts as downloaded once the protocol has gone on to
the next or the transfer has ended well, and leaves the batch.  The day's
downloads and the [ratio](Access-Levels) are checked for the whole batch
before it starts.  On a 3270, files come and go by IND$FILE on the
area's command line ([TN3270](TN3270)).

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

The FTP side is [ftpserverlib](https://github.com/fclairamb/ftpserverlib)
with a file system of the BBS's own: `/` lists the areas, an area its
files, and nothing of the server's disk is reachable.  Files are stored
under their record's number (`files_dir/1`, `files_dir/2`, ...), so no name
a user gives becomes a path.  Users may:

- download anything (resuming too);
- upload a new file into an area (not a sysop-only one), whose name is
  letters, digits and a few marks (no dots in front, no slashes, nothing a
  shell or Windows minds), up to `max_file_size` and what is left of
  `upload_quota`; an upload is written to a temporary file, hashed, and
  only then recorded and put in place;
- delete their own files (sysops any).

Everything else (overwriting, appending, resuming uploads, renaming,
directories, SITE, MFMT, CHMOD) is refused.  Active mode is off (FTP
bounce), data connections must come from the control connection's
address, and the login is the BBS's, with its failure delay and its
throttling by address.  `tls = required` (the default) refuses `USER`
before TLS, so no password crosses in the clear; `implicit` is TLS from
the first byte; `optional` and `no` are there for old clients, and `bbs
check` warns of them.  `anonymous = yes` lets `anonymous` and `ftp` in to
download only.

Open the FTP port and the passive range in the firewall.  An FTP session
is not a node: it does not show in who's online or count against
`max_nodes`; `[ftp] max_clients` and `max_per_ip` limit it.

**See also:** [File transfers](File-Transfers), [Security](Security)
