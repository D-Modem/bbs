File areas contain files that callers can download and upload. Callers
look at the file areas on the BBS (*F* on the main menu). They can also
use an FTP client, and log in with their BBS name and password. On FTP,
each area is a directory.

## On the BBS

| Action | Key | PF | What it does |
|---|---|---|---|
| an area | its number | `S` adjacent to it | shows its files, by name |
| new files | `N` | PF5 | shows the files that came after your last call (the last 30 days on a first call), in all areas, newest first |
| find | `F` | PF10 | shows the files that have the words in their name or descriptions (with upper or lower case) |
| batch | `B` | | shows the tagged files, to download them together (text terminals) |

The details of a file show its description. For an archive, they also
show a longer description. If an uploaded archive has a `FILE_ID.DIZ` at
its top level, that file becomes the long description. If the uploader
did not give a description, the first line of that file with useful text
becomes the description. (The BBS decodes CP437 art, removes colours and
control characters, and keeps a maximum of 20 lines.) From the details,
a caller can do these actions:

| Action | Key | PF | Notes |
|---|---|---|---|
| download | `D` | | with a transfer protocol ([File transfers](File-Transfers)) |
| tag | `T` | | puts the file into the batch, or removes it |
| view | `V` | PF6 | shows the contents of a ZIP, tar or tar.gz: names, sizes, dates |
| describe, remove | `E`, `R` | PF5, PF9 | for the uploader or a sysop |

In an area, `T` tags files by number (`1 3 5-7`). A `*` shows the tagged
files. The BBS sends the full batch together with YMODEM, ZMODEM or
Kermit (XMODEM sends only one file). A file counts as downloaded, and
leaves the batch, when the protocol starts the next file or when the
transfer ends correctly. Before the batch starts, the BBS checks the
downloads of the day and the [ratio](Access-Levels) for the full batch.
On a 3270, callers upload and download files with IND$FILE on the
command line of the area (refer to [TN3270](TN3270)).

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

The FTP server uses
[ftpserverlib](https://github.com/fclairamb/ftpserverlib) with the file
system of the BBS. `/` lists the areas, and each area lists its files.
Users cannot get to other items on the disk of the server. The BBS
stores each file with the number of its record (`files_dir/1`,
`files_dir/2` and other numbers). Thus, a name from a user never becomes
a path. Users can do these actions:

- Download all files (also with resume).
- Upload a new file into an area (but not into an area for the sysop
  only). The name must contain only letters, digits and some marks (no
  dots at the start, no slashes, no characters that cause problems for a
  shell or Windows). The maximum size is `max_file_size` and the
  remainder of `upload_quota`. The BBS writes an upload to a temporary
  file and calculates its hash. Only then does it record the file and
  put it in its location.
- Delete their own files (sysops can delete all files).

The server refuses all other actions (overwrite, append, resume of
uploads, rename, directories, SITE, MFMT, CHMOD). Active mode is
disabled, to prevent FTP bounce attacks. Data connections must come from
the same address as the control connection. The BBS checks logins in the
same way as all BBS logins, with the failure delay and the throttling by
address.

`tls = required` (the default) refuses `USER` before TLS. Thus, no
password goes across the network in clear text. `implicit` uses TLS from
the first byte. `optional` and `no` are for old clients, and `bbs check`
gives a warning about them. `anonymous = yes` lets `anonymous` and `ftp`
log in, to download only.

Open the FTP port and the passive port range in the firewall. An FTP
session is not a node. It does not show in Who's online, and it does not
count against `max_nodes`. `[ftp] max_clients` and `max_per_ip` set the
limits of FTP sessions.

**See also:** [File transfers](File-Transfers), [Security](Security)
