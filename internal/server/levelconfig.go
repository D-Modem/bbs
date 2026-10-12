package server

import (
	"fmt"
	"strconv"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/bbs"
	"github.com/Neo-Desktop/bbs-sec/internal/config"
)

// levelSections reads [level N] and [general]'s new_user_level and
// validated_level.
func (c *Config) levelSections(cf *config.File) error {
	s := &c.Settings
	g := cf.Get("general", "")
	var err error
	if s.NewUserLevel, err = g.Int("new_user_level", s.NewUserLevel, 0, bbs.MaxLevel); err != nil {
		return fmt.Errorf("[general] %w", err)
	}
	if s.ValidatedLevel, err = g.Int("validated_level", max(s.ValidatedLevel, s.NewUserLevel), 0, bbs.MaxLevel); err != nil {
		return fmt.Errorf("[general] %w", err)
	}
	if s.ValidatedLevel < s.NewUserLevel {
		return fmt.Errorf("[general] validated_level (%d) is below new_user_level (%d)", s.ValidatedLevel, s.NewUserLevel)
	}
	seen := map[int]bool{}
	for _, sec := range cf.All("level") {
		l, err := level(sec)
		if err != nil {
			return fmt.Errorf("[level %s] %w", sec.Name, err)
		}
		if seen[l.Num] {
			return fmt.Errorf("[level %s] given twice", sec.Name)
		}
		seen[l.Num] = true
		s.Levels = append(s.Levels, l)
	}
	bbs.SortLevels(s.Levels)
	if len(s.Levels) > 0 && s.Levels[0].Num > 0 {
		c.Warnings = append(c.Warnings, fmt.Sprintf("levels under %d have no [level]: no limits", s.Levels[0].Num))
	}
	return nil
}

func level(sec *config.Section) (bbs.Level, error) {
	n, err := strconv.Atoi(sec.Name)
	if err != nil || n < 0 || n > bbs.MaxLevel {
		return bbs.Level{}, fmt.Errorf("a level is a number, 0 to %d: [level 20]", bbs.MaxLevel)
	}
	l := bbs.AllowAll(n, sec.String("name", "Level "+sec.Name))
	mins, err := sec.Int("time", 0, 0, 24*60)
	if err != nil {
		return l, err
	}
	l.Time = time.Duration(mins) * time.Minute
	if l.Downloads, err = sec.Int("downloads", 0, 0, 100000); err != nil {
		return l, err
	}
	if l.Ratio, err = sec.Int("ratio", 0, 0, 1000); err != nil {
		return l, err
	}
	if l.RatioFree, err = sec.Int("ratio_free", 0, 0, 100000); err != nil {
		return l, err
	}
	for _, b := range []struct {
		key string
		p   *bool
	}{{"post", &l.Post}, {"mail", &l.Mail}, {"chat", &l.Chat}, {"page", &l.Page},
		{"download", &l.Download}, {"upload", &l.Upload}, {"doors", &l.Doors}} {
		if *b.p, err = sec.Bool(b.key, true); err != nil {
			return l, err
		}
	}
	return l, nil
}
