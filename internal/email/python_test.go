package email

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
	"testing"

	"github.com/D-Modem/bbs/internal/store"
)

// Python's smtplib, poplib and imaplib, mail programs of someone else's
// making, send, fetch, file and search mail.
func TestPythonClients(t *testing.T) {
	if exec.Command("python3", "-c", "import smtplib, poplib, imaplib").Run() != nil {
		t.Skip("no python3")
	}
	s := newSite(t, "a.test", &fakeDNS{}, []string{"sysop", "alice", "bob"}, Submission, "pop3", "imap")
	hp := func(kind string) (string, string) {
		h, p, _ := net.SplitHostPort(s.addrs[kind])
		return h, p
	}
	sh, sp := hp(Submission)
	ph, pp := hp("pop3")
	ih, ip := hp("imap")
	script := fmt.Sprintf(`
import smtplib, poplib, imaplib, email
from email.message import EmailMessage

m = EmailMessage()
m['From'] = 'alice <alice@a.test>'
m['To'] = 'bob@a.test'
m['Subject'] = 'From Python'
m.set_content('Hello Bob, from smtplib.')
with smtplib.SMTP(%[1]q, %[2]s) as c:
    c.login('alice', 'password1')
    c.send_message(m)

p = poplib.POP3(%[3]q, %[4]s)
p.user('bob')
p.pass_('password1')
count, size = p.stat()
assert count == 1, count
resp, lines, octets = p.retr(1)
msg = email.message_from_bytes(b'\r\n'.join(lines))
assert msg['Subject'] == 'From Python', msg['Subject']
assert 'from smtplib' in msg.get_payload(), msg.get_payload()
resp, uids, _ = p.uidl()
assert len(uids) == 1, uids
p.quit()

i = imaplib.IMAP4(%[5]q, %[6]s)
i.login('bob', 'password1')
typ, folders = i.list()
names = b'\n'.join(folders).decode()
for want in ('INBOX', 'Sent', 'Drafts', 'Starred', 'Unread', 'All Mail', 'Trash', '\\Flagged', '\\Trash'):
    assert want in names, (want, names)
typ, data = i.select('INBOX')
assert data[0] == b'1', data
typ, data = i.uid('SEARCH', None, 'SUBJECT', '"From Python"')
uid = data[0].split()[0]
typ, data = i.uid('FETCH', uid, '(FLAGS BODY.PEEK[HEADER.FIELDS (SUBJECT)])')
assert b'From Python' in data[0][1], data
assert b'\\Seen' in data[0][0], data
typ, _ = i.uid('STORE', uid, '+FLAGS', '(\\Flagged)')
assert typ == 'OK', typ
typ, data = i.select('Starred')
assert data[0] == b'1', data
i.select('INBOX')
typ, _ = i.uid('MOVE', uid, 'Trash')
assert typ == 'OK', typ
typ, data = i.select('INBOX')
assert data[0] == b'0', data
typ, data = i.select('Trash')
assert data[0] == b'1', data
typ, data = i.append('Drafts', '', imaplib.Time2Internaldate(0), b'Subject: Draft\r\n\r\nNot yet.\r\n')
assert typ == 'OK', (typ, data)
i.logout()
print('ok')
`, sh, sp, ph, pp, ih, ip)
	out, err := exec.Command("python3", "-W", "ignore", "-c", script).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "ok") {
		t.Fatalf("python: %v\n%s", err, out)
	}
	if l := inbox(t, s, "bob", store.FolderTrash); len(l) != 1 || !l[0].Starred {
		t.Errorf("bob's trash: %+v", l)
	}
	if l := inbox(t, s, "bob", store.FolderDrafts); len(l) != 1 || l[0].Subject != "Draft" {
		t.Errorf("bob's drafts: %+v", l)
	}
	if l := inbox(t, s, "alice", store.FolderSent); len(l) != 1 || l[0].Subject != "From Python" {
		t.Errorf("alice's sent: %+v", l)
	}
}
