package app

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"testing"

	"github.com/Neo-Desktop/bbs-sec/internal/imageboard"
	"github.com/Neo-Desktop/bbs-sec/internal/view"
)

// TestImageBoards: on a terminal, a thread started with an image sent by
// a transfer protocol, a reply quoting it, the posts read with their
// pictures, the image viewed whole.
func TestImageBoards(t *testing.T) {
	b, alice, _ := setup(t)
	set := imageboard.Defaults()
	set.Dir, set.ThreadWait, set.ReplyWait = filepath.Join(t.TempDir(), "chan"), 0, 0
	b.ImageBoards = &imageboard.Server{B: b, S: set}
	bd, err := b.Store.CreateBoard(imageboard.NewBoard("b", "Random"))
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 800, 400))
	for x := 0; x < 800; x++ {
		for y := 0; y < 400; y++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 99, 255})
		}
	}
	var pngData bytes.Buffer
	png.Encode(&pngData, img)

	f := &fake{t: t, b: b, upload: pngData.Bytes(), script: []any{
		"B", view.Choice{Action: view.Pick, Row: 0}, // /b/
		view.Choice{Action: "new"}, map[string]string{"name": "Anon#tea", "options": "none"},
		true, false, // an image, not a spoiler
		draft{"", "Hello", []string{"first post"}},
		view.Choice{Action: view.Pick, Row: 0}, // the thread
		"reply", map[string]string{"name": "Random", "options": "sage"}, false, draft{"", "", []string{">>1", "nice"}},
		"prev", // from the reply to the first post
		"view", view.Back,
		view.Back, view.Choice{Action: view.Back}, view.Choice{Action: view.Back},
	}}
	if err := Run(b, alice, f); err != ErrLogoff {
		t.Fatalf("Run: %v; shown %q", err, f.shown)
	}
	if len(f.script) > 0 {
		t.Errorf("left of the script: %v; shown %q", f.script, f.shown)
	}
	has(t, "screens", f.shown, "menu Main menu")
	has(t, "screens", f.shown, "table Image boards: /b/|Random|0|")
	has(t, "said", f.said, "Posted: /b/ No.1.")
	has(t, "said", f.said, "Posted: /b/ No.2.")
	has(t, "screens", f.shown, "table /b/ - Random: 1|0|0|Hello")
	has(t, "screens", f.shown, "message : >>1\nnice")
	has(t, "screens", f.shown, "message Hello: first post\n\nReplies: >>2")
	// the thumbnail (250x125) fitted to 200x100, and the image whole
	has(t, "screens", f.shown, "picture upload.png, 800x400, ")
	has(t, "screens", f.shown, ": 200x100")
	has(t, "screens", f.shown, "panel /b/ No.1")

	op, _ := b.Store.ChanPost(bd.ID, 1)
	reply, _ := b.Store.ChanPost(bd.ID, 2)
	if op.Trip != "!WokonZwxw2" || op.Name != "Anon" || op.IP != "bbs:alice" || op.UserID != alice.User().ID ||
		reply.Name != "Random" || reply.Thread != 1 || reply.HasFile() {
		t.Errorf("posts: %+v %+v", op, reply)
	}
	th, _ := b.Store.ChanThread(bd.ID, 1)
	if !th.Bumped.Equal(op.Bumped) {
		t.Error("sage bumped it")
	}
}
