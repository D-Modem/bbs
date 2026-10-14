package server

import (
	"fmt"
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
