# Web forum

The HTTP API's server also serves the message areas as a web forum, at
`/forum/` (and `/`, which goes there). It is laid out like the bulletin
boards of their day: categories of forums, topics, and posts with their
authors beside them, in one of two [themes](#themes). The look is the
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
```

| setting | default | |
|---|---|---|
| `forum` | `yes` | the forum at `/forum/` (with `no`, `/` goes to the web terminal again) |
| `forum_guests` | `yes` | those not logged in may read the areas anyone may read (areas with a `read` requirement never) |
| `forum_theme` | `ipb3` | the theme callers get until they choose one ([below](#themes)) |

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

## Themes

| theme | |
|---|---|
| `ipb3` | the default: a board of the early 2010s in the manner of Invision Power Board 3: a dark user bar, a navy header with tabs, rounded blue category bars, posts with their author's column on the left |
| `ipb2` | classic: a board of the mid-2000s in the manner of Invision Power Board 2: a white page, boxes outlined in navy, blue gradient title bars, pale blue rows, small Verdana; *Logged in as* and *Welcome Guest* bars, *Last Post Info*, *Fast Reply*, big blue buttons |

Both are this BBS's own CSS and templates, drawn to look like the boards
they recall; no code or images are taken from them. `forum_theme` is the
forum's; anyone (guests too) may choose another at the foot of every page,
kept in a cookie (`bbs_forum_theme`) for a year. A theme lays out the
board, topic lists, topics and profiles its own way, and shows the rest
(posting, searching, signing in) in its colors.

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
