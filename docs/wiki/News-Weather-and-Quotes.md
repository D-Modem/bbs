News feeds, each caller's own weather, and stock quotes, under one item
of the main menu (**[N]**), as CompuServe, Prodigy and AOL had them. Each
part is on when its section is in the configuration.

```
News, weather, quotes
  [H] Headlines             [W] Your weather
  [Q] Stock quotes          [B] Back to the main menu
```

Sysop menus reach them with the actions `news` (the menu above),
`headlines`, `weather` and `quotes`.

## News feeds

```
[feeds]
contact = sysop@example.org
catalog = all
```

A feed's items are posted to a message area of their own, read-only (only
the sysop posts), made the first time the feed is read and listed under
its group. So the news is read wherever areas are: the terminals and
3270s, newsreaders (make the area a newsgroup with `bbs area newsgroup`),
QWK packets, the web forum, gopher and Gemini. **Headlines** lists the
news areas, each with its newest story.

An item becomes a short message: its headline, its summary as plain text,
a link to the whole story, and where and when it is from. That is what
feeds are published for; the BBS does not fetch the stories themselves.

```
Subj: AI stocks climb after trillions wiped from market values

Investors had been selling off tech shares in part due to concerns the
Democrat election victory would mean slower AI development.

The whole story:
https://www.bbc.co.uk/news/articles/c35ynnpzdq51o

-- BBC News, Fri 6 Nov 2026 21:06 UTC
```

RSS 0.9x to 2.0, RSS 1.0 (RDF), Atom and JSON Feed are read, in any
character set. Each feed is read every `every` minutes (with a little
jitter, and less often after failures or when the site asks with
`Retry-After`), and asked only for what changed (`If-None-Match`,
`If-Modified-Since`). An item is posted once: its Message-ID is made from
the feed and the item's id, and remembered after the message is purged.
The first reading of a feed posts at most `max_items` of its newest.

`contact` is sent in the User-Agent. The SEC refuses readers without a way
to reach the person running them, and the National Weather Service asks
for one.

### The catalogue

`bbs feeds catalog` lists the feeds the BBS knows. `catalog =` takes `all`,
groups or names:

| Group | Feeds |
|---|---|
| News | BBC World, NPR, AP News (by way of Google News: AP has no feeds of its own), The Guardian, Al Jazeera, CBC, Deutsche Welle, Global Voices |
| Business | BBC Business, NPR Business, SEC press releases, SEC 8-K filings (EDGAR) |
| Technology | BBC Technology, Ars Technica, The Register, Hacker News |
| Science | NASA, USGS earthquakes of magnitude 4.5 and more |
| Government | the Federal Register (significant documents), Congress.gov (most-viewed bills, bills presented to the President, House and Senate floor), the White House, National Weather Service news |

The US government's feeds are in the public domain. The outlets' are
theirs: the BBS shows what they publish in their feeds, with the link.
Reuters and AP stopped publishing feeds (2020, 2024).

### Feeds of your own

```
[feed lobsters]
url = https://lobste.rs/rss
title = Lobsters
area = Lobsters
group = Technology
every = 60
keep_days = 14
```

`title` is who the messages are from (the feed's own title if not said),
`area` the area (the section's name if not said), `keep_days` how long
messages are kept when the area is made (`bbs area keep` changes it after).
A section named as a catalogue feed changes it (`[feed nasa]` with
`area = Space`), and `enabled = no` leaves it out.

`bbs feeds read [NAME...]` reads the feeds now, `bbs feeds list` shows
what is configured.

A news area that is a newsgroup is offered to news peers like any other:
leave feed newsgroups out of a peer's `groups` unless you mean to pass the
news on.

## Your weather

```
[weather]
contact = sysop@example.org
international = yes
quake_radius = 300
quake_min = 2.5
alerts_at_login = yes
```

The first time, the caller says where they are: a US ZIP code, a town
(`Springfield, IL`, `Leeds, UK`, `Paris, France`, `Tokyo`), or a latitude
and longitude. Towns are looked up in Open-Meteo's gazetteer (GeoNames);
when there are several of that name, the caller chooses. ZIP codes are
the US Census Bureau's tabulation areas, built into the BBS. The place is
kept with the account.

The weather screen has, in order:

- **Warnings, watches and advisories** in force at that point, from the
  National Weather Service (US only), the most serious first, with what
  to do.
- **The forecast**: the National Weather Service's seven days in the US;
  elsewhere Open-Meteo's (the weather now, then seven days). Open-Meteo
  is free for non-commercial use and its data is CC BY 4.0, so the screen
  credits it; `international = no` leaves it out.
- **Earthquakes** of the past week within `quake_radius` km of
  `quake_min` or more (and great ones, 6 and more, from four times as
  far), from the USGS, worldwide. Those with a PAGER alert above green or
  a tsunami flag are marked.

Units are US (°F, mph) in the US and metric elsewhere; the caller can
change them. With `alerts_at_login`, a caller is told of Severe and
Extreme warnings where they are as they log in.

What is fetched is kept a while (the NWS's points a day, forecasts half an
hour, alerts five minutes, the earthquake list ten minutes, shared by all),
so callers in one town cost one request.

## Stock quotes

```
[quotes]
file = /var/lib/bbs/quotes.json.gz
default = SPY DIA QQQ AAPL MSFT
```

The quotes are IEX's (the Investors Exchange): its own trades and quotes,
not the whole market's, with IEX's credit on each screen. Every caller has
a list of symbols (`default` until they add or remove one); a symbol
chosen shows its last trade, open, high, low, close, extended-hours price,
bid and ask, and IEX volume (all the market's, from Alpaca's paid plan).

### The day before's prices, free (HIST)

IEX publishes each session's whole TOPS feed the next day, free. `bbs
quotes fetch` reads the latest session's file into `file`. The file is
some 16 GB, so it is read as it downloads and nothing of it is kept but
the quotes (a few hundred KB); at 60 MB/s it takes under five minutes. A
download cut off is taken up where it stopped. Run it each morning as an
event:

```
[event quotes]
command = /usr/local/bin/bbs -config /etc/bbs/bbs.conf quotes fetch
at = 06:30
timeout = 3600
```

The change from the day before needs the day before's prices: the first
time, fetch the session before by its date, then the latest:

```
bbs quotes fetch 20261105
bbs quotes fetch
bbs quotes show AAPL SPY
```

`bbs quotes load FILE` reads a TOPS capture you have (pcap or pcapng,
gzipped or not), and the BBS reads the new file when it changes, with no
restart.

IEX's terms for HIST data ask for its credit ("Data provided for free by
IEX"), which the screens show.

### Live (TOPS)

```
[quotes]
listen = 233.215.21.4:10378
interface = eth1
delay = 15 minutes
```

A sysop who receives IEX's TOPS feed (its real-time feed, or its free
delayed one; both need a connection to IEX) can give its UDP address. The
BBS joins the multicast group and keeps the quotes as they come; `delay`
is told to callers. Without trades yet today, the screens show the file's.

The feed is IEX-TP carrying TOPS 1.6 (DEEP's messages are read too:
trades, quotes, official prices, trading status, system events). Test
securities are left out; odd lots count toward volume but not the last
sale, high or low; trades outside regular hours are the extended-hours
price.

### Live from Alpaca

```
[quotes]
alpaca_key = PK...
alpaca_secret = ...
alpaca_feed = iex
alpaca_symbols = 30
```

[Alpaca](https://alpaca.markets/data)'s market data needs no connection
to IEX: an account (a paper-trading one will do) gives a key and secret.
Its free plan (`alpaca_feed = iex`) has IEX's trades and quotes, live; a
paid one (`sip`) every US exchange's, and `delayed_sip` them 15 minutes
late.

A symbol a caller looks at is fetched then (Alpaca's snapshot: the day so
far, and the day before's close for the change), and followed on Alpaca's
stream: the default symbols and those looked at most recently, up to
`alpaca_symbols` (the free plan allows 30 at a time). What is followed is
fetched again every five minutes; the rest when it is next looked at, if
a minute old. The quotes begin again with each session's first trade, so
nothing need run each morning. The session (before the open, regular
hours, after hours, closed) is by the clock: a weekday with no trades by
9:35 is taken for a holiday.

`bbs quotes alpaca AAPL MSFT` tries the keys: the snapshots, then the
stream for a few seconds. Keep the configuration file 0640, as the keys
are in it.

Alpaca's terms are for personal, non-commercial use: to make its data
available to others through your own application, they ask for 30 days'
written notice first, and its written consent to republish it. Tell them
of the BBS before you turn this on. `listen` and `alpaca_key` are one or
the other.

**See also:** [Message areas](Message-Areas), [Newsgroups and NNTP](Newsgroups-and-NNTP), [Sysop tools](Sysop-Tools)
