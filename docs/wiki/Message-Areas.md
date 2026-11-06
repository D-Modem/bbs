This page describes what callers can do in the message areas, on any
terminal. The letters are the keys on a text terminal, and the PF keys are
a 3270's.

## The areas

```
Message areas
    Area                 Description                           Msgs   New
  1 General              Talk                                    42 *   3
  2 Other                More talk                               17

Area number, [N]ew scan, [T]o you, [F]ind, [S]can setup, [Q]uit:
```

| Action | Key | PF | What it does |
|---|---|---|---|
| an area | its number | `S` by it | its messages, a page at a time |
| new scan | `N` | PF5 | reads the new messages of each area in the scan, area after area |
| to you | `T` | PF4 | the newest 100 messages addressed to you (in any case), in the areas you read; `*` those you have not read |
| find | `F` | PF10 | messages whose subject, text or author has the words in them (in any case, two characters or more), newest first, up to 100 |
| scan setup | `S` | PF11 | which areas the new scan goes through |
| add area | `+` | PF6 | the sysop only |

## Scan setup

Every area is in the new scan until the caller takes it out. Typing an
area's number (or `S` beside it on a 3270) switches it in or out, `A`
(PF5) puts every area in, and `X` (PF6) takes every area out. Areas marked
`*` are in the scan. The choice is kept with the account.

If the sysop has put areas in groups, they are listed by group (areas in
no group first), with a Group column.

## In an area

An area lists its messages a page at a time (Enter for more on a text
terminal, PF7/PF8 on a 3270), with `*` marking those not yet read. Typing
a message's number reads it, even if it is not on the page. `R` (PF5)
reads from the first new message, `P` (PF6) posts, and `L` (PF9) posts a
poll: a question and 2 to 10 answers, separated by `|`.

## Reading

| Action | Key | PF |
|---|---|---|
| next | `N`, Enter, space | PF11, Enter |
| previous | `P` | PF10 |
| reply in the area | `R` | PF4 |
| reply by mail | `M` | PF5 |
| delete (yours, or any for the sysop) | `D` | PF9 |
| vote it up or down (or take your vote back) | `V` | PF1 |
| the thread: every message from the one it began with, replies under what they answer | `T` | PF6 |
| the message this answers | `O` | PF2 |
| answer a poll (again: your answer changes) | `A` | PF12 |
| back | `Q` | PF3 |

A message's votes appear beside its number (`+3 -1`). A poll's answers
appear under its text with each answer's share, and your answer is marked
`*`.

## Aliases and signatures

In the profile, `I` sets an alias and `G` sets a signature (up to three
lines). In an area where the sysop allows aliases, posting asks whether to
post under the alias. The message is then from the alias (in newsgroups
and echoes too), although the sysop can still see who wrote it. An alias
is checked like a new user's name, and cannot be anyone else's name or
alias. The signature goes under every post and mail written on the BBS.

Reading in an area marks the area read up to that point. Reading messages
found by *To you* or *Find* does not, so the new scan still shows those
messages as new.

For how to write messages, see [Writing messages](Writing-Messages).

**See also:** [Administration](Administration#message-areas), [TN3270](TN3270)
