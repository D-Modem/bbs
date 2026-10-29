package server

import (
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/config"
	"github.com/D-Modem/bbs/internal/door"
)

// doorSections reads the [door NAME]s.
func (c *Config) doorSections(cf *config.File) error {
	names := map[string]bool{}
	for _, s := range cf.All("door") {
		d, err := doorSection(s)
		if err == nil && d.Remote != nil && d.Remote.Kind == door.KindDoorParty && d.Remote.HostKey == "" {
			c.Warnings = append(c.Warnings, fmt.Sprintf("[door %s] DoorParty's host key is not checked: "+
				"set host_key (SHA256:..., logged at the first call)", s.Name))
		}
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

// doorSection reads a [door NAME]: a program run with drop files in a
// directory of its node's, or with a type, a door elsewhere.
func doorSection(s *config.Section) (*door.Door, error) {
	if s.Name == "" {
		return nil, errors.New("a door needs a name: [door NAME]")
	}
	d := &door.Door{Name: s.Name, Description: s.String("description", "")}
	var err error
	kind := strings.ToLower(s.String("type", "program"))
	if kind != "program" {
		if d.Remote, err = remoteDoor(s, kind); err != nil {
			return nil, err
		}
		return d, doorCommon(s, d)
	}
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
	if err := doorCommon(s, d); err != nil {
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

// doorCommon reads what every kind of door has: its charset, time limit
// and who may open it.
func doorCommon(s *config.Section, d *door.Door) error {
	switch d.Charset = strings.ToLower(s.String("charset", door.CP437)); d.Charset {
	case door.CP437, door.UTF8, door.Raw:
	case "utf8":
		d.Charset = door.UTF8
	default:
		return fmt.Errorf("charset: %q is not cp437, utf-8 or raw", d.Charset)
	}
	mins, err := s.Int("time_limit", 60, 1, 24*60)
	if err != nil {
		return err
	}
	d.TimeLimit = time.Duration(mins) * time.Minute
	if d.SysopOnly, err = s.Bool("sysop_only", false); err != nil {
		return err
	}
	d.Require = s.String("require", "")
	if _, err := bbs.ParseRequire(d.Require); err != nil {
		return fmt.Errorf("require: %v", err)
	}
	if d.Single, err = s.Bool("single", false); err != nil {
		return err
	}
	return nil
}

// remoteDoor reads a door elsewhere: type = telnet, rlogin, bbslink or
// doorparty.
func remoteDoor(s *config.Section, kind string) (*door.Remote, error) {
	r := &door.Remote{Kind: kind}
	need := func(keys ...string) error {
		for _, k := range keys {
			if s.String(k, "") == "" {
				return fmt.Errorf("%s: needed for type = %s", k, kind)
			}
		}
		return nil
	}
	address := func(def string) error {
		r.Address = s.String("address", def)
		if _, _, err := net.SplitHostPort(r.Address); err != nil {
			return fmt.Errorf("address: host:port (%v)", err)
		}
		return nil
	}
	switch kind {
	case door.KindTelnet:
		if err := need("address"); err != nil {
			return nil, err
		}
		return r, address("")
	case door.KindRLogin:
		if err := need("address"); err != nil {
			return nil, err
		}
		r.ClientUser, r.ServerUser = s.String("client_user", "{user}"), s.String("server_user", "{user}")
		r.Terminal = s.String("terminal", "ansi")
		return r, address("")
	case door.KindBBSLink:
		if err := need("syscode", "authcode", "schemecode", "door"); err != nil {
			return nil, err
		}
		r.SysCode, r.AuthCode, r.SchemeCode = s.String("syscode", ""), s.String("authcode", ""), s.String("schemecode", "")
		r.DoorCode, r.URL = s.String("door", ""), s.String("url", door.BBSLinkURL)
		return r, address(door.BBSLinkAddress)
	case door.KindDoorParty:
		if err := need("ssh_user", "ssh_password", "tag"); err != nil {
			return nil, err
		}
		r.SSHUser, r.SSHPassword, r.Tag = s.String("ssh_user", ""), s.String("ssh_password", ""), s.String("tag", "")
		r.DoorCode, r.HostKey = s.String("door", ""), s.String("host_key", "")
		r.ClientUser = s.String("client_user", "{user}")
		if r.HostKey != "" && !strings.HasPrefix(r.HostKey, "SHA256:") {
			return nil, errors.New("host_key: its SHA256: fingerprint")
		}
		r.Target = s.String("rlogin_address", door.DoorPartyRLogin)
		if _, _, err := net.SplitHostPort(r.Target); err != nil {
			return nil, fmt.Errorf("rlogin_address: host:port (%v)", err)
		}
		return r, address(door.DoorPartyAddress)
	}
	return nil, fmt.Errorf("type: %q is not program, telnet, rlogin, bbslink or doorparty", kind)
}
