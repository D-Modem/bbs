package server

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/config"
	"github.com/Neo-Desktop/bbs-sec/internal/door"
)

// doorSections reads the [door NAME]s.
func (c *Config) doorSections(cf *config.File) error {
	names := map[string]bool{}
	for _, s := range cf.All("door") {
		d, err := doorSection(s)
		if err != nil {
			return fmt.Errorf("[door %s] %w", s.Name, err)
		}
		if names[strings.ToLower(d.Name)] {
			return fmt.Errorf("[door %s] given twice", d.Name)
		}
		names[strings.ToLower(d.Name)] = true
		c.Doors = append(c.Doors, d)
	}
	return nil
}

func doorSection(s *config.Section) (*door.Door, error) {
	if s.Name == "" {
		return nil, errors.New("a door needs a name: [door NAME]")
	}
	d := &door.Door{Name: s.Name, Description: s.String("description", "")}
	var err error
	if d.Command, err = door.Split(s.String("command", "")); err != nil {
		return nil, fmt.Errorf("command: %w", err)
	}
	if len(d.Command) == 0 {
		return nil, errors.New("command: none given")
	}
	d.Dir = s.String("dir", filepath.Join("doors", "node{node}"))
	if !strings.Contains(d.Dir, "{node}") {
		return nil, errors.New("dir: must have {node} in it: each node its own drop files")
	}
	d.DropFiles = strings.Fields(strings.ToLower(strings.ReplaceAll(s.String("drop_files", door.DropDefault), ",", " ")))
	for _, f := range d.DropFiles {
		ok := false
		for _, k := range door.DropFiles {
			ok = ok || f == k
		}
		if !ok {
			return nil, fmt.Errorf("drop_files: %q is not one of %s", f, strings.Join(door.DropFiles, ", "))
		}
	}
	switch d.Charset = strings.ToLower(s.String("charset", door.CP437)); d.Charset {
	case door.CP437, door.UTF8, door.Raw:
	case "utf8":
		d.Charset = door.UTF8
	default:
		return nil, fmt.Errorf("charset: %q is not cp437, utf-8 or raw", d.Charset)
	}
	mins, err := s.Int("time_limit", 60, 1, 24*60)
	if err != nil {
		return nil, err
	}
	d.TimeLimit = time.Duration(mins) * time.Minute
	if d.SysopOnly, err = s.Bool("sysop_only", false); err != nil {
		return nil, err
	}
	if d.Single, err = s.Bool("single", false); err != nil {
		return nil, err
	}
	if d.PTY, err = s.Bool("pty", false); err != nil {
		return nil, err
	}
	if d.Cleanup, err = door.Split(s.String("cleanup", "")); err != nil {
		return nil, fmt.Errorf("cleanup: %w", err)
	}
	switch ct := strings.ToLower(s.String("door32_comm", "serial")); ct {
	case "serial":
		d.CommType = 1
	case "telnet":
		d.CommType = 2
	case "local":
		d.CommType = 0
	default:
		return nil, fmt.Errorf("door32_comm: %q is not serial, telnet or local", ct)
	}
	return d, nil
}
