Posts and mail are written in one of three editors, by terminal: the
full-screen editor on ANSI terminals, the line editor on the rest, and
the 3270's editor screen.  First come `To` (Enter keeps what is offered)
and `Subject`.

## The full-screen editor

On an ANSI terminal (not a VT52 or a Commodore) of 40 columns and 12
rows or more:

```
To: alice  Subject: Re: The meeting
-------------------------------------------------------------------------------
alice wrote:
> Thursday at eight?
Thursday suits me.
_



^Z save  ^X give up  ^Y delete line  ^L redraw  ^Q quote                    4/5
```

| key | |
|---|---|
| typing | goes in at the cursor; a word that passes the edge wraps to the next line |
| cursor keys, Home, End, PgUp, PgDn | move about |
| Enter | a new line (the line breaks at the cursor) |
| Backspace, Delete (or ^D) | take away a character, or join two lines |
| ^Y | delete the line |
| ^Q | the quote window (a reply): Up and Down choose a line of the message replied to, Enter takes it into yours, ^Q or ESC closes the window |
| ^Z | save (`/S` alone on a line and Enter does too) |
| ^X (or ^A) | give up, after a Y |
| ^L | draw the screen again |

Lines are at most 79 characters (less on a narrower screen), and a
message at most as many lines as the line editor takes.  Messages from
other nodes wait until the message is saved.

## The line editor

On other terminals, or by choice: each line is typed and sent with
Enter, a word wrapping at the edge.  On a line of its own:

| | |
|---|---|
| `/S` | save |
| `/A` | give up |
| `/L` | list what is written |
| `/D` | delete the last line |
| `/C` | clear all |

A reply begins with the message replied to, quoted.

## Choosing

`[E]ditor full/line` in *Your profile* switches between them on an ANSI
terminal, and the choice is kept ([`editor`](Terminal-Identification#what-a-caller-sets)).

## On a 3270

The editor screen: `To`, `Subject`, and a screen of lines to type in;
PF5 sends, PF3 gives up.  A reply begins with its quote.

**See also:** [TN3270](TN3270), [Terminal identification](Terminal-Identification)
