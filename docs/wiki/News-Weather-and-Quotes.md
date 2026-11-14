The BBS gives news feeds, the local weather of each caller, and stock
quotes, under one item of the main menu (**[N]**), as CompuServe,
Prodigy and AOL did. Each part is enabled when its section is in the
configuration.

```
News, weather, quotes
  [H] Headlines             [W] Your weather
  [Q] Stock quotes          [B] Back to the main menu
```

Sysop menus get to them with the actions `news` (the menu above),
`headlines`, `weather` and `quotes`.

## News feeds

```
[feeds]
contact = sysop@example.org
catalog = all
```

The BBS posts the items of a feed to a read-only message area of their
own (only the sysop posts). The BBS makes the area the first time that it
reads the feed, and lists it under its group. Thus, callers can read the
news in all locations where they read areas: the terminals and 3270s,
newsreaders (make the area a newsgroup with `bbs area newsgroup`), QWK
packets, the web forum, gopher and Gemini. **Headlines** lists the news
areas, each with its newest story.

An item becomes a short message: its headline, its summary as plain
text, a link to the full story, and its source and time. Feeds are
published for this purpose. The BBS does not get the stories.

```
Subj: AI stocks climb after trillions wiped from market values

Investors had been selling off tech shares in part due to concerns the
Democrat election victory would mean slower AI development.

The whole story:
https://www.bbc.co.uk/news/articles/c35ynnpzdq51o

-- BBC News, Fri 6 Nov 2026 21:06 UTC
```

The BBS reads RSS 0.9x to 2.0, RSS 1.0 (RDF), Atom and JSON Feed, in all
character sets. It reads each feed every `every` minutes. It adds a
small random variation, and reads less frequently after failures or
when the site asks with `Retry-After`. It asks only for changes
(`If-None-Match`, `If-Modified-Since`). The BBS posts an item one time
only. It makes the Message-ID from the feed and the id of the item, and
it remembers it after it purges the message. The first time that the BBS
reads a feed, it posts a maximum of `max_items` of the newest items.

The BBS sends `contact` in the User-Agent. The SEC refuses readers that
do not give a method to contact the person who runs them. The National
Weather Service asks for a contact.

### The catalogue

`bbs feeds catalog` lists the feeds that the BBS knows. `catalog =`
accepts `all`, groups or names:

| Group | Feeds |
|---|---|
| News | BBC World, NPR, AP News (through Google News: AP has no feeds of its own), The Guardian, Al Jazeera, CBC, Deutsche Welle, Global Voices |
| Business | BBC Business, NPR Business, SEC press releases, SEC 8-K filings (EDGAR) |
| Technology | BBC Technology, Ars Technica, The Register, Hacker News |
| Science | NASA, USGS earthquakes of magnitude 4.5 and more |
| Government | the Federal Register (significant documents), Congress.gov (most-viewed bills, bills presented to the President, House and Senate floor), the White House, National Weather Service news |

The feeds of the US government are in the public domain. The feeds of
the outlets belong to the outlets. The BBS shows what they publish in
their feeds, with the link. Reuters and AP stopped their feeds (2020,
2024).

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

- `title` is the sender of the messages (the title of the feed if you do
  not set it).
- `area` is the area (the name of the section if you do not set it).
- `keep_days` is the time that the BBS keeps messages when it makes the
  area (`bbs area keep` changes it later).

A section with the name of a catalogue feed changes that feed (for
example, `[feed nasa]` with `area = Space`). `enabled = no` removes it.

`bbs feeds read [NAME...]` reads the feeds now. `bbs feeds list` shows
the configured feeds.

The BBS offers a news area that is a newsgroup to news peers, as all
other newsgroups. Do not put feed newsgroups in the `groups` of a peer
unless you want to send the news to that peer.

## Your weather

```
[weather]
contact = sysop@example.org
international = yes
quake_radius = 300
quake_min = 2.5
alerts_at_login = yes
```

The first time, the caller gives the location: a US ZIP code, a town
(`Springfield, IL`, `Leeds, UK`, `Paris, France`, `Tokyo`), or a
latitude and longitude. The BBS looks up towns in the gazetteer of
Open-Meteo (GeoNames). If there are some towns with that name, the
caller selects one. ZIP codes are the tabulation areas of the US Census
Bureau, which are part of the BBS. The BBS keeps the location with the
account.

The weather screen has these parts, in this sequence:

- **Warnings, watches and advisories** that apply at that point now,
  from the National Weather Service (US only). The most serious come
  first, with the instructions.
- **The forecast**: the seven days of the National Weather Service in
  the US. In other countries, the forecast of Open-Meteo (the weather
  now, then seven days). Open-Meteo is free for non-commercial use and
  its data is CC BY 4.0. Thus, the screen gives its credit.
  `international = no` removes it.
- **Earthquakes** of the past week within `quake_radius` km, of
  `quake_min` or more (and great earthquakes, 6 and more, from four times
  that distance), from the USGS, worldwide. The screen marks the
  earthquakes with a PAGER alert above green or a tsunami flag.

Units are US (°F, mph) in the US and metric in other countries. The
caller can change them. With `alerts_at_login`, the BBS tells a caller
about Severe and Extreme warnings at the location of the caller at the
login.

The BBS keeps the data that it gets for some time (the points of the NWS
for a day, forecasts for half an hour, alerts for five minutes, the
earthquake list for ten minutes, shared by all callers). Thus, callers in
one town cause only one request.

## Stock quotes

```
[quotes]
file = /var/lib/bbs/quotes.json.gz
default = SPY DIA QQQ AAPL MSFT
```

The quotes come from IEX (the Investors Exchange). They are the trades
and quotes of IEX only, not of the full market, with the credit of IEX
on each screen. Each caller has a list of symbols (`default` until the
caller adds or removes one). For a selected symbol, the screen shows its
last trade, open, high, low, close, extended-hours price, bid and ask,
and IEX volume (the volume of the full market comes from the paid plan
of Alpaca).

### The day before's prices, free (HIST)

IEX publishes the full TOPS feed of each session on the next day, free
of charge. `bbs
quotes fetch` reads the file of the latest session into `file`. The file
is about 16 GB. Thus, the BBS reads it during the download, and keeps
only the quotes (some hundred KB). At 60 MB/s, it takes less than five
minutes. If a download stops, the BBS continues it from the point where
it stopped. Run it each morning as an event:

```
[event quotes]
command = /usr/local/bin/bbs -config /etc/bbs/bbs.conf quotes fetch
at = 06:30
timeout = 3600
```

To calculate the change from the day before, the BBS must have the
prices of the day before. The first time, get the previous session by
its date, then the latest:

```
bbs quotes fetch 20261105
bbs quotes fetch
bbs quotes show AAPL SPY
```

`bbs quotes load FILE` reads a TOPS capture that you have (pcap or
pcapng, with or without gzip). The BBS reads the new file when it
changes, with no restart.

The terms of IEX for HIST data ask for its credit ("Data provided for
free by IEX"). The screens show it.

### Live (TOPS)

```
[quotes]
listen = 233.215.21.4:10378
interface = eth1
delay = 15 minutes
```

A sysop who receives the TOPS feed of IEX (its real-time feed, or its
free delayed feed. The two must have a connection to IEX) can give its
UDP address. The BBS joins the multicast group and keeps the quotes when
they arrive. The BBS tells callers the `delay`. If there are no trades
yet today, the screens show the data from the file.

The feed is IEX-TP that carries TOPS 1.6 (the BBS also reads the
messages of DEEP: trades, quotes, official prices, trading status,
system events). The BBS ignores test securities. Odd lots count toward
volume, but not toward the last sale, high or low. Trades outside
regular hours are the extended-hours price.

### Live from Alpaca

```
[quotes]
alpaca_key = PK...
alpaca_secret = ...
alpaca_feed = iex
alpaca_symbols = 30
```

The market data of [Alpaca](https://alpaca.markets/data) does not need a
connection to IEX. An account (a paper-trading account is sufficient)
gives a key and secret. Its free plan (`alpaca_feed = iex`) has the
trades and quotes of IEX, live. A paid plan (`sip`) has those of all US
exchanges. `delayed_sip` gives them 15 minutes late.

When a caller looks at a symbol, the BBS gets it at that time (the
snapshot of Alpaca: the day until now, and the close of the day before
for the change). Then the BBS follows it on the stream of Alpaca. It
follows the default symbols and the symbols that callers looked at most
recently, up to `alpaca_symbols` (the free plan permits 30 at a time).
The BBS gets the followed symbols again every five minutes. It gets the
other symbols when a caller next looks at them, if they are one minute
old. The quotes start again with the first trade of each session. Thus,
nothing must run each morning. The BBS finds the session (before the
open, regular hours, after hours, closed) from the clock. If a weekday
has no trades by 9:35, the BBS thinks that it is a holiday.

`bbs quotes alpaca AAPL MSFT` tries the keys: the snapshots, then the
stream for some seconds. Keep the configuration file at 0640, because
the keys are in it.

The terms of Alpaca are for personal, non-commercial use. To make its
data available to others through your own application, they ask for 30
days of written notice first, and their written consent to republish it.
Tell them about the BBS before you enable this function. Use `listen`
or `alpaca_key`, not the two together.

**See also:** [Message areas](Message-Areas), [Newsgroups and NNTP](Newsgroups-and-NNTP), [Sysop tools](Sysop-Tools)
