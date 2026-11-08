package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// uids is a folder's numbers.
func uids(t *testing.T, s *Store, user int64, folder int) []int64 {
	t.Helper()
	es, err := s.FolderEntries(user, folder)
	if err != nil {
		t.Fatal(err)
	}
	var out []int64
	for _, e := range es {
		out = append(out, e.UID)
	}
	return out
}

// TestMailbox: one copy of a mail, the recipient's and the sender's
// entries, the folders they are in, and their numbers only growing.
func TestMailbox(t *testing.T) {
	s := open(t)
	alice, _ := s.CreateUser("Alice", "alicepass1", "", false)
	bob, _ := s.CreateUser("Bob", "bobpass123", "", false)
	id, err := s.SendMail(&Message{FromID: alice.ID, From: "Alice", ToID: bob.ID, To: "Bob", Subject: "Hi", Body: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if in, _ := s.Inbox(bob.ID); len(in) != 1 || in[0].ID != id || in[0].Read {
		t.Fatalf("bob's inbox: %+v", in)
	}
	if in, _ := s.Inbox(alice.ID); len(in) != 0 {
		t.Errorf("alice's inbox: %+v", in)
	}
	if sent, _ := s.Folder(alice.ID, FolderSent); len(sent) != 1 || sent[0].ID != id {
		t.Errorf("alice's sent: %+v", sent)
	}
	if u := uids(t, s, bob.ID, FolderUnread); len(u) != 1 {
		t.Errorf("unread: %v", u)
	}
	if _, err := s.Mail(alice.ID, id); !errors.Is(err, ErrNotFound) {
		t.Errorf("the sender reads it as received: %v", err)
	}

	// read: out of Unread; starred: into Starred, with a new number
	if err := s.MarkMailRead(bob.ID, id); err != nil {
		t.Fatal(err)
	}
	if u := uids(t, s, bob.ID, FolderUnread); len(u) != 0 {
		t.Errorf("unread after reading: %v", u)
	}
	inbox := uids(t, s, bob.ID, FolderInbox)
	if err := s.StarMail(bob.ID, id, true); err != nil {
		t.Fatal(err)
	}
	star := uids(t, s, bob.ID, FolderStarred)
	if len(star) != 1 || star[0] <= inbox[0] {
		t.Errorf("starred %v after inbox %v", star, inbox)
	}
	if got := uids(t, s, bob.ID, FolderInbox); len(got) != 1 || got[0] != inbox[0] {
		t.Errorf("inbox changed: %v", got)
	}

	// deleted: into the trash, out of everything else; the sender's
	// copy stays
	if err := s.DeleteMail(bob.ID, id); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.UnreadMail(bob.ID); n != 0 {
		t.Errorf("unread %d", n)
	}
	for f := 0; f < NumFolders; f++ {
		n := len(uids(t, s, bob.ID, f))
		if want := map[bool]int{true: 1, false: 0}[f == FolderTrash]; n != want {
			t.Errorf("bob's %s: %d", FolderNames[f], n)
		}
	}
	trash, _ := s.FolderEntries(bob.ID, FolderTrash)
	if err := s.RestoreBox(bob.ID, trash[0].Box); err != nil {
		t.Fatal(err)
	}
	if got := uids(t, s, bob.ID, FolderInbox); len(got) != 1 || got[0] <= star[0] {
		t.Errorf("restored: %v", got)
	}
	if err := s.TrashBox(bob.ID, trash[0].Box); err != nil {
		t.Fatal(err)
	}
	if err := s.RestoreBox(alice.ID, trash[0].Box); !errors.Is(err, ErrNotFound) {
		t.Errorf("another's entry: %v", err)
	}

	// the trash emptied: bob's entry goes, the mail stays for alice
	if n, err := s.PurgeTrash(time.Now().Add(time.Hour)); err != nil || n != 1 {
		t.Fatalf("purged %d %v", n, err)
	}
	if sent, _ := s.Folder(alice.ID, FolderSent); len(sent) != 1 {
		t.Fatalf("alice's sent after bob's purge: %+v", sent)
	}
	if err := s.PurgeBox(alice.ID, sent0(t, s, alice.ID)); err != nil {
		t.Fatal(err)
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM mail`).Scan(&n)
	if n != 0 {
		t.Errorf("mail left: %d", n)
	}

	// appended, found by its Message-ID; the queue keeps a mail no one
	// has
	box, err := s.AppendMail(alice.ID, RoleSent, &Message{FromID: alice.ID, From: "Alice", To: "x@example.com",
		Subject: "Out", Body: "b", Raw: []byte("Subject: Out\r\n\r\nb\r\n"), MessageID: "<1@x>"}, true, false, false, "$Forwarded")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.SentByMessageID(alice.ID, "<1@x>"); err != nil || got != box {
		t.Errorf("by Message-ID: %d %v", got, err)
	}
	m, e, err := s.BoxMail(alice.ID, box)
	if err != nil || string(m.Raw) != "Subject: Out\r\n\r\nb\r\n" || e.Flags != "$Forwarded" || e.Role != RoleSent {
		t.Fatalf("box mail: %+v %+v %v", m, e, err)
	}
	if _, _, err := s.BoxMail(bob.ID, box); !errors.Is(err, ErrNotFound) {
		t.Errorf("another's: %v", err)
	}
	mid, err := s.DeliverMail(&Message{FromID: alice.ID, From: "Alice", To: "x@example.com", Subject: "Q", Body: "b",
		Raw: []byte("raw")}, Delivery{Sent: true, Queue: []*Outbound{{UserID: alice.ID, Sender: "alice@here", Rcpt: "x@example.com"}}})
	if err != nil {
		t.Fatal(err)
	}
	q, _ := s.MailQueue(time.Now(), 0)
	if len(q) != 1 || q[0].MailID != mid || q[0].Rcpt != "x@example.com" {
		t.Fatalf("queue: %+v", q)
	}
	if err := s.QueueRetry(q[0].ID, time.Now().Add(time.Hour), "421 later"); err != nil {
		t.Fatal(err)
	}
	if q, _ := s.MailQueue(time.Now(), 0); len(q) != 0 {
		t.Errorf("not due yet: %+v", q)
	}
	sentBox, _ := s.FolderEntries(alice.ID, FolderSent)
	for _, e := range sentBox {
		s.PurgeBox(alice.ID, e.Box)
	}
	if raw, err := s.MailRaw(mid); err != nil || string(raw) != "raw" {
		t.Errorf("queued mail gone with its entry: %q %v", raw, err)
	}
	if err := s.QueueDone(q[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MailRaw(mid); !errors.Is(err, ErrNotFound) {
		t.Errorf("sent and no one's: %v", err)
	}
}

func sent0(t *testing.T, s *Store, user int64) int64 {
	es, _ := s.FolderEntries(user, FolderSent)
	if len(es) == 0 {
		t.Fatal("nothing sent")
	}
	return es[0].Box
}

// TestMailboxMigration: an older database's mail is put in its
// recipients' inboxes, read or not as it was.
func TestMailboxMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	d, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE, password TEXT NOT NULL, created INTEGER NOT NULL)`,
		`INSERT INTO users (name, password, created) VALUES ('Bob', 'x', 1)`,
		`CREATE TABLE mail (id INTEGER PRIMARY KEY, from_id INTEGER NOT NULL, from_name TEXT NOT NULL,
			to_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE, to_name TEXT NOT NULL, subject TEXT NOT NULL,
			body TEXT NOT NULL, reply_to INTEGER NOT NULL DEFAULT 0, created INTEGER NOT NULL, "read" INTEGER NOT NULL DEFAULT 0)`,
		`INSERT INTO mail (from_id, from_name, to_id, to_name, subject, body, created, "read") VALUES (0, 'Sysop', 1, 'Bob', 'Old', 'b', 5, 1)`,
		`INSERT INTO mail (from_id, from_name, to_id, to_name, subject, body, created, "read") VALUES (0, 'Sysop', 1, 'Bob', 'New', 'b', 6, 0)`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	d.Close()
	s, err := Open("sqlite:" + path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	in, err := s.Inbox(1)
	if err != nil || len(in) != 2 || !in[0].Read || in[1].Read || in[1].Subject != "New" {
		t.Fatalf("inbox: %+v %v", in, err)
	}
	if u := uids(t, s, 1, FolderUnread); len(u) != 1 {
		t.Errorf("unread: %v", u)
	}
	if u := uids(t, s, 1, FolderAll); len(u) != 2 {
		t.Errorf("all: %v", u)
	}
}
