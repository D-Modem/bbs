package imageboard

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math"
	mrand "math/rand/v2"
	"strings"
	"sync"
	"time"

	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/f64"
	"golang.org/x/image/math/fixed"

	"github.com/Neo-Desktop/bbs-sec/internal/store"
)

// A captcha for those who post without signing in: letters drawn crooked
// over noise, made here (no outside service sees the posters).  Its token
// carries its answer sealed (AES-GCM, with a key of the boards' secret)
// and when it ends, so the server keeps nothing but the tokens used, until
// they would have ended anyway: each is good once.

const (
	captchaLetters = "ABCDEFGHJKLMNPRSTUVXYZ2345678" // none that look alike (I 1, O 0, Q; W in this font)
	captchaH       = 70
	// the letters a captcha may have
	MinCaptchaLength, MaxCaptchaLength = 4, 10
)

// CaptchaLevel is how hard a captcha is to read.
type CaptchaLevel struct {
	specks int     // dots under the letters
	lines  int     // lines through them
	angle  float64 // how far a letter turns, either way (radians)
	wave   float64 // how far the line of them waves (pixels, and as much again by chance)
	jitter float64 // how far a letter is out of line (pixels)
}

// CaptchaLevels are the captchas' difficulties.
var CaptchaLevels = map[string]CaptchaLevel{
	"easy":   {specks: 250, lines: 1, angle: 0.12, wave: 1, jitter: 3},
	"normal": {specks: 600, lines: 2, angle: 0.25, wave: 2, jitter: 6},
	"hard":   {specks: 1100, lines: 3, angle: 0.4, wave: 4, jitter: 10},
}

// captchaW is a captcha's width for so many letters.
func captchaW(n int) int { return 30*n + 20 }

var (
	errCaptchaOld   = errors.New("That verification has run out: here is a new one.")
	errCaptchaWrong = errors.New("Wrong verification: try the new one.")
)

// captchas are the tokens answered, until they end.
type captchas struct {
	mu   sync.Mutex
	used map[string]time.Time
}

func (s *Server) captchaAEAD() cipher.AEAD {
	k := sha256.Sum256(append([]byte("captcha:"), s.key...))
	block, _ := aes.NewCipher(k[:])
	aead, _ := cipher.NewGCM(block)
	return aead
}

// NewCaptcha is a new captcha's token.
func (s *Server) NewCaptcha() string {
	n := min(max(s.conf().CaptchaLength, MinCaptchaLength), MaxCaptchaLength)
	answer := make([]byte, n)
	for i := range answer {
		answer[i] = captchaLetters[mrand.IntN(len(captchaLetters))]
	}
	ttl := s.conf().CaptchaTTL
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	plain := binary.BigEndian.AppendUint64(nil, uint64(time.Now().Add(ttl).Unix()))
	plain = append(plain, answer...)
	aead := s.captchaAEAD()
	nonce := make([]byte, aead.NonceSize())
	rand.Read(nonce)
	return base64.RawURLEncoding.EncodeToString(aead.Seal(nonce, nonce, plain, nil))
}

// openCaptcha is a token's answer and when it ends.
func (s *Server) openCaptcha(token string) (string, time.Time, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	aead := s.captchaAEAD()
	if err != nil || len(raw) < aead.NonceSize()+8 || len(token) > 200 {
		return "", time.Time{}, errCaptchaOld
	}
	plain, err := aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], nil)
	if err != nil || len(plain) < 8+MinCaptchaLength || len(plain) > 8+MaxCaptchaLength {
		return "", time.Time{}, errCaptchaOld
	}
	return string(plain[8:]), time.Unix(int64(binary.BigEndian.Uint64(plain)), 0), nil
}

// CheckCaptcha checks an answer to a token, which is then used up.
func (s *Server) CheckCaptcha(token, answer string) error {
	want, ends, err := s.openCaptcha(token)
	if err != nil || time.Now().After(ends) {
		return errCaptchaOld
	}
	s.captchas.mu.Lock()
	defer s.captchas.mu.Unlock()
	if s.captchas.used == nil {
		s.captchas.used = map[string]time.Time{}
	}
	for t, e := range s.captchas.used {
		if time.Now().After(e) {
			delete(s.captchas.used, t)
		}
	}
	if _, ok := s.captchas.used[token]; ok {
		return errCaptchaOld
	}
	s.captchas.used[token] = ends // right or wrong: one try each
	got := strings.ToUpper(strings.Join(strings.Fields(answer), ""))
	if got != want {
		return errCaptchaWrong
	}
	return nil
}

// captchaImage draws a token's letters: each turned and scaled its own
// way, the line of them waved, lines and specks over it all.
func captchaImage(text, level string) []byte {
	lv, ok := CaptchaLevels[level]
	if !ok {
		lv = CaptchaLevels["normal"]
	}
	cw := captchaW(len(text))
	img := image.NewRGBA(image.Rect(0, 0, cw, captchaH))
	bg := color.RGBA{uint8(225 + mrand.IntN(30)), uint8(225 + mrand.IntN(30)), uint8(225 + mrand.IntN(30)), 255}
	draw.Draw(img, img.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)
	// specks under the letters
	for i := 0; i < lv.specks*len(text)/6; i++ {
		img.Set(mrand.IntN(cw), mrand.IntN(captchaH), color.RGBA{uint8(mrand.IntN(200)), uint8(mrand.IntN(200)), uint8(mrand.IntN(200)), 255})
	}
	face := basicfont.Face7x13
	step := float64(cw-20) / float64(len(text))
	for i, r := range text {
		glyph := image.NewRGBA(image.Rect(0, 0, 9, 15))
		ink := color.RGBA{uint8(mrand.IntN(120)), uint8(mrand.IntN(120)), uint8(mrand.IntN(140)), 255}
		d := &font.Drawer{Dst: glyph, Src: image.NewUniform(ink), Face: face, Dot: fixed.P(1, 12)}
		d.DrawString(string(r))
		scale := 3.0 + mrand.Float64()*0.9
		angle := (mrand.Float64()*2 - 1) * lv.angle
		cx := 10 + step*float64(i) + step/2 + (mrand.Float64()-0.5)*lv.jitter
		cy := float64(captchaH)/2 + (mrand.Float64()-0.5)*lv.jitter*1.5
		sin, cos := math.Sin(angle), math.Cos(angle)
		// glyph space to image: scale, turn, put at (cx, cy)
		gx, gy := 4.5, 7.5
		m := f64.Aff3{
			scale * cos, -scale * sin, cx - scale*(cos*gx-sin*gy),
			scale * sin, scale * cos, cy - scale*(sin*gx+cos*gy),
		}
		draw.BiLinear.Transform(img, m, glyph, glyph.Bounds(), draw.Over, nil)
	}
	// a wave through it all
	waved := image.NewRGBA(img.Bounds())
	amp, period, phase := lv.wave*(1+mrand.Float64()), 40+mrand.Float64()*30, mrand.Float64()*math.Pi*2
	for y := 0; y < captchaH; y++ {
		for x := 0; x < cw; x++ {
			sy := y + int(amp*math.Sin(float64(x)/period*2*math.Pi+phase))
			if sy < 0 || sy >= captchaH {
				waved.Set(x, y, bg)
				continue
			}
			waved.Set(x, y, img.At(x, sy))
		}
	}
	// lines across the letters
	for n := 0; n < lv.lines; n++ {
		ink := color.RGBA{uint8(mrand.IntN(150)), uint8(mrand.IntN(150)), uint8(mrand.IntN(150)), 255}
		y0, y1 := mrand.Float64()*captchaH, mrand.Float64()*captchaH
		bend := (mrand.Float64() - 0.5) * 40
		for x := 0; x < cw; x++ {
			t := float64(x) / float64(cw)
			y := y0 + (y1-y0)*t + bend*math.Sin(t*math.Pi)
			for w := 0; w < 2; w++ {
				waved.Set(x, int(y)+w, ink)
			}
		}
	}
	var out bytes.Buffer
	png.Encode(&out, waved)
	return out.Bytes()
}

// captchaPNG is a token's picture (captcha.png?t=TOKEN): a token that is
// not one, or has ended, draws nothing.
func (s *Server) captchaPNG(q *req) {
	answer, ends, err := s.openCaptcha(q.r.URL.Query().Get("t"))
	if err != nil || time.Now().After(ends) {
		q.notFound()
		return
	}
	h := q.w.Header()
	h.Set("Content-Type", "image/png")
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Security-Policy", "default-src 'none'")
	q.w.Write(captchaImage(answer, s.conf().CaptchaLevel))
}

// captchaJSON is a new captcha's token, for the page's script ("new one").
func (s *Server) captchaJSON(q *req) {
	q.json(map[string]string{"token": s.NewCaptcha()})
}

// captchaOn says whether a board's posters answer a captchas: its own
// say, or [chan]'s.
func (s *Server) captchaOn(b *store.Board) bool {
	switch b.Captcha {
	case "yes":
		return true
	case "no":
		return false
	}
	return s.conf().Captcha
}

// needsCaptcha says whether the caller must answer a captcha to post on a
// board: not when signed in.
func (q *req) needsCaptcha(b *store.Board) bool {
	return q.user == nil && q.s.captchaOn(b)
}

// ShowOptions says whether a board's post form has its Options line (sage,
// nonoko): its own say, or [chan]'s.  Without it, posts bump.
func (s *Server) ShowOptions(b *store.Board) bool {
	switch b.Options {
	case "yes":
		return true
	case "no":
		return false
	}
	return s.conf().Options
}
