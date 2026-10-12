package store

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// open is a fresh store: SQLite here, or the engine of BBS_TEST_DATABASE.
func open(t *testing.T) *Store {
	t.Helper()
	BcryptCost = 4
	s, err := Open(filepath.Join(t.TempDir(), "bbs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// TestEngine: what the engines do differently, done alike.
func TestEngine(t *testing.T) {
	s := open(t)
	t.Logf("engine %s", s.Engine())

	// names: any case finds one; a name in another case is taken; accents
	// are not case
	u, err := s.CreateUser("Alice", "alicepass1", "Here", false)
	if err != nil || u.ID == 0 || u.Name != "Alice" || u.Level != 20 || u.Sysop {
		t.Fatalf("create: %+v %v", u, err)
	}
	if got, err := s.UserByName("aLiCe"); err != nil || got.ID != u.ID {
		t.Fatalf("by name in another case: %v %v", got, err)
	}
	if _, err := s.CreateUser("ALICE", "x", "", false); !errors.Is(err, ErrExists) {
		t.Fatalf("ALICE again: %v", err)
	}
	r1, err1 := s.CreateUser("Rene", "renepass1", "", false)
	r2, err2 := s.CreateUser("René", "renepass1", "", false)
	if err1 != nil || err2 != nil || r1.ID == r2.ID {
		t.Fatalf("Rene and René: %v %v", err1, err2)
	}
	if got, _ := s.UserByName("rené"); got == nil || got.ID != r2.ID {
		// SQLite's NOCASE folds ASCII only: René is found as written
		if s.Engine() != SQLite {
			t.Errorf("rené: %v", got)
		}
	}
	sysop, _ := s.CreateUser("sysop", "sysoppass1", "", true)
	if !sysop.Sysop || sysop.ID <= u.ID {
		t.Errorf("sysop: %+v", sysop)
	}
	// booleans both ways
	if err := s.SetLocked(u.ID, true); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.UserByID(u.ID); !got.Locked {
		t.Error("not locked")
	}
	s.SetLocked(u.ID, false)
	if w, err := s.UsersBelow(30); err != nil || len(w) != 3 {
		t.Errorf("below 30: %d %v", len(w), err)
	}
	// an update to the same value is no "not found"
	if err := s.SetLevel(u.ID, 20); err != nil {
		t.Errorf("same level: %v", err)
	}
	if err := s.SetLevel(99999, 20); !errors.Is(err, ErrNotFound) {
		t.Errorf("no user: %v", err)
	}

	// areas, messages, ids, blobs, long text
	a, err := s.CreateArea("General", "Talk", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateArea("general", "", false); !errors.Is(err, ErrExists) {
		t.Errorf("area again: %v", err)
	}
	raw := []byte{0, 1, 2, 0xff, 0xfe, '\n', 0x80}
	body := strings.Repeat("A line of a long message, with ünïcødé and 'quotes' and ? marks.\n", 2000)
	id1, err := s.Post(&Message{AreaID: a.ID, FromID: u.ID, From: u.Name, To: "All", Subject: "First?",
		Body: body, MessageID: "<1@test>", Raw: raw})
	if err != nil {
		t.Fatal(err)
	}
	id2, err := s.Post(&Message{AreaID: a.ID, FromID: u.ID, From: u.Name, To: "All", Subject: "Second", Body: "b"})
	if err != nil || id2 <= id1 {
		t.Fatalf("second: %d %v", id2, err)
	}
	if _, err := s.Post(&Message{AreaID: a.ID, From: "x", Subject: "dup", Body: "b", MessageID: "<1@test>"}); !errors.Is(err, ErrExists) {
		t.Errorf("a Message-ID twice: %v", err)
	}
	m, err := s.Message(id1)
	if err != nil || m.Body != body || m.Subject != "First?" || m.MessageID != "<1@test>" {
		t.Fatalf("message: %v", err)
	}
	if got, err := s.MessageByMessageID("<1@test>"); err != nil || got.ID != id1 {
		t.Errorf("by Message-ID: %v", err)
	}
	if r, err := s.Raw(id1); err != nil || !bytes.Equal(r, raw) {
		t.Errorf("raw: %x %v", r, err)
	}
	if seen, _ := s.Seen("<1@test>"); !seen {
		t.Error("not in the history")
	}
	if err := s.Remember("<1@test>"); err != nil {
		t.Errorf("remember again: %v", err)
	}
	if got, _ := s.UserByID(u.ID); got.Posts != 2 {
		t.Errorf("posts %d", got.Posts)
	}
	// the last read only goes forward (upsert, greatest)
	for _, id := range []int64{id2, id1} {
		if err := s.SetLastRead(u.ID, a.ID, id); err != nil {
			t.Fatal(err)
		}
	}
	if lr, _ := s.LastRead(u.ID, a.ID); lr != id2 {
		t.Errorf("last read %d, want %d", lr, id2)
	}
	if n, _ := s.NewCount(u.ID, a.ID); n != 0 {
		t.Errorf("new %d", n)
	}
	// newsgroups, peers (upserts)
	if err := s.SetNewsgroup(a.ID, "bbs.general"); err != nil {
		t.Fatal(err)
	}
	b, _ := s.CreateArea("Other", "", false)
	if err := s.SetNewsgroup(b.ID, "bbs.general"); !errors.Is(err, ErrExists) {
		t.Errorf("a newsgroup twice: %v", err)
	}
	s.SetNewsgroup(b.ID, "") // NULLs do not clash
	c, _ := s.CreateArea("Third", "", false)
	if err := s.SetNewsgroup(c.ID, ""); err != nil {
		t.Errorf("two areas with no newsgroup: %v", err)
	}
	for _, last := range []int64{5, 9} {
		if err := s.SetPeerLast("peer", "bbs.general", last); err != nil {
			t.Fatal(err)
		}
		if err := s.SetPeerPushed("peer", last); err != nil {
			t.Fatal(err)
		}
	}
	if n, ok, _ := s.PeerLast("peer", "bbs.general"); !ok || n != 9 {
		t.Errorf("peer last %d %v", n, ok)
	}
	if n, ok, _ := s.PeerPushed("peer"); !ok || n != 9 {
		t.Errorf("peer pushed %d %v", n, ok)
	}

	// mail, "read"
	if _, err := s.SendMail(&Message{FromID: sysop.ID, From: "sysop", ToID: u.ID, To: u.Name, Subject: "hi", Body: "b"}); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.UnreadMail(u.ID); n != 1 {
		t.Errorf("unread %d", n)
	}
	inbox, _ := s.Inbox(u.ID)
	if err := s.MarkMailRead(u.ID, inbox[0].ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.UnreadMail(u.ID); n != 0 {
		t.Errorf("unread after reading %d", n)
	}

	// tokens and keys ("key")
	if _, err := s.AddToken(&Token{UserID: u.ID, Name: "t", Hash: "h1", Scopes: "read", Session: true}); err != nil {
		t.Fatal(err)
	}
	if tk, err := s.TokenByHash("h1"); err != nil || !tk.Session || tk.User != "Alice" {
		t.Errorf("token: %+v %v", tk, err)
	}
	k := &SSHKey{UserID: u.ID, Type: "ssh-ed25519", Fingerprint: "SHA256:x", Key: "ssh-ed25519 AAAA c"}
	if _, err := s.AddSSHKey(k); err != nil {
		t.Fatal(err)
	}
	if got, err := s.SSHKeyByFingerprint("SHA256:x"); err != nil || got.Key != k.Key {
		t.Errorf("key: %+v %v", got, err)
	}

	// file areas and files, the day's counts
	fa, err := s.CreateFileArea("Uploads", "", false)
	if err != nil {
		t.Fatal(err)
	}
	fid, err := s.AddFile(&File{AreaID: fa.ID, Name: "a.zip", Size: 10, SHA256: "ab", UploaderID: u.ID, Uploader: u.Name})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddFile(&File{AreaID: fa.ID, Name: "A.ZIP", Size: 1, SHA256: "cd", UploaderID: u.ID, Uploader: u.Name}); !errors.Is(err, ErrExists) {
		t.Errorf("a file name twice: %v", err)
	}
	s.CountDownload(fid)
	s.CountUserDownload(u.ID)
	s.AddTime(u.ID, 90)
	s.AddTime(u.ID, 30)
	if got, _ := s.UserByID(u.ID); got.DownloadsToday() != 1 || got.TimeToday() != 120 {
		t.Errorf("today: %d downloads, %d s", got.DownloadsToday(), got.TimeToday())
	}
	if fas, _ := s.FileAreas(); len(fas) != 1 || fas[0].Files != 1 || fas[0].Bytes != 10 {
		t.Errorf("file areas: %+v", fas[0])
	}

	// netmail
	if _, err := s.QueueNetmail(&Netmail{FromID: u.ID, From: u.Name, To: "Bob", ToAddr: "1:2/3",
		Subject: "s", Body: "b", MsgID: "1:2/4 1"}); err != nil {
		t.Fatal(err)
	}

	// deleting a user takes their mail, tokens and keys with them
	if err := s.DeleteUser(u.ID); err != nil {
		t.Fatal(err)
	}
	if ts, _ := s.Tokens(u.ID); len(ts) != 0 {
		t.Errorf("tokens left: %d", len(ts))
	}
	if _, err := s.SSHKeyByFingerprint("SHA256:x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("key left: %v", err)
	}
}

// TestCopy: a database copied into another (SQLite into the test engine,
// or into SQLite again): everything there, and ids going on after.
func TestCopy(t *testing.T) {
	src, err := Open("sqlite:" + filepath.Join(t.TempDir(), "src.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	u, _ := src.CreateUser("Alice", "alicepass1", "Here", true)
	a, _ := src.CreateArea("General", "", false)
	src.Post(&Message{AreaID: a.ID, FromID: u.ID, From: "Alice", Subject: "s", Body: "b", MessageID: "<m@x>", Raw: []byte{0, 0xff}})
	src.SendMail(&Message{FromID: u.ID, From: "Alice", ToID: u.ID, To: "Alice", Subject: "s", Body: "b"})
	src.AddToken(&Token{UserID: u.ID, Name: "t", Hash: "h", Scopes: "read", Session: true})
	src.SetPeerLast("p", "g", 3)

	dst := open(t)
	var tables []string
	if err := Copy(src, dst, func(table string, n int) { tables = append(tables, fmt.Sprintf("%s %d", table, n)) }); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(tables, ","), "users 1,areas 1,messages 1") {
		t.Errorf("copied: %v", tables)
	}
	got, err := dst.UserByName("alice")
	if err != nil || !got.Sysop || got.Location != "Here" {
		t.Fatalf("user: %+v %v", got, err)
	}
	if _, err := dst.CheckPassword("Alice", "alicepass1"); err != nil {
		t.Errorf("password: %v", err)
	}
	m, _ := dst.MessageByMessageID("<m@x>")
	if raw, _ := dst.Raw(m.ID); !bytes.Equal(raw, []byte{0, 0xff}) {
		t.Errorf("raw %x", raw)
	}
	if tk, _ := dst.TokenByHash("h"); tk == nil || !tk.Session {
		t.Errorf("token %+v", tk)
	}
	// new rows get new ids
	b, err := dst.CreateUser("Bob", "bobpass123", "", false)
	if err != nil || b.ID <= got.ID {
		t.Errorf("a new user after the copy: %+v %v", b, err)
	}
	if err := Copy(src, dst, nil); err == nil {
		t.Error("copied into a database with users")
	}
}
