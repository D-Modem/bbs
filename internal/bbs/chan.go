package bbs

import (
	"image"

	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// ImageBoards are the image boards (internal/imageboard's), as the
// terminals' screens use them: to post as the web does, and to see the
// pictures.
type ImageBoards interface {
	// Submit posts, checked as every post is (bans, the board's rules,
	// flood control, the image); an error says why it was not posted, in
	// words for the poster.
	Submit(sub *ChanSubmission) (*store.BoardPost, error)
	// Picture is a post's image to fit in maxW by maxH pixels: its
	// thumbnail (a spoiler's says so) or, full, the image itself.
	Picture(b *store.Board, p *store.BoardPost, full bool, maxW, maxH int) (image.Image, error)
	// ShowOptions says whether a board's posters have Options (sage)
	ShowOptions(b *store.Board) bool
}

// ChanSubmission is a post as its poster wrote it.
type ChanSubmission struct {
	Board    *store.Board
	Thread   int64  // replied to (0: a new thread)
	Name     string // as typed: Name, Name#password, Name##password
	Email    string // "sage" does not bump
	Subject  string
	Comment  string
	Password string // to delete it by ("": none)
	Image    []byte // nil: none
	FileName string // the image's, as uploaded
	Spoiler  bool
	Capcode  bool        // staff posting as staff
	Sign     bool        // signed with the user's account name (not a typed one)
	IP       string      // whom flood control and bans are of
	User     *store.User // signed in (nil: no one)
	Staff    string      // what the poster is on the board ("admin", "mod", "")
}
