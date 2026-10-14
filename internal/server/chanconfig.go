package server

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/Neo-Desktop/bbs-sec/internal/config"
	"github.com/Neo-Desktop/bbs-sec/internal/imageboard"
)

// chanSection reads [chan]: how the image boards are run.
func (c *Config) chanSection(s *config.Section) error {
	ch := imageboard.Defaults()
	ch.Dir = s.String("dir", ch.Dir)
	var err error
	if ch.MaxFile, err = parseSize(s, "max_file", ch.MaxFile); err != nil {
		return err
	}
	if ch.MaxFile < 1<<10 || ch.MaxFile > 1<<30 {
		return fmt.Errorf("max_file: from 1K to 1G")
	}
	if ch.ThreadWait, err = s.Seconds("thread_wait", int(ch.ThreadWait.Seconds()), 0, 86400); err != nil {
		return err
	}
	if ch.ReplyWait, err = s.Seconds("reply_wait", int(ch.ReplyWait.Seconds()), 0, 86400); err != nil {
		return err
	}
	if ch.MaxComment, err = s.Int("max_comment", ch.MaxComment, 100, 20000); err != nil {
		return err
	}
	if ch.Strip, err = s.Bool("strip_metadata", ch.Strip); err != nil {
		return err
	}
	if ch.Captcha, err = s.Bool("captcha", ch.Captcha); err != nil {
		return err
	}
	if ch.CaptchaLength, err = s.Int("captcha_length", ch.CaptchaLength, imageboard.MinCaptchaLength,
		imageboard.MaxCaptchaLength); err != nil {
		return err
	}
	if ch.CaptchaTTL, err = s.Seconds("captcha_lifetime", int(ch.CaptchaTTL.Seconds()), 60, 86400); err != nil {
		return err
	}
	if ch.CaptchaLevel = strings.ToLower(s.String("captcha_level", ch.CaptchaLevel)); imageboard.CaptchaLevels[ch.CaptchaLevel] == (imageboard.CaptchaLevel{}) {
		return fmt.Errorf("captcha_level: easy, normal or hard, not %q", ch.CaptchaLevel)
	}
	if ch.Options, err = s.Bool("options", ch.Options); err != nil {
		return err
	}
	// the country database and its credit are [general]'s unless the
	// boards have their own
	ch.GeoCredit = s.String("geoip_credit", c.Settings.GeoCredit)
	ch.GeoCreditURL = s.String("geoip_credit_url", c.Settings.GeoCreditURL)
	if u := ch.GeoCreditURL; u != "" && !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") {
		return fmt.Errorf("geoip_credit_url: an http(s) address, not %q", u)
	}
	if ch.GeoIP = s.String("geoip", c.Settings.GeoIP); ch.GeoIP != "" && ch.GeoIP != c.Settings.GeoIP {
		if _, err := os.Stat(ch.GeoIP); err != nil {
			c.Warnings = append(c.Warnings, fmt.Sprintf("[chan] geoip: %v: no flags until it is there", err))
		}
	}
	// videos' and PDFs' thumbnails: the helpers, if the sysop has them
	for _, h := range []struct {
		key string
		p   *string
	}{{"ffmpeg", &ch.FFmpeg}, {"pdftoppm", &ch.PDFToPPM}} {
		if v := s.String(h.key, ""); v != "" {
			path, err := exec.LookPath(v)
			if err != nil {
				return fmt.Errorf("%s: %w", h.key, err)
			}
			*h.p = path
		}
	}
	if ch.MaxVideo, err = s.Seconds("max_video", int(ch.MaxVideo.Seconds()), 0, 86400); err != nil {
		return err
	}
	if ch.Style = s.String("style", ""); ch.Style != "" && !imageboard.KnownStyle(ch.Style) {
		var names []string
		for _, st := range imageboard.Styles {
			names = append(names, st.Name)
		}
		return fmt.Errorf("style: %q is not one of %s", ch.Style, strings.Join(names, ", "))
	}
	c.Chan = ch
	return nil
}
