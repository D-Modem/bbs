package server

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/imageboard"
	"github.com/D-Modem/bbs/internal/ui3270"
)

// TestPictures3270: on x3270, which has no vector graphics, an image
// board post's picture is drawn in graphic escape's blocks above what it
// is; turned off on the profile, it is said in words alone.
func TestPictures3270(t *testing.T) {
	if _, err := exec.LookPath("s3270"); err != nil {
		t.Skip("no s3270")
	}
	addr := freeTCP(t)
	e := startFull(t, ui3270.Graphics{Vector: true}, nil, func(b *bbs.BBS, dir string) {
		set := imageboard.Defaults()
		set.Dir, set.ThreadWait, set.ReplyWait = filepath.Join(dir, "chan"), 0, 0
		ib := &imageboard.Server{B: b, S: set}
		b.ImageBoards = ib
		bd, err := b.Store.CreateBoard(imageboard.NewBoard("b", "Random"))
		if err != nil {
			t.Fatal(err)
		}
		// red over green
		img := image.NewRGBA(image.Rect(0, 0, 200, 100))
		for y := 0; y < 100; y++ {
			for x := 0; x < 200; x++ {
				c := color.RGBA{255, 0, 0, 255}
				if y >= 50 {
					c = color.RGBA{0, 255, 0, 255}
				}
				img.Set(x, y, c)
			}
		}
		var data bytes.Buffer
		png.Encode(&data, img)
		if _, err := ib.Submit(&imageboard.Submission{Board: bd, Subject: "Flag", Comment: "red over green",
			Image: data.Bytes(), FileName: "flag.png", IP: "test"}); err != nil {
			t.Fatal(err)
		}
	}, &Listener{Name: "telnet", Kind: "telnet", Address: addr, TN3270: true})
	view := func() string {
		script := strings.Join([]string{
			"Connect(" + addr + ")", "Wait(10,InputField)",
			`String("alice")`, "Tab()", `String("alicepass1")`, "Enter()", "Wait(5,InputField)",
			`String("b")`, "Enter()", "Wait(5,InputField)", // image boards
			`String("s")`, "Enter()", "Wait(5,InputField)", // /b/
			`String("s")`, "Enter()", "Wait(5,InputField)", // the thread
			"Ascii()", "ReadBuffer(Ascii)", "Quit()", ""}, "\n")
		cmd := exec.Command("s3270", "-model", "3279-2")
		cmd.Stdin = strings.NewReader(script)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("s3270: %v\n%s", err, out)
		}
		if os.Getenv("SHOW3270") != "" {
			t.Logf("%s", out)
		}
		return string(out)
	}
	out := view()
	// the buffer's rows: 5 to 10 red blocks, 11 to 16 green (a cell
	// 9x12 on s3270: 32 cells by 12 rows is the image's 2:1)
	var rows []string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "data: ") {
			rows = append(rows, l)
		}
	}
	if len(rows) < 48 {
		t.Fatalf("no buffer:\n%s", out)
	}
	rows = rows[len(rows)-24:] // ReadBuffer's, after Ascii's
	blocks := strings.Repeat("3f ", 32) + "SA(42=00)"
	for r := 5; r <= 16; r++ {
		color := map[bool]string{true: "f2", false: "f4"}[r <= 10]
		if !strings.Contains(rows[r], "SA(42="+color+") "+blocks) {
			t.Errorf("row %d, not %s blocks: %s", r, color, rows[r])
		}
	}
	// the text after it in its own color, not the picture's last
	if strings.Contains(rows[17], "SA(") || !strings.Contains(rows[17], "5b 66 6c 61 67") {
		t.Errorf("the caption's row: %s", rows[17])
	}
	if !strings.Contains(out, "[flag.png, 200x100, 0 KB]") || !strings.Contains(out, "red over green") {
		t.Errorf("no caption or body:\n%s", out)
	}

	// pictures off: in words alone
	u, _ := e.b.Store.UserByName("alice")
	if err := e.b.Store.SetPrefs(u.ID, "pictures=off"); err != nil {
		t.Fatal(err)
	}
	out = view()
	if strings.Contains(out, "SA(42=f2)") || !strings.Contains(out, "data:  [flag.png, 200x100, 0 KB]") {
		t.Errorf("pictures off:\n%s", out)
	}
}
