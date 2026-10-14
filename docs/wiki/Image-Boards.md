# Image boards

The HTTP API's server also serves image boards, at `/chan/`, in the manner
of kusaba&nbsp;x and of 4chan that it followed (and Futaba before both):
boards by directory (`/chan/b/`, `/chan/g/`), threads that start with an
image, anonymous replies, bumping, sage and tripcodes. Nothing is needed
but the `bbs` binary: thumbnails are made in Go, and the pages load
nothing from elsewhere.

Unlike the [web forum](Web-Forum), the image boards are a world of their
own: posters need no account, and their posts are not messages in the
BBS's areas. The BBS's accounts are for staff: sysops are admins of every
board, and each board can name its own mods.

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
;geoip = /var/lib/bbs/GeoLite2-Country.mmdb   ; for the boards with flags
;geoip_credit = IP geolocation by DB-IP       ; its licence's credit, at each page's foot
;geoip_credit_url = https://db-ip.com
```

| setting | default | |
|---|---|---|
| `chan` (in `[http]`) | `yes` | the image boards at `/chan/` |
| `dir` | `chan` | where images and thumbnails are kept, and the boards' secret (`dir/secret`, made the first time: keep it, or secure tripcodes change) |
| `max_file` | `4M` | the largest image; `bbs chan board set DIR max_file 8M` gives a board its own |
| `thread_wait`, `reply_wait` | `60`, `15` | flood control: staff are not held to it |
| `max_comment` | `2000` | characters in a comment (and 100 lines) |
| `strip_metadata` | `yes` | JPEGs' EXIF, XMP and comments taken out before they are kept |
| `captcha` | `yes` | those not signed in answer a captcha to post ([below](#captchas-and-members)); a board may say otherwise |
| `captcha_length` | `6` | the captcha's letters, 4 to 10 |
| `captcha_level` | `normal` | how hard it is to read: `easy` (letters barely turned, a line through them), `normal`, `hard` (turned further, waved, three lines, more specks) |
| `captcha_lifetime` | `600` | seconds a captcha lasts (60 to a day) |
| `options` | `yes` | the post form's Options line (`sage`, `nonoko`); without it posts always bump. A board may say otherwise |
| `style` | by the board | Yotsuba B on a worksafe board, Yotsuba on the rest, as 4chan does |

## On the terminals

The image boards are on the text BBS and on 3270s too: **B** on the main
menu (or a sysop's menu item `boards`). The boards, a board's threads by
bump order (replies, images, subject), and a thread read a post at a
time, as messages: who posted (tripcode, capcode), the thread, and under
each post the posts that quote it. The posters are the BBS's callers,
posting as anyone on the web does: a name (`Name#password` for a
tripcode), `sage`, a subject or none, staff as `## Admin` or `## Mod`;
the same bans, flood control (by the caller's address, or by their
account where there is none: a modem's), limits and pruning.

A post's picture is shown above it, as well as the terminal can:

| terminal | how |
|---|---|
| kitty, Ghostty | kitty's graphics protocol (by what they say they are) |
| sixel: xterm (`-ti vt340`), foot, mlterm, WezTerm, Windows Terminal, iTerm2, SyncTERM, the [web terminal](HTTP-API#the-web-terminal) | sixel |
| iTerm2 before sixel, mintty | iTerm2's inline images |
| any other UTF-8 terminal with colour | coloured half blocks (`▀`, 256 colours): rough, but the picture |
| the rest, and 3270s | what it is, in words: `[cat.png, 640x480, 52 KB]` |

The thumbnail goes above the post (a spoiler's says it is one); **V**
shows the image itself, as large as the screen allows. A caller turns
pictures off (or on) on their profile, **M**, and it stays so. A thread
is started with its image sent by the terminal's transfer protocol
(ZMODEM, YMODEM, XMODEM, Kermit), where it has one; terminals without
(a 3270, the web terminal) reply, or start threads on `text_only`
boards. Staff's tools (deleting, bans, reports) are the web's.

## Posting

A board's index has the post form at its top: name, options, subject,
comment, an image, and a password to delete the post by (filled in for
each browser, which keeps it in a cookie). A thread starts with an image
(unless the board is `text_only`); replies need a comment, an image, or
both.

| typed | |
|---|---|
| `Name#password` | a tripcode: `Name !WokonZwxw2`. The same as 2channel's and 4chan's: `#tea` is `!WokonZwxw2` here too |
| `Name##password` | a secure tripcode, `!!...`, made with this site's secret: it cannot be found by trying passwords elsewhere |
| `sage` in Options | the reply does not bump its thread |
| `nonoko` in Options | back to the board after posting, not the thread |
| `>text` | greentext |
| `>>123` | a link to post 123, `(OP)` if it starts its thread, struck through if it is gone |
| `>>>/g/123`, `>>>/g/` | a post on another board, or the board |
| `[spoiler]...[/spoiler]` | hidden until hovered |
| `[code]...[/code]` | set as code |
| a link | made a link |

**Spoiler?** shows the image's thumbnail as *Spoiler Image* until it is
opened. Names cannot be used on a `forced_anon` board.

Images are JPEG, PNG, GIF or WebP, told by their bytes, not their names;
up to 10000 pixels a side. Thumbnails are JPEGs, 250 pixels for a thread's
first image and 125 for a reply's. The same image twice on a board is
refused, with a link to where it is (by its MD5, as 4chan does). JPEGs
lose their EXIF before they are kept, so a phone's photo does not say
where it was taken.

## Captchas and members

Posters who are not signed in type the letters of a captcha under the
form: drawn here, crooked over noise (no outside service sees the
posters), good for one post and ten minutes; **New one** gives another.
Its answer travels sealed in the form (AES-GCM, with the boards' secret),
so nothing is kept of it but that it has been used. `captcha = no` in
`[chan]` turns it off, `captcha_length`, `captcha_level` and
`captcha_lifetime` make it longer or shorter, harder or easier; a board's
own `captcha` (`yes`, `no`, `default`) says otherwise than `[chan]` for
it.

The BBS's users sign in at `/chan/login` (**Sign in** at the top of each
page) and post without one. Their posts are marked as a member's: a ✓
beside the name, `"member": 1` in the JSON, whatever name they gave. They
may also tick **Sign it as NAME**: the post then carries their account's
name (underlined, a blue ✓, `"account"` in the JSON), which no one else
can post under, as anyone can type a name. On a `forced_anon` board posts
are marked, never signed. On the terminals every poster is signed in: no
captcha, `[member]` by the name, and *Sign it as NAME?* before the post.

## Poster IDs and flags

A board with `ids yes` gives each poster an ID in each thread, as 4chan's
do: eight characters from a keyed hash of their address, the board and
the thread, so a poster has the same ID all through a thread and another
in the next, and nothing of their address can be had from it. Each ID has
a colour of its own; hovering it says how many posts in the thread are
by it, and clicking it marks them all. On the terminals it is by the
name: `Anonymous (ID: aQvqbFdJ)`.

A board with `flags yes` shows the flag of each poster's country by their
name (an emoji, its name on hovering; `[NZ]` on the terminals), from a
country database the sysop has, `[chan] geoip`:

- a MaxMind DB file (`.mmdb`): MaxMind's GeoLite2 or GeoIP2 Country or
  City, DB-IP's *IP to Country Lite*, IPinfo's and others in its format;
- or a CSV of ranges, plain or gzipped: DB-IP's (`start,end,CC`) or
  IP2Location LITE DB1's (`"from","to","CC","Country"`, by numbers).

The BBS reads them itself (nothing is fetched); replace the file and it
is read again within a minute. DB-IP's free one, for instance, is a
monthly download (`https://download.db-ip.com/free/dbip-country-lite-YYYY-MM.mmdb.gz`,
gzipped as it comes is fine); it was checked against MaxMind's own reader,
200,000 addresses alike. Its licence (CC BY 4.0), as GeoLite2's, asks
for a credit: `geoip_credit` and `geoip_credit_url` put it at the foot of
each page. Posts from no address (a modem's caller)
or one the database does not know have no flag. Behind a reverse proxy
name it in `trusted_proxies`, or every poster is the proxy and has its ID
and its flag. Both are kept with each post when it is made: turning them
off leaves those already made as they were.

## Threads

A reply bumps its thread to the top of the board until the thread has
`bump_limit` replies (300); a thread takes `image_limit` images (150).
A board has `pages` pages of `per_page` threads (10 of 10); when a new
thread pushes the last one past them, it is pruned, its images with it.
Stickies stay at the top and are never pruned; closed threads take no
replies (but staff's).

The index shows each thread's first post and its last five replies (one
under a sticky), with how many were left out; a comment longer than 15
lines is cut there. A thread's page has all of it, and under each post
the posts that quote it. The **catalog** shows every thread at a glance:
its image, its replies and images, the start of its first post; by bump
order or by when it was made.

With JavaScript (the pages work without it): clicking a post's number
quotes it in the reply form, clicking a thumbnail opens the image in place,
and hovering a `>>123` shows that post.

## Deleting and reporting

Tick posts, give the password they were posted with (the browser's own is
filled in), and **Delete**; **File Only** deletes their images and leaves
the posts. The ▸ beside a post reports it to staff.

## Staff

Sysops and a board's mods sign in at `/chan/login` with their BBS account
and get **Manage** at the top of each page, an `[M]` beside each post, and
the choice of posting as `## Admin` or `## Mod`.

- **Reports** (`/chan/manage`): what has been reported on their boards,
  how often, and why.
- **A post** (`[M]`): its address and that address's other posts; delete
  the post (a thread's first: the thread) or its file; make a thread
  sticky or closed; dismiss its reports; delete every post of the address
  on the board; and ban.
- **Bans**: by address or by range (the /24 of IPv4, the /48 of IPv6), on
  the board or (admins) on every board, for some days or for ever,
  optionally saying `(USER WAS BANNED FOR THIS POST)` under the post. The
  banned see why, until when, and what they posted, when they next post.
  `/chan/manage/bans` lists them and lifts them.

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

| board setting | default | |
|---|---|---|
| `title`, `description` | | as the board's banner shows them |
| `pages`, `per_page` | 10, 10 | threads kept: pages of threads |
| `bump_limit`, `image_limit` | 300, 150 | a thread's |
| `max_file` | `[chan]`'s | the largest image |
| `default_name` | Anonymous | a post with no name |
| `nsfw` | no | not worksafe: Yotsuba, and left off the front page's images |
| `forced_anon` | no | no names or tripcodes |
| `text_only` | no | threads need no image |
| `oekaki` | no | posters can draw their pictures in the page ([below](#oekaki)) |
| `ids` | no | each poster has an ID in a thread ([below](#poster-ids-and-flags)) |
| `flags` | no | posts show their country's flag (with `[chan] geoip`) |
| `locked` | no | only staff post |
| `login` | `no` | `post`: only the BBS's users post (signed in at `/chan/login`); `all`: only they see the board at all (its pages, images and JSON; others are sent to sign in, and the front page marks it *(members)*) |
| `captcha` | `default` | `yes` or `no` for this board, else `[chan]`'s |
| `options` | `default` | `yes` or `no`: this board's post form has the Options line (`sage`, `nonoko`), else as `[chan]` says |
| `mods` | | users who moderate it, by name |
| `sort` | 0 | where it comes in the lists of boards |

## Oekaki

An oekaki board (お絵かき, doodling: `bbs chan board set oe oekaki yes`)
has a drawing board in its post form, as Futaba's boards had their Java
painters. Choose the canvas's size (50 to 1200 pixels a side) and **Open
the drawing board**:

- **Pen**, **Eraser** and **Fill**, a colour (or one from the palette), a
  brush size;
- **Undo** and **Redo** (Ctrl+Z, Ctrl+Y), **Clear**;
- **Use this drawing**, or just post: the drawing goes as the post's image,
  a PNG.

The PNG carries how it was drawn: every stroke, with its timing, in a
chunk of its own (`okST`) that other programs pass over, so the picture
looks the same anywhere. Under a drawing, **Replay** draws it again
stroke by stroke, at 1× to 32× (pauses cut short), ending as the picture
is; the post says how long the drawing took. On an oekaki board **Draw
over** starts a reply from the drawing's strokes, to add to it: the new
drawing's replay has both. Since the strokes are in the file, a drawing
saved and posted again (on any board) still replays; programs that
rewrite PNGs may drop the chunk.

The drawing board and the replays need JavaScript (the boards' only
pages that do); without it, an oekaki board is an image board.

What the server takes is checked: a replay must be of this picture's
size, its strokes on the canvas, at most a million numbers and 32 MB
unpacked, and well formed. One that is not is taken out of the PNG, which
is kept as a picture. The format, for programs that want to make or read
it, is zlib-compressed JSON:

```json
{"v": 1, "w": 300, "h": 200, "bg": "#ffffff",
 "s": [{"t": "p", "c": "#000000", "z": 4, "p": [0, 80, 180, 33, 80, 150]},
       {"t": "f", "c": "#22b14c", "z": 1, "p": [900, 150, 150]}]}
```

A stroke is the pen (`p`), the eraser (`e`: the pen in the background's
colour) or a fill (`f`, one point); `z` is its size, and `p` its points as
`dt, x, y`, `dt` the milliseconds since the point before (for a stroke's
first point, since the stroke before ended). The drawing's time is the
sum of them.

## Styles

Callers choose one at the top of each page (kept in a cookie):

| style | |
|---|---|
| `yotsuba` | 4chan's, on boards not worksafe: cream and maroon |
| `yotsuba-b` | 4chan's worksafe blue |
| `futaba` | Futaba's, as kusaba had it |
| `burichan` | kusaba's blue |
| `photon` | kusaba's grey and orange |
| `tomorrow` | 4chan's dark style |

## JSON

The boards can be read as JSON, in the shapes of 4chan's read-only API
(github.com/4chan/4chan-API), so that its readers read these boards too:

| | |
|---|---|
| `/chan/boards.json` | the boards and how they are run |
| `/chan/b/threads.json` | each page's threads, when each changed, their replies |
| `/chan/b/catalog.json` | each page's threads, with their last replies |
| `/chan/b/1.json` | a page of the index |
| `/chan/b/thread/123.json` | a thread |

Posts have 4chan's `id`, `country` and `country_name` where the board
has them, and `boards.json` its `user_ids` and `country_flags`. Beyond
4chan's fields, `boards.json` says `"oekaki": 1` of an oekaki board, and
a drawing that replays has `"replay": 1` and `"draw_ms"`.

A post's image is at `/chan/b/src/{tim}{ext}` and its thumbnail at
`/chan/b/thumb/{tim}s.jpg` (where 4chan's are at `i.4cdn.org/b/`).
kusaba's addresses go where they should: `/chan/b/res/123.html` to the
thread, `/chan/b/1.html` to the index.

## Not done

Video and other files (WebM, PDF: 4chan's needs ffmpeg), the archive of
pruned threads.

Behind a reverse proxy, name it in `[http]`'s `trusted_proxies`: otherwise
every poster has the proxy's address, and flood control and bans reach
everyone. See [HTTP API: behind a proxy](HTTP-API#behind-a-proxy).

**See also:** [Web forum](Web-Forum), [HTTP API](HTTP-API)
