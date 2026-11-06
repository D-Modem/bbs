package confedit

import (
	"fmt"
	"strings"
)

// Reference is the configuration reference page (the wiki's and the
// site's Configuration-Reference.md): every kind of section and every key
// the catalog knows, with its help, default, examples and whether it
// takes effect at a reload.  go generate in this package writes it.
func Reference(c *Catalog) []byte {
	var b strings.Builder
	b.WriteString(`<!-- Made by go generate ./internal/confedit from bbs.conf.sample: edit the sample, not this page. -->

# Configuration reference

Every section and key of bbs.conf, from the comments in
[bbs.conf.sample](https://github.com/D-Modem/bbs/blob/HEAD/bbs.conf.sample)
(the same help the [configuration screens](Configuration#editing-it-the-configuration-screens)
show).  **Live** keys take effect when the file is read again
(` + "`systemctl reload bbs`" + `, or saving in the configuration screens); the
rest when the BBS starts again.  [Configuration](Configuration) says how
the file is laid out.

`)
	b.WriteString("| section | what it is |\n|---|---|\n")
	for _, k := range c.Kinds {
		fmt.Fprintf(&b, "| [`%s`](#%s) | %s |\n", kindHeader(k), anchor(k), cell(prose(firstSentence(k.Help))))
	}
	for _, k := range c.Kinds {
		fmt.Fprintf(&b, "\n## `%s`\n\n", kindHeader(k))
		if k.Help != "" {
			b.WriteString(prose(k.Help) + "\n\n")
		}
		if k.Named {
			fmt.Fprintf(&b, "There may be any number of these, each with its own NAME.\n\n")
		}
		if len(k.Keys) > 0 {
			b.WriteString("| key | default | live | what it does |\n|---|---|---|---|\n")
			for _, x := range k.Keys {
				help := prose(x.Help)
				if help == "" {
					help = "See the section above."
				}
				if len(x.Examples) > 0 && !(len(x.Examples) == 1 && x.Examples[0] == x.Default) {
					var ex []string
					for _, e := range x.Examples {
						ex = append(ex, code(e))
					}
					help += " Examples: " + strings.Join(ex, ", ") + "."
				}
				def := ""
				if x.Default != "" {
					def = code(x.Default)
				}
				live := ""
				if Live(k.Name, x.Name) {
					live = "yes"
				}
				fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n", x.Name, def, live, cell(help))
			}
			b.WriteString("\n")
		}
		if k.Free != "" {
			fmt.Fprintf(&b, "Any other key is %s.\n\n", cell(k.Free))
		}
	}
	b.WriteString("**See also:** [Configuration](Configuration)\n")
	return []byte(b.String())
}

// kindHeader is the kind's section header as the file has it.
func kindHeader(k *Kind) string {
	if k.Named {
		return "[" + k.Name + " NAME]"
	}
	return "[" + k.Name + "]"
}

// anchor is the id GitHub's wiki and the site give the kind's heading:
// "[listen NAME]" is listen-name.
func anchor(k *Kind) string {
	return strings.ToLower(strings.ReplaceAll(strings.Trim(kindHeader(k), "[]"), " ", "-"))
}

// firstSentence is the help's first sentence (all of it if it has one only).
func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	for i := 0; i+1 < len(s); i++ {
		if s[i] == '.' && s[i+1] == ' ' {
			return s[:i+1]
		}
	}
	return s
}

// prose is help as a paragraph, ended by a stop.
func prose(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	if !strings.HasSuffix(s, ".") && !strings.HasSuffix(s, ")") && !strings.HasSuffix(s, ":") {
		s += "."
	}
	return s
}

// cell is s made safe in a Markdown table's cell.
func cell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	return strings.Join(strings.Fields(s), " ")
}

// code is s as inline code, with a backquote in it fenced by two.
func code(s string) string {
	if strings.Contains(s, "`") {
		return "`` " + s + " ``"
	}
	return "`" + s + "`"
}
