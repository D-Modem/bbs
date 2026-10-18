package server

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/Neo-Desktop/bbs-sec/internal/config"
	"github.com/Neo-Desktop/bbs-sec/internal/qwk"
	"github.com/Neo-Desktop/bbs-sec/internal/qwknet"
)

// qwkNets reads the [qwknet NAME] sections: QWK networks this BBS is a
// node of.
func (c *Config) qwkNets(cf *config.File) error {
	names := map[string]bool{}
	for _, s := range cf.All("qwknet") {
		n, err := qwkNet(s)
		if err != nil {
			return fmt.Errorf("[qwknet %s] %w", s.Name, err)
		}
		if names[n.Name] {
			return fmt.Errorf("[qwknet %s] given twice", s.Name)
		}
		names[n.Name] = true
		if n.TLS == "no" {
			c.Warnings = append(c.Warnings, fmt.Sprintf("[qwknet %s] its password goes in the clear (tls = no)", n.Name))
		}
		c.QWKNets = append(c.QWKNets, n)
	}
	return nil
}

// qwkNet reads a [qwknet NAME] (its name lower-cased): the hub's FTP server
// and account, and its conferences (CONFERENCE = AREA), an area one each.
func qwkNet(s *config.Section) (*qwknet.Net, error) {
	name := strings.ToLower(s.Name)
	if name == "" || strings.ContainsAny(name, " /:") {
		return nil, errors.New("a QWK network needs a name of one word: [qwknet dovenet]")
	}
	n := &qwknet.Net{Name: name, Address: s.String("address", ""), TLS: strings.ToLower(s.String("tls", "explicit")),
		User: s.String("user", ""), Password: s.String("password", ""), Dir: s.String("dir", "/qwk"),
		HubID: strings.ToUpper(s.String("hub_id", "")), Confs: map[int]string{}}
	if _, _, err := net.SplitHostPort(n.Address); err != nil {
		return nil, errors.New("address: the hub's FTP server, host:port")
	}
	switch n.TLS {
	case "no", "explicit", "implicit":
	default:
		return nil, errors.New("tls: no, explicit or implicit")
	}
	if n.User == "" || n.Password == "" {
		return nil, errors.New("user and password: the account the hub gave this node")
	}
	if n.HubID != "" && !qwk.ValidID(n.HubID) {
		return nil, errors.New("hub_id: 1 to 8 letters and digits")
	}
	mins, err := s.Int("every", 60, 0, 7*24*60)
	if err != nil {
		return nil, err
	}
	n.Every = time.Duration(mins) * time.Minute
	areas := map[string]bool{}
	for _, k := range s.Keys() {
		conf, err := strconv.Atoi(k)
		if err != nil {
			continue // a setting, or misspelt: Unused says
		}
		area := strings.TrimSpace(s.String(k, ""))
		if conf < 1 || conf > 65535 || area == "" {
			return nil, fmt.Errorf("%s: CONFERENCE = AREA, the conference 1 to 65535", k)
		}
		if areas[strings.ToLower(area)] {
			return nil, fmt.Errorf("%s: the area %s carries another conference already", k, area)
		}
		areas[strings.ToLower(area)] = true
		n.Confs[conf] = area
	}
	if len(n.Confs) == 0 {
		return nil, errors.New("no conferences: CONFERENCE = AREA, e.g. 2001 = DOVE General")
	}
	return n, nil
}
