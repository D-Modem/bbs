The server of the HTTP API also shows the message areas as a web forum.
The forum is at `/forum/`. The path `/` also goes to the forum. The layout
is similar to the bulletin boards of their time. The forum has categories
of forums, topics, and posts. The name of the author is adjacent to each
post. You can select one of many [themes](#themes). The CSS is the BBS's
own CSS, and it is in the `bbs` binary. The pages load nothing from other
locations, and no script runs on the pages.

The forum is the same BBS. It has the same accounts, areas and messages.
A post on the forum is also a message on the terminals, in QWK packets,
on NNTP and on FidoNet (if its area is on these networks). When a
terminal caller replies, the reply shows in the topic.

```ini
[http]
address = :8443
;forum = yes
;forum_guests = yes
;forum_theme = ipb3
;forum_flags = yes
```

| Setting | Default | What it sets |
|---|---|---|
| `forum` | `yes` | the forum at `/forum/` (with `no`, `/` goes to the web terminal again) |
| `forum_guests` | `yes` | users who are not logged in can read the areas that all users can read (never areas with a `read` requirement) |
| `forum_theme` | `ipb3` | the theme that callers get until they select a theme ([below](#themes)) |
| `forum_flags` | `yes` | the flag of the poster's country adjacent to the name ([below](#flags)) |

## The pages

| Page | What it shows |
|---|---|
| `/forum/` | the board: the areas in their group (the `group` of the area, or *Message Areas*), each with its topics, replies, newest post and an indication of new posts; the statistics of the board; the users who are online |
| `/forum/area/N` | the topics of an area, newest activity first, 20 on each page; *Mark this forum read* |
| `/forum/area/N/new` | a new topic, with an optional poll |
| `/forum/topic/N` | the posts of a topic, 20 on each page, and a reply box; the poll, if the topic has one |
| `/forum/post/N` | a message, in its topic (for links to a message from other locations) |
| `/forum/user/NAME` | the profile and recent posts of a user |
| `/forum/online` | the users who are online, on all types of line |
| `/forum/search?q=` | messages that contain the words (minimum three letters) |
| `/forum/login`, `/forum/register` | log in, and sign up (when `new_users` permits it) |

When the [web terminal](HTTP-API#the-web-terminal) is on, a *Terminal* tab
goes to it.

## Topics

A topic is a message that is not a reply to a message in its area. The
topic also includes all replies to it and all replies to these replies, at
all levels. Replies on the terminals go into topics in the same way. If a
user deletes the first post of a topic, the next oldest post becomes the
first post.

The forum marks topics as read when a user reads them (for each user and
each topic). It marks an area as read in the same way as the new scan of
the BBS. *Mark this forum read* marks the area as read up to its newest
message. This also applies to the new scan on the terminals.

To post, a user must have the same permissions as for a post on the
terminals (the `post` requirement of the area). News-only areas do not
accept posts. Posts are the same as on the BBS:

- Posts are text. Quotes start with `> `.
- Links show as links.
- The forum wraps lines that are longer than a terminal line (79 columns)
  at spaces. It does not cut them. Thus, the terminals show the full
  paragraph.
- The forum adds the user's signature, as on the terminals.
- If an area permits aliases, a user can post under an alias. The forum
  does not show the real name.

On a post:

- **Quote** starts a reply that quotes the post (`name wrote:` and the
  lines with `> `). The quote shows in a quote box.
- **Like** is the BBS's vote on a message. Users cannot vote on their own
  messages.
- **Delete** is for the writer of the post and for the sysop.
- Users who can post in the area of a poll can answer the poll. A second
  answer replaces the first answer.

## Flags

If you set a country database (`[general] geoip`), each post keeps the
country of its poster. The BBS finds the country when the user posts:

- For users on the web, telnet or SSH, it uses the address.
- For modem callers, it uses the caller ID (`[general]
  phone_country`, refer to [Modem Callers](Modem-Callers#where-callers-are)).

The forum shows the country as a flag adjacent to the poster's name, in
all themes. The flag is an emoji. When you move the pointer over it, it
shows the name of the country. *Online Users* also shows a flag adjacent
to the line of each caller.

The BBS keeps only the country. It never keeps the address or the number.
These posts have no flag:

- Posts from before you set the database.
- Posts from other sources (FidoNet, Usenet, QWK).
- Posts from locations that the database does not know.

`forum_flags = no` shows no flags (the BBS keeps the data). While the
forum shows flags, the `geoip_credit` of the database shows at the bottom
of each page.

If the BBS is behind a reverse proxy, put the proxy in
`[http] trusted_proxies`. If you do not do this, all posters get the
country of the proxy.

## Themes

| Theme | What it looks like |
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
| `mybb16` | classic: MyBB 1.4 and 1.6's default look, 1.2's pages and colours (the mybb12 theme's) with each post a table of its own: a pale blue strip with the date and *Post: #1*, the author across the top with picture, title and stars and the counts at the right, quotes in plain boxes |
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

All themes are this BBS's own CSS and templates. Their appearance is
similar to the boards that they recall. They contain no code or images
from those boards.

`forum_theme` sets the theme of the forum. All users (also guests) can
select a different theme at the bottom of each page. The forum keeps the
selection in a cookie (`bbs_forum_theme`) for one year. A theme has its
own layout for the board, the topic lists, the topics and the profiles.
The other pages (posting, searching, signing in) show in the colors of
the theme.

Some themes use the templates of a different theme, with their own
stylesheets:

- The phpBB 3.2 and 3.3 themes use the templates of 3.1.
- vBulletin 6 uses the templates of 5.
- SMF 3.0 and 2.1 use the templates of Curve.
- XenForo 2.3 uses the templates of 2.
- MyBB 1.8 uses the templates of 1.6, and 1.6 uses the templates of 1.2.
- FluxBB 1.5 uses the templates of PunBB 1.2.
- YaBB 2.5 uses the templates of 2, and 2 uses the templates of 1.

## Logins

A login is the session token of the HTTP API (it lasts `session_hours`).
The forum keeps the token in an `HttpOnly`, `SameSite=Lax` cookie that is
only for `/forum` (`Secure` with TLS). The BBS limits and logs logins as
it does for all other logins. The forum does not accept an API token as a
cookie. When a user logs out, the BBS revokes the token. Each form that a
logged-in user sends contains a token from the user's session. The forum
refuses a form without this token.

### Sign-in codes

A user who is signed in on the BBS (telnet, SSH, a modem by modemmenu, a
3270) can get a sign-in code from *Your profile*, `C` (PF10). The code has
eight letters and digits, `ABCD-EFGH`. You can use it one time, for five
minutes. A new code cancels the previous code.

The Sign In page accepts the code alone, with no name or password. Use it
in these conditions:

- You do not want to type a password on the computer that you use.
- The user's password is the password of modemmenu.

At all other locations that ask for a password (the web terminal, the
image boards, the API, FTP), you can use the code one time in place of
the password, with the user's name.

The BBS keeps the codes in memory as their SHA-256 (a restart cancels
them). It never logs them. A wrong code is a failed login. The BBS limits
it and writes it in the hack log, as for all other failed logins. A code
for a locked account opens nothing.

The pages permit nothing from other locations (`Content-Security-Policy:
default-src 'self'`). They do not permit framing, and they keep no copies
(`no-store`). Requests count against the `rate` of the API for each
address.

**Refer to:** [HTTP API](HTTP-API), [Message areas](Message-Areas), [Writing messages](Writing-Messages)
