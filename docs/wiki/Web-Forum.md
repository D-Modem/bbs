# Web forum

The HTTP API's server also serves the message areas as a web forum, at
`/forum/` (and `/`, which goes there). It is laid out like the bulletin
boards of their day: categories of forums, topics, and posts with their
authors beside them, in one of several [themes](#themes). The look is the
BBS's own CSS, built into the `bbs` binary; nothing is loaded from
elsewhere, and no script runs on the pages.

It is the same BBS: the same accounts, areas and messages. A post on the
forum is a message on the terminals, in QWK packets, on NNTP and FidoNet
(as its area is), and a terminal caller's reply shows in its topic.

```ini
[http]
address = :8443
;forum = yes
;forum_guests = yes
;forum_theme = ipb3
;forum_flags = yes
```

| setting | default | |
|---|---|---|
| `forum` | `yes` | the forum at `/forum/` (with `no`, `/` goes to the web terminal again) |
| `forum_guests` | `yes` | those not logged in may read the areas anyone may read (areas with a `read` requirement never) |
| `forum_theme` | `ipb3` | the theme callers get until they choose one ([below](#themes)) |
| `forum_flags` | `yes` | posters' countries' flags by their names ([below](#flags)) |

## The pages

| | |
|---|---|
| `/forum/` | the board: the areas by their group (the areas' `group`, else *Message Areas*), each with its topics, replies, newest post and whether it has any new; the board's statistics; who is online |
| `/forum/area/N` | an area's topics, newest activity first, 20 to a page; *Mark this forum read* |
| `/forum/area/N/new` | a new topic, with a poll if wanted |
| `/forum/topic/N` | a topic's posts, 20 to a page, and a reply box; the poll, if it has one |
| `/forum/post/N` | a message, found in its topic (links to a message from elsewhere) |
| `/forum/user/NAME` | a user's profile and recent posts |
| `/forum/online` | who is online, on every kind of line |
| `/forum/search?q=` | messages with the words (three letters at least) |
| `/forum/login`, `/forum/register` | logging in, and signing up (when `new_users` allows it) |

A *Terminal* tab goes to the [web terminal](HTTP-API#the-web-terminal)
when it is on.

## Topics

A topic is a message that replies to nothing in its area, and every reply
to it or to its replies, however deep. Replies on the terminals thread
into topics as they do here. When the first post of a topic is deleted,
the next oldest becomes its first. Topics are marked read as they are
read (per user, per topic), and an area's as the BBS's new scan counts
them; *Mark this forum read* marks the area read to its newest message,
on the terminals' new scan too.

Posting needs what posting on the terminals needs (the area's `post`
requirement; news-only areas take no posts). Posts are as on the BBS:
text, quoted with `> `, links made links when shown. Lines longer than a
terminal's (79 columns) are wrapped at spaces, not cut, so that the
terminals see the whole paragraph. The user's signature is added as on
the terminals, and an area that allows aliases lets the post be under one
(the real name is not shown).

On a post:

- **Quote** begins a reply with the post quoted (`name wrote:` and the
  lines with `> `), shown as a quote box.
- **Like** is the BBS's vote on a message (not on one's own).
- **Delete** is for the post's writer and the sysop.
- A poll is answered by those who may post in its area; answering again
  changes the answer.

## Flags

With a country database (`[general] geoip`) each post keeps its poster's
country, found when it is posted: by the address for those on the web,
telnet or SSH, by the caller ID for modem callers (`[general]
phone_country`, see [Modem Callers](Modem-Callers#where-callers-are)).
The forum shows it as a flag by the poster's name, in every theme (an
emoji, the country's name on hovering), and by each caller's line on
*Online Users*. Only the country is kept, never the address or number;
posts from before, from elsewhere (FidoNet, Usenet, QWK) or from where the
database does not know have none. `forum_flags = no` shows none (what is
kept stays). The database's `geoip_credit` goes at the foot of each page
while flags are shown. Behind a reverse proxy, name it in
`[http] trusted_proxies`, or every poster is in the proxy's country.

## Themes

| theme | |
|---|---|
| `ipb3` | the default: a board of the early 2010s in the manner of Invision Power Board 3: a dark user bar, a navy header with tabs, rounded blue category bars, posts with their author's column on the left |
| `phpbb33` | prosilver as phpBB 3.3 kept it: the 3.2 look, its list headings a little taller (3.3 changed little else) |
| `phpbb32` | prosilver of 2016-2020 in the manner of phpBB 3.2: the 3.1 layout with its pictures turned to icon-font glyphs in grey and the links' blue (drawn by the BBS: no icon font is loaded), names in the lists in normal weight |
| `phpbb31` | prosilver of 2014-2016 in the manner of phpBB 3.1: prosilver made responsive, in a white frame on a pale page; the header's search box with icon buttons, *Quick links* and the user's own menu (opened without script), breadcrumbs, white gradient buttons with red words (*Post Reply*, *New Topic*), icon-only post buttons, the last post's subject in the forum list, small coloured icons |
| `phpbb30` | prosilver: a board of 2007-2014 in the manner of phpBB 3.0's prosilver: a rounded blue gradient header with the board's name and a search box, a pale blue navigation bar with *Board index*, forums in rounded blue boxes with uppercase headings and pale gradient rows, round folder icons, white *NEW TOPIC* and *POST REPLY* buttons, posts in rounded panels with the poster on the right, *Who is online* and *Statistics*, a quick reply |
| `phpbb2` | silver: a PHP board of 2002-2008 in the manner of phpBB 2's subSilver: a grey page round a white panel, a rounded logo beside the board's title and a menu of little icons, tables in a thick blue border with blue gradient headings in orange, silver category bars fading to white, rows in three silvers, *new topic* and *post reply* pill buttons, *Posted / Post subject* above each post and *Back to top* beneath, *Who is Online* and a login box on the index |
| `vb6` | clean: vBulletin 6's restyling of vBulletin 5 Connect: the vb5 theme's pages under a white header with tabs underlined in blue, rounded cards with soft shadows on a cool grey page, blue buttons in place of orange, larger type in the system's own face, and a dark scheme when the reader's device asks for one |
| `vb5` | connect: a PHP board of 2013-2020 in the manner of vBulletin 5 Connect's default style: a flat navy header with the board's name and the member's menu, tabs that open onto a white row of links, a breadcrumb, flat white widgets with thin grey-blue edges on a pale page, forum and topic tables with small uppercase column heads and round photos for the last posters, orange *New Topic* and *Post Reply* buttons, posts with the poster on the left and small *Reply*, *Quote* and *Like* buttons beneath, *Who's Online* and *Forum Statistics* side by side, a navy foot |
| `vb4` | steel: a PHP board of 2010-2015 in the manner of vBulletin 4's default style: a white header with the board's name and a login strip, a deep blue tab bar (*Forum*, *What's New?*, *Community*) over a pale blue row of links, a search box, a breadcrumb with a big page title beneath, blocks with rounded blue gradient heads on a light grey page, round forum icons that glow blue for new posts, *Threads / Posts* and *Last Post* columns, threads with *Started by*, posts with the poster's details on the left and *Reply With Quote* beneath, *What's Going On?*, *Quick Reply*, *Post New Thread* and *Reply to Thread* buttons |
| `vb3` | blue: a PHP board of 2004-2008 in the manner of vBulletin 3.5's default style: a white page in a grey one under the board's name, a breadcrumb box with the welcome (or a login form) beside it and a strip of blue menu buttons beneath, tables in a thin navy border with steel-blue gradient category bars and darker column heads, rows in two pale lavenders, the forum's last thread, poster and time in the *Last Post* column, *What's Going On?* with active users and statistics, each post a table of its own (date and *#N* above, the poster's name large with *Join Date* and *Posts* on the left, small *Quote* buttons beneath), *Quick Reply*, a deep blue foot with the theme chooser |
| `vb2` | purple: a PHP board of 2001 in the manner of vBulletin 2.0: a white panel in a purple page, an oval logo over grey pill links, *Registered Members / Total Threads / Total Posts*, tables in a slate border with lilac headings and purple category bars in yellow, alternating greys, yellow *v* squares for new posts, *Last Post* in month-day-year with the time in grey, posts with *Junior Member*, *Registered* and *Posts* beside them and grey buttons beneath, *Currently Active Users*, a login row for guests |
| `vblite` | lavender: a PHP board of 2000 in the manner of vBulletin Lite 1.0: a white page, a chrome italic logo beside five little icon links, navy table headings, rows in two lavenders, round bulbs for new posts, italic white *new thread* and *post reply* buttons, *Posted by:* with the date above it, *All dates are in Month-Day-Year format* |
| `xf23` | light: XenForo 2.3's refresh of 2.0-2.2 (the xf2 theme's pages): rounder corners, pill-shaped buttons, page links and reactions, round pictures, softer borders, and the dark variation when the reader's device asks for one |
| `xf2` | xen: a PHP board of 2017-2023 in the manner of XenForo 2's default style: a deep blue head, a lighter blue navigation bar whose chosen tab opens onto a pale row of section links, flat white blocks with blue-tinted heads, forums with comment icons that turn bright blue when unread and *Threads* / *Messages* stacked beside them, a side column with *Members online* and *Forum statistics*, an orange *Post thread* button, posts with the poster in a tinted cell and *Like* and *Reply* beneath, a dark foot |
| `xf1` | classic: a PHP board of 2010-2017 in the manner of XenForo 1's default style: a deep navy head, tabs opening onto a pale blue row of links, a breadcrumb of arrow-ended crumbs, blue gradient category strips, speech-bubble forum icons with *Discussions* and *Messages* beneath, *Members Online Now*, posts with the poster in a pale box whose arrow points at the message, *Discussion in '…' started by …*, a blue *Post New Thread* pill |
| `mybb18` | flat: MyBB 1.8's default look over the earlier pages (the mybb16 theme's): a white head with the board's name and linked icons, a black glossy bar with search and welcome over a grey row of the member's links, rounded tables with deep blue heads and black glossy column rows, speech-bubble forum icons, posts with the author in a grey band across the top and their counts at the right, black buttons, a grey two-tier foot |
| `mybb16` | classic: MyBB 1.4 and 1.6's default look, 1.2's pages and colors (the mybb12 theme's) with each post a table of its own: a pale blue strip with the date and *Post: #1*, the author across the top with picture, title and stars and the counts at the right, quotes in plain boxes |
| `mybb12` | original: a PHP board of 2005-2008 in the manner of MyBB 1.0 and 1.2's default theme: a white frame on a light grey page, the board's name with a bold menu at the right, a grey *Welcome back* panel, tables in a steel-blue border with blue gradient heads and pale blue column rows over two greys, round forum lamps, *Board Statistics*, posts as rows with the author on the left and the date beneath, dashed quote boxes |
| `pun14` | oxygen 2: PunBB 1.3 and 1.4's Oxygen, a lighter look over pages of its own: a grey head with the board's name, a blue bar of links and a white welcome strip, each forum list and topic in a box with a blue head and a grey row naming the columns, bevelled square forum icons, posts with a grey *Reply by* strip and the date at the right, the author with picture and counts at the left, *Profile*, *Like* and *Quote* in a row beneath, *Quick reply* at the foot |
| `flux15` | air: FluxBB 1.4 and 1.5's Air over PunBB 1.2's pages (the pun12 theme's): a white rounded frame on a pale blue page, a slate-blue menu of tabs, teal headings over pale blue column rows, posts in pale blue with the author at the left, quotes in pale yellow boxes, Arial |
| `pun12` | oxygen: a lightweight PHP board of 2004-2008 in the manner of PunBB 1.2's (and FluxBB 1.2's) Oxygen: small Verdana, a blue title bar with a menu of plain links, tables of forums with blue heads and grey column rows, square forum lamps, posts with a blue strip giving the number and date, the author at the left on grey, *Quick post* at the foot, *Board information* |
| `yabb25` | silver: YaBB 2.5's default look over 2's pages (the yabb2 theme's): a silver page in a grey frame, a glossy clock strip, glossy grey tabs with the chosen one lit and a search box, each category under a rounded glossy title, boards on pale grey rows shaded at the top with a grey folder and a glowing orange orb for new posts, posts with the poster across the top on a grey band over the message, in rounded grey frames |
| `yabb2` | default: YaBB 2.0-2.4's default template over 1's pages (the yabb1 theme's): a blue-white page, tables in grey frames with a pale ledge, a bright blue title row with the greeting and a search box, the way here in bold blue, pale blue category rows with a speech balloon, rounded lamps with a tick (orange for new), the last post before the counts, posts ruled off in pale blue with smaller grey type, little validator buttons at the foot |
| `yabb1` | gold: a Perl board of 2001-2004 in the manner of YaBB 1 Gold: everything in tables ruled with a one-pixel slate grid, a head with the board's name and *Hey, alice* over a pale menu row of words split by bars, the way here as a tree of open folders, pale steel title and category rows, manila folders for boards and topics (a blue badge for new, a flag when hot), posts with the poster and their stars at the left, *Reply #1 on: 10/12/26 at 23:12:05*, an *Info Center* |
| `smf30` | natural: Simple Machines Forum 3.0's refinement of 2.1 (the smf21 theme's pages): the top strip and the menu each on a soft blue-to-white band, a rounded breadcrumb, a deep slate foot, and a dark scheme in slate blues and warm orange when the reader's device asks for one |
| `smf21` | simple: Simple Machines Forum 2.1's default look, grown out of Curve (the smf20 theme's pages): a white strip across the top with the member's name and a search box, a dark blue page edge, a bordered frame with the board's name in orange, orange pills for the current menu item, a boxed breadcrumb, flat steel-blue category bars, title bars edged in orange, posts as bordered boxes, small capital buttons in a row, a dark blue foot |
| `smf20` | curve: a PHP board of 2011-2019 in the manner of Simple Machines Forum 2.0's Curve: a slate blue band fading to a pale page, a white rounded frame, *Hello* with the member's picture and the news beside it, rounded menu buttons, a breadcrumb with little arrows, rounded steel-blue category bars over rounded pale-blue blocks, orange board and poster names, *Posts / Topics* and *Last post by … in … on …*, an *Info Center*, posts with the poster's group, stars and post count on the left and *« Reply #1 on: … »* above, *Quick Reply* |
| `smf1` | core: a PHP board of 2005-2010 in the manner of Simple Machines Forum 1's Core look: a pale lilac-grey page, a box with a blue gradient head carrying the board's name, *Hello* and the time, the member's picture and links, a news line, dark blue menu tabs hanging below in small capitals, blue category bars over grey-lavender cells, an *Info Center*, posts with the poster's name, group and stars on the left, *Quote* and *Remove* beside each subject |
| `phpbb1` | dark: a PHP board of 2001 in the manner of phpBB 1: a black page with white text and teal links, a glowing italic logo, glowing *New Topic* and *Reply* buttons, [bracketed] menu links, the *-N-* statistics block, near-black table lines with cells in alternating grey and navy, small folder icons, posts between rules with little labelled links beneath |
| `ikon` | grey: a Perl board of 2001 in the manner of Ikonboard 2: a narrow centered page, grey tables with one-pixel lines between cells, a title box with a menu row and a *Welcome* box, a brushed-metal logo beside a folder tree of where you are, dark red links, grey bevelled buttons, folder badges and gem icons, posts with *Total Posts, Joined, Posted on* beneath |
| `ipb1` | retro: a board of the early 2000s in the manner of Invision Power Board 1: tables outlined in slate blue with white lines between their cells, bright title bars, a flat menu box, round forum badges and envelope topic icons, rounded glossy buttons, *Posted on* and a post links bar |
| `ipb5` | fresh: a community suite of the mid-2020s in the manner of Invision Community 5: a slate header with the board's name, its sections and the member's menu over a darker band with the breadcrumb and a round search box, a cool grey page, white boxes with soft corners and shadows, rounded square forum icons that turn blue for unread content, *Topics* and *Posts* counts, round photos, *Who's Online* and statistics in a side column, posts with the author beside them and pill-shaped reactions beneath, a reply box with the poster's photo, a slate foot |
| `ipb4` | modern: a community suite of the late 2010s in the manner of Invision Community 4.7: a pale page, a white header with the board's name and the user's menu, a blue navigation bar with white tabs and a search box, white boxes with soft shadows, big round forum icons, stacked post counts, round photos, *Who's Online* and statistics beside the forums, posts with the author's pane on the left, *2 hours ago* times, likes as reactions, a reply box with the poster's photo |
| `ipb2` | classic: a board of the mid-2000s in the manner of Invision Power Board 2: a white page, boxes outlined in navy, blue gradient title bars, pale blue rows, small Verdana; *Logged in as* and *Welcome Guest* bars, *Last Post Info*, *Fast Reply*, big blue buttons |

All are this BBS's own CSS and templates, drawn to look like the boards
they recall; no code or images are taken from them. `forum_theme` is the
forum's; anyone (guests too) may choose another at the foot of every page,
kept in a cookie (`bbs_forum_theme`) for a year. A theme lays out the
board, topic lists, topics and profiles its own way, and shows the rest
(posting, searching, signing in) in its colors. A theme may build on another's templates (the phpBB 3.2 and 3.3 themes wear 3.1's, vBulletin 6's wears 5's, SMF 3.0's and 2.1's wear Curve's, XenForo 2.3's wears 2's, MyBB 1.8's wears 1.6's, which wears 1.2's, FluxBB 1.5's wears PunBB 1.2's, and YaBB 2.5's wears 2's, which wears 1's, with stylesheets of their own).

## Logins

A login is the HTTP API's session token (lasting `session_hours`), kept in
an `HttpOnly`, `SameSite=Lax` cookie for `/forum` alone (`Secure` with
TLS). Logins are throttled and logged as anywhere else; a token for the
API is not taken as a cookie. Logging out revokes the token. Every form a
logged-in user sends carries a token of their session's, and is refused
without it.

The pages allow nothing from elsewhere (`Content-Security-Policy:
default-src 'self'`), no framing, and keep no copies (`no-store`).
Requests count against the API's `rate` per address.

**See also:** [HTTP API](HTTP-API), [Message areas](Message-Areas), [Writing messages](Writing-Messages)
