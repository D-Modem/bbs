package textui

import (
	"reflect"
	"testing"
)

func typeIn(b *fsBuffer, s string) {
	for _, r := range s {
		if r == '\r' {
			b.Enter()
		} else {
			b.Insert(r)
		}
	}
}

func TestBufferWrap(t *testing.T) {
	b := newBuffer(nil, 10, 100)
	typeIn(b, "the quick brown fox")
	if got := b.Text(); !reflect.DeepEqual(got, []string{"the quick", "brown fox"}) {
		t.Errorf("wrapped %q", got)
	}
	if b.row != 1 || b.col != 9 {
		t.Errorf("cursor %d,%d", b.row, b.col)
	}
	// one long word breaks where it must
	b = newBuffer(nil, 5, 100)
	typeIn(b, "abcdefgh")
	if got := b.Text(); !reflect.DeepEqual(got, []string{"abcde", "fgh"}) {
		t.Errorf("long word %q", got)
	}
	// a body given is wrapped too
	b = newBuffer([]string{"one two three"}, 8, 100)
	if got := b.Text(); !reflect.DeepEqual(got, []string{"one two", "three"}) {
		t.Errorf("body %q", got)
	}
}

func TestBufferEditing(t *testing.T) {
	b := newBuffer(nil, 20, 100)
	typeIn(b, "hello\rworld")
	b.Move(KeyHome, 0)
	b.Backspace() // joins
	if got := b.Text(); !reflect.DeepEqual(got, []string{"helloworld"}) || b.col != 5 {
		t.Errorf("joined %q at %d", got, b.col)
	}
	b.Insert(' ')
	b.Move(KeyEnd, 0)
	b.Enter()
	typeIn(b, "x")
	b.Move(KeyUp, 0)
	b.Move(KeyEnd, 0)
	b.Delete() // joins the next line on
	if got := b.Text(); !reflect.DeepEqual(got, []string{"hello worldx"}) {
		t.Errorf("deleted %q", got)
	}
	b.KillLine()
	if got := b.Text(); len(got) != 0 {
		t.Errorf("killed %q", got)
	}
	// moving past line ends
	b = newBuffer([]string{"ab", "cd"}, 20, 100)
	b.Move(KeyEnd, 0)
	b.Move(KeyRight, 0)
	if b.row != 1 || b.col != 0 {
		t.Errorf("right %d,%d", b.row, b.col)
	}
	b.Move(KeyLeft, 0)
	if b.row != 0 || b.col != 2 {
		t.Errorf("left %d,%d", b.row, b.col)
	}
}

func TestBufferQuoteAndLimit(t *testing.T) {
	b := newBuffer(nil, 20, 4)
	if n := b.InsertLines([]string{"> one", "> two"}); n != 2 {
		t.Errorf("quoted %d", n)
	}
	typeIn(b, "mine")
	if got := b.Text(); !reflect.DeepEqual(got, []string{"> one", "> two", "mine"}) {
		t.Errorf("quoted %q", got)
	}
	b.Enter()
	if b.Enter() {
		t.Error("a fifth line")
	}
	if n := b.InsertLines([]string{"> three"}); n != 0 {
		t.Errorf("quoted past the limit: %d", n)
	}
}
