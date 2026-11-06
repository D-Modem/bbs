Posts and mail are written in one of three editors, depending on the
terminal: the full-screen editor on ANSI terminals, the line editor on
other text terminals, and the editor screen on a 3270. Each one first asks
for `To` (Enter keeps what is offered) and `Subject`.

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

| Key | What it does |
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

Lines can be up to 79 characters (fewer on a narrower screen), and a
message can have as many lines as the line editor allows. Messages from
other nodes are held until the message is saved.

## The line editor

The line editor is used on other terminals, or by choice. Each line is
typed and sent with Enter, and words wrap at the edge. These commands work
on a line of their own:

| Command | What it does |
|---|---|
| `/S` | save |
| `/A` | give up |
| `/L` | list what is written |
| `/D` | delete the last line |
| `/C` | clear all |

A reply starts with the message being answered, quoted.

## Choosing

On an ANSI terminal, `[E]ditor full/line` in *Your profile* switches
between the two editors, and the choice is kept ([`editor`](Terminal-Identification#what-a-caller-sets)).

## On a 3270

A 3270 has an editor screen with `To`, `Subject` and a screen of lines to
type in. PF5 sends and PF3 gives up. A reply starts with its quote.

**See also:** [TN3270](TN3270), [Terminal identification](Terminal-Identification)
