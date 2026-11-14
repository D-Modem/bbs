The server of the HTTP API also shows image boards, at `/chan/`. They are
similar to kusaba&nbsp;x and to 4chan, which kusaba&nbsp;x copied (and
Futaba before the two). They have these items:

- Boards by directory (`/chan/b/`, `/chan/g/`).
- Threads that start with an image.
- Anonymous replies.
- Bumps, sage and tripcodes.

You need only the `bbs` binary. Go code makes the thumbnails, and the
pages load nothing from other locations.

The image boards are different from the [web forum](Web-Forum). They are
separate from the rest of the BBS. Posters do not need an account, and
their posts are not messages in the areas of the BBS. The accounts of the
BBS are for staff. Sysops are admins of all boards, and each board can
have its own mods.

```
bbs chan board add b Random "Anything goes." -nsfw
bbs chan board add g Technology "Computers, terminals and BBSes"
bbs chan board set g mods alice,bob
bbs chan board list
```

```ini
[http]
address = :8443
;chan = yes

[chan]
;dir = chan              ; images and thumbnails, a directory a board
;max_file = 4M           ; the largest image (a board can say otherwise)
;thread_wait = 60        ; seconds between an address's threads
;reply_wait = 15         ; and between its posts
;max_comment = 2000      ; characters
;strip_metadata = yes    ; take EXIF (where a phone took it) out of JPEGs
;style = yotsuba-b       ; the style everyone gets until they choose
;captcha = yes           ; those not signed in answer a captcha to post
;captcha_length = 6      ; its letters, 4 to 10
;captcha_level = normal  ; easy, normal or hard to read
;captcha_lifetime = 600  ; seconds one lasts
;options = yes           ; the post form's Options line (sage, nonoko)
;geoip = /var/lib/bbs/GeoLite2-Country.mmdb   ; for the boards with flags ([general]'s by default)
;geoip_credit = IP geolocation by DB-IP       ; its licence's credit, at each page's foot
;geoip_credit_url = https://db-ip.com
;ffmpeg = ffmpeg         ; videos' thumbnails, a frame (else a card saying what it is)
;pdftoppm = pdftoppm     ; PDFs' thumbnails, the first page
;max_video = 120         ; seconds a video may run (0: any)
;ruffle = /usr/share/ruffle   ; Ruffle's self-hosted files: Flash played in the page
;archive_days = 3        ; days a pruned thread stays in its board's archive (0: always)
```

| Setting | Default | What it sets |
|---|---|---|
| `chan` (in `[http]`) | `yes` | the image boards at `/chan/` |
| `dir` | `chan` | the location of images and thumbnails, and the secret of the boards (`dir/secret`, made the first time: keep it, or secure tripcodes change) |
| `max_file` | `4M` | the largest image; `bbs chan board set DIR max_file 8M` gives a board its own limit |
| `thread_wait`, `reply_wait` | `60`, `15` | flood control: it does not apply to staff |
| `max_comment` | `2000` | characters in a comment (and 100 lines) |
| `strip_metadata` | `yes` | the BBS removes EXIF, XMP and comments from JPEGs before it keeps them |
| `captcha` | `yes` | users who are not signed in must answer a captcha to post ([below](#captchas-and-members)); a board can have a different setting |
| `captcha_length` | `6` | the letters of the captcha, 4 to 10 |
| `captcha_level` | `normal` | how difficult it is to read: `easy` (letters turned a small quantity, one line through them), `normal`, `hard` (turned more, waved, three lines, more specks) |
| `captcha_lifetime` | `600` | seconds that a captcha is valid (60 to one day) |
| `options` | `yes` | the Options line of the post form (`sage`, `nonoko`); without it, all posts bump. A board can have a different setting |
| `style` | by the board | Yotsuba B on a worksafe board, Yotsuba on the other boards, as on 4chan |
| `ffmpeg`, `pdftoppm` | none | the programs that make the thumbnails of videos and PDFs (a name on the PATH, or a path); without them, a card that tells the type of file ([below](#videos-and-pdfs)) |
| `max_video` | `120` | the maximum length of a video in seconds (0: all lengths) |
| `archive_days` | `3` | days that a pruned thread stays in the archive of its board ([below](#the-archive)); 0 keeps them always |
| `ruffle` | none | the directory of the self-hosted package of Ruffle (`ruffle.js` and its `.wasm`): the page plays SWFs and FLVs ([below](#flash)); without it, users can only download them |

## On the terminals

The image boards are also on the text BBS and on 3270s. Use **B** on the
main menu (or a sysop's menu item `boards`). The terminals show these
items as messages:

- The boards.
- The threads of a board in bump order (replies, images, subject).
- A thread, one post at a time: the poster (tripcode, capcode), the
  thread, and below each post the posts that quote it.

The posters are the callers of the BBS. They post in the same way as
users on the web: a name (`Name#password` for a tripcode), `sage`, a
subject or no subject, staff as `## Admin` or `## Mod`. The same bans,
flood control, limits and pruning apply. Flood control uses the address
of the caller, or the account if there is no address (for example, a
modem caller).

The terminal shows the picture of a post above the post, as well as the
terminal can:

| Terminal | How |
|---|---|
| kitty, Ghostty | kitty's graphics protocol (by what they say they are) |
| sixel: xterm (`-ti vt340`), foot, mlterm, WezTerm, Windows Terminal, iTerm2, SyncTERM, the [web terminal](HTTP-API#the-web-terminal) | sixel |
| iTerm2 before sixel, mintty | iTerm2's inline images |
| all other UTF-8 terminals with color | colored half blocks (`▀`, 256 colors): rough, but you can see the picture |
| all other terminals, and 3270s | a description in words: `[cat.png, 640x480, 52 KB]` |

The thumbnail shows above the post. The thumbnail of a spoiler tells that
it is a spoiler. **V** shows the full image, as large as the screen
permits. A caller can set pictures off (or on) in the profile, **M**. The
setting stays.

To start a thread, send its image with the transfer protocol of the
terminal (ZMODEM, YMODEM, XMODEM, Kermit), if the terminal has one.
Terminals without a protocol (a 3270, the web terminal) can reply, or
start threads on `text_only` boards. The tools for staff (deletion, bans,
reports) are only on the web.

## Posting

The post form is at the top of the index of a board. It has these fields:
name, options, subject, comment, an image, and a password to delete the
post. The browser fills in the password, and keeps it in a cookie. A
thread starts with an image (unless the board is `text_only`). Replies
must have a comment, an image, or the two.

| Typed | Result |
|---|---|
| `Name#password` | a tripcode: `Name !WokonZwxw2`. It is the same as on 2channel and 4chan: `#tea` is `!WokonZwxw2` here also |
| `Name##password` | a secure tripcode, `!!...`, made with the secret of this site: nobody can find it with password tests on a different site |
| `sage` in Options | the reply does not bump its thread |
| `nonoko` in Options | after the post, the board shows, not the thread |
| `>text` | greentext |
| `>>123` | a link to post 123, `(OP)` if it starts its thread, struck through if it is deleted |
| `>>>/g/123`, `>>>/g/` | a post on a different board, or the board |
| `[spoiler]...[/spoiler]` | hidden until you move the pointer over it |
| `[code]...[/code]` | shown as code |
| a link | shown as a link |

**Spoiler?** shows the thumbnail of the image as *Spoiler Image* until a
user opens it. On a `forced_anon` board, users cannot use names.

Images are JPEG, PNG, GIF or WebP. The BBS identifies them by their
bytes, not by their names. The maximum is 10000 pixels on each side.
Thumbnails are JPEGs: 250 pixels for the first image of a thread and 125
for the image of a reply. The BBS refuses the same image two times on a
board, and gives a link to the first post (by its MD5, as on 4chan).
The BBS removes the EXIF of JPEGs before it keeps them. Thus, the photo
from a phone does not tell where it was taken.

## Captchas and members

Posters who are not signed in type the letters of a captcha below the
form. The BBS draws the captcha, with crooked letters over noise. No
outside service sees the posters. A captcha is valid for one post and for
ten minutes. **New one** gives a different captcha. The answer goes in
the form, sealed (AES-GCM, with the secret of the boards). Thus, the BBS
keeps only the fact that the captcha is used.

To set the captcha off, set `captcha = no` in `[chan]`. Use
`captcha_length`, `captcha_level` and `captcha_lifetime` to make it
longer or shorter, more or less difficult. The `captcha` setting of a
board (`yes`, `no`, `default`) replaces the setting of `[chan]` for that
board.

The users of the BBS sign in at `/chan/login` (**Sign in** at the top of
each page), and post without a captcha. The BBS marks their posts as the
posts of a member: a ✓ adjacent to the name, `"member": 1` in the JSON,
for all names that they give. They can also select **Sign it as NAME**.
Then the post shows the name of their account (underlined, a blue ✓,
`"account"` in the JSON). No other poster can post under that name. (All
posters can type a name.) On a `forced_anon` board, the BBS marks posts,
but it never signs them. On the terminals, all posters are signed in:
there is no captcha, `[member]` shows adjacent to the name, and
*Sign it as NAME?* shows before the post.

## Poster IDs and flags

On a board with `ids yes`, each poster gets an ID in each thread, as on
4chan. The ID has eight characters from a keyed hash of the address, the
board and the thread. Thus, a poster has the same ID in all of a thread
and a different ID in the next thread. Nobody can get the address from
the ID. Each ID has its own color. When you move the pointer over an ID,
it shows the number of posts by that ID in the thread. When you click the
ID, it marks all of them. On the terminals, the ID is adjacent to the
name: `Anonymous (ID: aQvqbFdJ)`.

A board with `flags yes` shows the flag of the country of each poster
adjacent to the name. The flag is an emoji, and its name shows when you
move the pointer over it. On the terminals, it is `[NZ]`. The country
comes from a country database of the sysop, `[chan] geoip` (by default
`[general] geoip`, the database that the rest of the BBS uses). These
formats are possible:

- A MaxMind DB file (`.mmdb`): GeoLite2 or GeoIP2 Country or City from
  MaxMind, *IP to Country Lite* from DB-IP, the files of IPinfo and other
  files in this format.
- A CSV of ranges, plain or gzipped: DB-IP's (`start,end,CC`) or
  IP2Location LITE DB1's (`"from","to","CC","Country"`, by numbers).

The BBS reads them itself (it downloads nothing). When you replace the
file, the BBS reads it again in less than one minute. For example, the
free file from DB-IP is a monthly download
(`https://download.db-ip.com/free/dbip-country-lite-YYYY-MM.mmdb.gz`; you
can use the gzipped file as it is). We checked it against MaxMind's own
reader, with 200,000 addresses that gave the same result. Its licence
(CC BY 4.0), as the licence of GeoLite2, asks for a credit:
`geoip_credit` and `geoip_credit_url` put it at the bottom of each page.

Posts without an address (a modem caller), or with an address that the
database does not know, have no flag. If the BBS is behind a reverse
proxy, put the proxy in `trusted_proxies`. If you do not do this, all
posters have the address of the proxy, and its ID and its flag. The BBS
keeps the ID and the flag with each post when the user posts it. If you
set them off, the posts that exist stay as they were.

## Threads

A reply bumps its thread to the top of the board until the thread has
`bump_limit` replies (300). A thread accepts `image_limit` images (150).
A board has `pages` pages of `per_page` threads (10 of 10). When a new
thread pushes the last thread past this limit, the BBS prunes it. It
**archives** the thread, or, on a board with `archive no`, it deletes the
thread and its images. Stickies stay at the top, and the BBS never prunes
them. Closed threads accept no replies (but staff can reply).

### The archive

As on 4chan, a pruned thread goes to the archive of its board
(`/chan/b/archive`, *[Archive]* adjacent to *[Catalog]*). It is not on
the pages and the catalog of the board, but it is complete, with its
files, and it is read-only. Nobody can reply to it (also not staff: it
is a record). Users cannot delete its posts with their passwords (staff
can delete them). Its page shows *This thread is archived* in place of
the reply form, and its first post has an *Archived* badge.

The archive lists the threads, the last archived first: number, subject
and the start of the comment, replies, date. `archive.json` is the same
as on 4chan: the numbers, oldest first. The JSON of an archived thread
has `"archived": 1` and `"archived_on"`.

After `[chan] archive_days` (3), the BBS removes a thread from the
archive, with its files. It does this check when a user makes a thread,
and when a user looks at the archive or the thread. `archive_days = 0`
keeps them always. `bbs chan board set DIR archive no` prunes the
threads of a board as before, and deletes them immediately. On the
terminals, the thread list of the board has *Archive* (A): the archived
threads, which you can read.

The index shows the first post of each thread and its last five replies
(one below a sticky), with the number of posts that it does not show. It
cuts a comment that is longer than 15 lines. The page of a thread shows
all of it, and below each post the posts that quote it. The **catalog**
shows all threads together: the image, the replies and images, the start
of the first post. The order is by bump or by date of creation.

With JavaScript (the pages also operate without it):

- Click the number of a post to quote it in the reply form.
- Click a thumbnail to open the image in its location.
- Move the pointer over a `>>123` to show that post.

## Deleting and reporting

To delete posts, do these steps:

1. Select the posts.
2. Type the password that you used for the posts. (The browser fills in
   its own password.)
3. Click **Delete**. **File Only** deletes their images and keeps the
   posts.

The ▸ adjacent to a post reports it to staff.

## Staff

Sysops and the mods of a board sign in at `/chan/login` with their BBS
account. They get **Manage** at the top of each page, an `[M]` adjacent
to each post, and an option to post as `## Admin` or `## Mod`.

- **Reports** (`/chan/manage`): the reports on their boards, the number
  of reports, and the reasons.
- **A post** (`[M]`): its address and the other posts from that address.
  Staff can delete the post (for the first post of a thread: the thread)
  or its file, make a thread sticky or closed, dismiss its reports,
  delete all posts of the address on the board, and ban.
- **Bans**: by address or by range (the /24 of IPv4, the /48 of IPv6), on
  the board or (admins) on all boards, for some days or for ever. As an
  option, the ban shows `(USER WAS BANNED FOR THIS POST)` below the post.
  When banned users try to post again, they see the reason, the end date,
  and their post. `/chan/manage/bans` lists the bans and removes them.

From the command line:

```
bbs chan board add DIR TITLE [DESCRIPTION] [-nsfw] [-forced-anon] [-text-only] [-locked] [-login] [-oekaki]   (-login: only users post)
bbs chan board set DIR KEY VALUE
bbs chan board list
bbs chan board del DIR            ; its posts and images too
bbs chan ban add ADDRESS DAYS [BOARD|-] [REASON...]
bbs chan ban list
bbs chan ban del ID
```

| Board setting | Default | What it sets |
|---|---|---|
| `title`, `description` | | the text in the banner of the board |
| `pages`, `per_page` | 10, 10 | the threads that the board keeps: pages of threads |
| `bump_limit`, `image_limit` | 300, 150 | the limits of a thread |
| `max_file` | `[chan]`'s | the largest image |
| `default_name` | Anonymous | the name for a post without a name |
| `nsfw` | no | not worksafe: Yotsuba, and not in the images of the front page |
| `forced_anon` | no | no names or tripcodes |
| `text_only` | no | threads do not need an image |
| `oekaki` | no | posters can draw their pictures in the page ([below](#oekaki)) |
| `ids` | no | each poster has an ID in a thread ([below](#poster-ids-and-flags)) |
| `flags` | no | posts show the flag of their country (with `[chan] geoip`) |
| `files` | none | the files that the board accepts in addition to images: `webm`, `mp4`, `pdf`, `swf`, `flv`, as many as necessary (`webm,pdf`); `none` for images only ([below](#videos-and-pdfs)) |
| `sound` | no | videos and Flash can have sound |
| `archive` | yes | the BBS archives pruned threads, read-only, for `archive_days` ([below](#the-archive)); `no` deletes them immediately |
| `locked` | no | only staff can post |
| `login` | `no` | `post`: only the users of the BBS can post (signed in at `/chan/login`); `all`: only they can see the board (its pages, images and JSON; the BBS sends other users to sign in, and the front page marks the board *(members)*) |
| `captcha` | `default` | `yes` or `no` for this board, or the setting of `[chan]` |
| `options` | `default` | `yes` or `no`: the post form of this board has the Options line (`sage`, `nonoko`), or the setting of `[chan]` |
| `mods` | | the users who moderate it, by name |
| `sort` | 0 | its position in the lists of boards |

## Videos and PDFs

A board can accept videos and PDFs in addition to images. 4chan accepts
WebM on all boards and PDF on /po/:

```
bbs chan board set wsg files webm,mp4
bbs chan board set wsg sound yes
bbs chan board set po files pdf
```

The BBS identifies the type of a file from its bytes, never from its
name. It reads each file before it keeps it:

| Format | Accepted | Refused |
|---|---|---|
| WebM | VP8, VP9 or AV1 video, Vorbis or Opus sound | a Matroska file that is not WebM (browsers do not play it), other codecs, other tracks, two videos in one file |
| MP4 | H.264, AV1 or VP9 video, AAC or Opus sound | HEVC and other codecs that not all browsers play; a file that is not complete (no movie header) |
| PDF | a complete file (`%PDF-` to `%%EOF`) | a file that is cut short |

These limits apply to a video:

- The maximum is 4096 pixels on each side.
- The maximum length is `max_video` seconds. While there is a limit, the
  BBS refuses a video that does not tell its length.
- The video has no sound, unless the `sound` setting of the board is yes.

The post of a video shows its size and length (`320x240, 0:05`). The
post of a PDF shows its pages if the BBS can count them
(`PDF, 2 pages`). In the page, the thumbnail of a video plays it in its
location. *[Close]* stops it. The thumbnail of a PDF opens it in a
different tab, in the viewer of the browser. The BBS serves the PDF as a
PDF, without the Content Security Policy of the boards, because a PDF
viewer does not operate with that policy. No program identifies it as a
different type.

Thumbnails: with `[chan] ffmpeg`, the thumbnail of a video is its first
frame. With `pdftoppm` (poppler-utils), the thumbnail of a PDF is its
first page. The BBS tells ffmpeg the container (`-f matroska`, `-f mov`),
and ffmpeg reads only the file. If one of these programs fails on a file,
the BBS refuses the file (a browser also cannot show it). Without these
programs, the thumbnail is a card that tells the type of file
(*WEBM 0:05*, *PDF 2 pages*). On the terminals, a video or a PDF shows as
its thumbnail. The file itself is for the browser.

## Flash

A board can also accept Flash, as /f/ on 4chan did.
[Ruffle](https://ruffle.rs), the Flash Player emulator, plays it in the
page:

```
make ruffle                      # Ruffle's self-hosted package, into ruffle/ (or unpack a release's)
bbs chan board set f files swf,flv
bbs chan board set f sound yes
```

```ini
[chan]
ruffle = /opt/bbs/ruffle         ; the directory with ruffle.js (some 30 MB, not in the BBS)
```

| Format | Read for | Notes |
|---|---|---|
| SWF (`FWS`, `CWS`, `ZWS`) | the size of its stage, frames and rate, sounds | the BBS reads compressed files as they unpack, and keeps nothing (it refuses a small file that unpacks to a large size, at no cost); Ruffle unpacks an LZMA file (`ZWS`), thus the BBS accepts it as 550x400 with sound |
| FLV | its `onMetaData` (size, length), its tags (sound, the codec of the video: Sorenson H.263, Screen, VP6, VP6 alpha, H.264) | without metadata, its length is the time of its last tag, and its size is the 320x240 of Flash |

A movie or FLV with sound needs the `sound` setting of the board. The
`max_video` limit applies to an FLV as to videos. With `[chan] ffmpeg`,
the thumbnail of an FLV is its first frame. The thumbnail of a movie is a
card (*FLASH 550x400*): only the browser draws Flash.

Click the thumbnail to play it in its location, with *[Close]*. These are
the security controls of the player:

- The player is a frame of `/chan/flash`. It is **sandboxed**
  (`allow-scripts` alone, by its Content Security Policy and by the
  frame). It does not have the origin of the site. Thus, it has no
  cookies, and it cannot access the page.
- The page downloads the file (with its own cookies, thus a board for
  members only also operates). It gives the bytes to the frame. The
  frame gives them to Ruffle with the network, scripting and links off
  (`allowNetworking: "none"`, `allowScriptAccess: false`,
  `openUrlMode: "deny"`).
- Ruffle does not play an FLV by itself, as Flash Player did not. The BBS
  makes its own small movie (`/chan/flvplayer.swf?w=&h=`, a `NetStream`
  played into a `Video` of the size of the FLV). This movie plays the FLV
  from a `blob:` of the frame.
- The BBS serves the files of Ruffle at `/chan/ruffle/` to all users
  (the frame has no origin to send). It serves only the files of the
  package (`.js`, `.wasm`, `.map`, no directories).

Ruffle decodes H.264 with the decoder of the browser (WebCodecs). Chrome,
Edge and Safari have this decoder. Ruffle itself decodes Sorenson, VP6
and Screen video.

## Oekaki

An oekaki board (お絵かき, doodling: `bbs chan board set oe oekaki yes`)
has a drawing board in its post form, as the boards of Futaba had their
Java painters. Select the size of the canvas (50 to 1200 pixels on each
side) and click **Open the drawing board**:

- **Pen**, **Eraser** and **Fill**, a color (or one from the palette), a
  brush size.
- **Undo** and **Redo** (Ctrl+Z, Ctrl+Y), **Clear**.
- **Use this drawing**, or post: the drawing goes as the image of the
  post, a PNG.

The PNG contains the record of the drawing: all strokes, with their
timing, in a separate chunk (`okST`). Other programs ignore this chunk,
thus the picture looks the same in all programs. Below a drawing,
**Replay** draws it again stroke by stroke, at 1× to 32× (pauses are
made shorter), until it is the same as the picture. The post shows how
long the drawing took. On an oekaki board, **Draw over** starts a reply
from the strokes of the drawing, to add to it. The replay of the new
drawing has the two. The strokes are in the file, thus a drawing that you
save and post again (on any board) can still replay. Programs that write
PNGs again can remove the chunk.

The drawing board and the replays need JavaScript. They are the only
pages of the boards that need it. Without JavaScript, an oekaki board is
an image board.

The server checks what it receives. A replay must have these properties:

- The size of this picture.
- Its strokes on the canvas.
- A maximum of one million numbers and 32 MB unpacked.
- A correct format.

If a replay does not have these properties, the BBS removes it from the
PNG, and keeps the PNG as a picture. The format, for programs that make
or read it, is zlib-compressed JSON:

```json
{"v": 1, "w": 300, "h": 200, "bg": "#ffffff",
 "s": [{"t": "p", "c": "#000000", "z": 4, "p": [0, 80, 180, 33, 80, 150]},
       {"t": "f", "c": "#22b14c", "z": 1, "p": [900, 150, 150]}]}
```

A stroke is the pen (`p`), the eraser (`e`: the pen in the color of the
background) or a fill (`f`, one point). `z` is its size, and `p` is its
points as `dt, x, y`. `dt` is the milliseconds since the previous point
(for the first point of a stroke, since the end of the previous stroke).
The time of the drawing is the sum of them.

## Styles

Callers select a style at the top of each page (the BBS keeps it in a
cookie):

| Style | What it looks like |
|---|---|
| `yotsuba` | 4chan's, on boards that are not worksafe: cream and maroon |
| `yotsuba-b` | 4chan's worksafe blue |
| `futaba` | Futaba's, as on kusaba |
| `burichan` | kusaba's blue |
| `photon` | kusaba's grey and orange |
| `tomorrow` | 4chan's dark style |

## JSON

You can read the boards as JSON, in the format of the read-only API of
4chan (github.com/4chan/4chan-API). Thus, readers for 4chan can also read
these boards:

| File | What it holds |
|---|---|
| `/chan/boards.json` | the boards and their settings |
| `/chan/b/threads.json` | the threads of each page, the time of the last change of each thread, their replies |
| `/chan/b/catalog.json` | the threads of each page, with their last replies |
| `/chan/b/1.json` | a page of the index |
| `/chan/b/thread/123.json` | a thread |

Posts have the `id`, `country` and `country_name` fields of 4chan if the
board has them. `boards.json` has its `user_ids` and `country_flags`. In
addition to the fields of 4chan:

- `boards.json` has `"oekaki": 1` for an oekaki board.
- A drawing that replays has `"replay": 1` and `"draw_ms"`.
- A board that accepts videos or PDFs has
  `"file_types": ["webm", "pdf"]`. With videos, it has the
  `webm_audio` and `max_webm_duration` fields of 4chan.
- `is_archived` is 1 for a board with an archive.
- The post of a video has `"duration_ms"`, and the post of a PDF has
  `"page_count"`, in addition to the `ext`, `w`, `h` fields of 4chan.

The file of a post is at `/chan/b/src/{tim}{ext}`, and its thumbnail is
at `/chan/b/thumb/{tim}s.jpg` (4chan has them at `i.4cdn.org/b/`). The
addresses of kusaba go to the correct location: `/chan/b/res/123.html`
to the thread, `/chan/b/1.html` to the index.

If the BBS is behind a reverse proxy, put the proxy in the
`trusted_proxies` setting of `[http]`. If you do not do this, all
posters have the address of the proxy, and flood control and bans apply
to all users. Refer to [HTTP API: behind a proxy](HTTP-API#behind-a-proxy).

**Refer to:** [Web forum](Web-Forum), [HTTP API](HTTP-API)
