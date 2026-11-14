Sysops can add their own screens and functions to the BBS in JavaScript.
A script draws the same types of screen as the other parts of the BBS
(menus, tables, forms, panels, messages and the editor). Thus, it
operates on all terminals: a text terminal in ANSI, ASCII or PETSCII, a
RIP terminal (where menus become buttons), and a 3270. A script operates
as its caller, with the access of the caller.

```
; bbs.conf
[general]
scripts_dir = /etc/bbs/scripts

[menu main]
o = script:oneliners One-liners
p = script:poll Today's poll
```

A menu item `script:NAME` runs `NAME.js` from `scripts_dir` (names are
lower-case letters, digits, `_` and `-`). A `logon.js` in that directory
runs after each login, before the menus. `bbs check` compiles all
scripts. It refuses a configuration if a script does not compile, or if
a menu names a script that does not exist. If you change a script while
the BBS runs, the BBS compiles it again at its next run.
`scripts.sample` has two scripts that you can use as a start: one-liners
and a poll.

## What a script is

Scripts run in [goja](https://github.com/dop251/goja), in strict mode.
goja supports ECMAScript 5.1 and much of ECMAScript 6 (`let`, arrow
functions, classes, `Map`, template strings). A script cannot get to
files, the network, processes or timers. It has only the objects that
the text below gives.

A script can compute for a maximum of two seconds between one screen and
the next. The time that the caller is at a screen does not count. After
two seconds, the BBS stops the script and tells the caller. If a script
fails, the BBS logs it and tells the caller (it also tells a sysop the
reason). If the caller hangs up or is idle, the script ends, also if it
catches the error. `exit()` ends a script before its end.

```js
// a guestbook in eight lines
var lines = data.get("lines") || [];
var k = ui.menu({title: "Guestbook", intro: lines.slice(-10),
	items: [{key: "s", label: "Sign it"}, {key: "q", label: "Quit"}]});
if (k === "S") {
	var v = ui.form({title: "Sign", fields: [{name: "line", label: "Your words", width: 60}]});
	if (v && v.line) data.set("lines", lines.concat(user.name + ": " + v.line).slice(-100));
}
```

## ui: the screens

| Function | Returns | Notes |
|---|---|---|
| `ui.menu({title, intro, items: [{key, label}], prompt, screen})` | the selected key (upper case), `""`, or `ui.BACK` (PF3 of a 3270) | `intro`: lines before the items. `screen`: a display file in place of the items |
| `ui.table({title, note, columns: [{title, width, right, wide}], rows, empty, pick, prompt, actions, more, less})` | `{action, row, num, text}` | `rows`: arrays of cells, or `{num, cells, mark, hot}`. `pick` lets the caller select rows (`action` is `ui.PICK`, `row` is the index, -1 for a number that is not shown, `num` is the typed value). `wide` columns show only on screens of 60 columns or more. `more`/`less` tell that there are pages after and before (`ui.MORE`, `ui.LESS`) |
| `ui.message({where, from, to, subject, date, body, actions, default, quit})` | the selected action | |
| `ui.form({title, intro, fields: [{name, label, prompt, width, secret, value}], submit})` | the values by name, or `null` if the caller stopped | one question for each field on a text terminal, one screen on a 3270 |
| `ui.panel({title, lines, actions})` | the selected action, `ui.BACK`, or `""` | |
| `ui.edit({title, to, subject, body, quote})` | `{to, subject, body}`, or `null` | the full-screen editor, the line editor or the editor of the 3270. `quote` is for the quote window |
| `ui.confirm(question, default)` | true or false | |
| `ui.say(text, tone)` | | shows a line now (on a 3270: on the message line of the next screen) |
| `ui.show(title, lines)` | | shows lines until the caller reads them |
| `ui.screen(name)` | true if there was a display file | a display file of `screens_dir` |
| `ui.kind`, `ui.pageSize`, `ui.narrow` | | `"text"` or `"3270"`. The number of rows that a table shows. True if under 60 columns |

Lines are strings (`\n` starts a new line) or objects
`{label, text, tone, bar}`. An object gives a label and its value, a tone
(`plain`, `dim`, `bright`, `good`, `bad` or `warn`), and a bar from 0 to
1. Actions are `{id, label, key, pf, short}`:

- `key` is the key on a text terminal.
- `pf` is the PF key on a 3270. An action without `pf` does not show on
  a 3270.
- `short` is its name on the key line of the 3270.

On a text terminal, `Q` (or Enter at an empty prompt) goes back. On a
3270, PF3 goes back.

## The BBS

| Object | What it gives |
|---|---|
| `bbs.name`, `bbs.sysop`, `bbs.location`, `bbs.log(text)` | the system. A line in its log |
| `user.id`, `.name`, `.location`, `.level`, `.flags`, `.sysop`, `.calls`, `.posts`, `.since`, `.lastCall`, `user.has(require)` | the caller. `has` takes an [access requirement](Access-Levels), for example `"20 A"` (level 20 or more, and flag A) |
| `node.number`, `node.doing(text)`, `node.who()`, `node.message(n, text)` | the node of the caller. The activity of the caller that *Who's online* shows. The nodes. A line to a node |
| `msg.areas()`, `msg.list(area, after, limit)`, `msg.get(id)` | the message areas that the caller reads. Their messages (up to 200 at a time, after an id). One message (`null` if the caller cannot read it) |
| `msg.post(area, {to, subject, body, replyTo})`, `msg.mail({to, subject, body})` | post (gives the id of the message) and mail, as the caller. Refusals are thrown as errors |
| `data.get(name)`, `data.set(name, value)`, `data.remove(name)`, `data.keys(prefix)` | the data that the script keeps for all users: a JSON value of up to 64 KB under a name of up to 100 characters, with a maximum of 1000 names |
| `data.user.get` ... | the same, for the caller only (100 names) |
| `console.log(...)` | the log of the BBS |

The BBS keeps the data of a script in the database (`script_data`) under
the name of the script. Thus, if you change the name of a script, it
starts with no data. The BBS deletes the data of a user with the account.

**See also:** [Customization](Customization), [Development](Development#screens)
