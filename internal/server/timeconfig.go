package server

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strconv"

	"github.com/D-Modem/bbs/internal/config"
	"github.com/D-Modem/bbs/internal/timesync"
)

// TimeSettings are [time], [ntp], [nts] and [ptp]'s: the servers the
// BBS's clock follows, and the time servers it is.
type TimeSettings struct {
	Sync      timesync.Settings
	UTCOffset int
	NTP       *NTPSettings          // nil: no NTP server
	NTS       *NTSSettings          // nil: no NTS-KE server
	PTP       *timesync.PTPSettings // nil: no PTP port (no [ptp], no ptp servers)
}

// NTPSettings are [ntp]'s.
type NTPSettings struct {
	Address string
	Rate    int // requests a second an address may make (0: no limit)
	Burst   int
}

// NTSSettings are [nts]'s.
type NTSSettings struct {
	Address           string
	CertFile, KeyFile string
	NTPHost           string // the NTP server clients are told of ("": this one)
	MaxClients        int
	MaxPerIP          int
}

// timeSections reads [time], [ntp], [nts] and [ptp].
func (c *Config) timeSections(cf *config.File) error {
	ts, ns, ks, ps := cf.Get("time", ""), cf.Get("ntp", ""), cf.Get("nts", ""), cf.Get("ptp", "")
	if ts == nil && ns == nil && ks == nil && ps == nil {
		return nil
	}
	t := &TimeSettings{}
	if err := func() error {
		var err error
		if t.Sync.Servers, err = timesync.ParseServers(ts.String("servers", "")); err != nil {
			return fmt.Errorf("servers: %v", err)
		}
		if t.Sync.Poll, err = ts.Seconds("poll", 64, 16, 86400); err != nil {
			return err
		}
		if t.Sync.SetClock, err = ts.Bool("set_clock", false); err != nil {
			return err
		}
		if t.Sync.Local.Stratum, err = ts.Int("stratum", 0, 0, 15); err != nil {
			return err
		}
		t.Sync.Local.RefID = ts.String("refid", "")
		if len(t.Sync.Local.RefID) > 4 {
			return errors.New("refid: four letters at most (GPS, PPS ...)")
		}
		if t.Sync.RootCAs = ts.String("root_cas", ""); t.Sync.RootCAs != "" {
			if _, err := timesync.RootCAs(t.Sync.RootCAs); err != nil {
				return fmt.Errorf("root_cas: %v", err)
			}
		}
		if t.UTCOffset, err = ts.Int("utc_offset", 37, 0, 1000); err != nil {
			return err
		}
		if len(t.Sync.Servers) > 0 && t.Sync.Local.Stratum > 0 {
			c.Warnings = append(c.Warnings, "[time] stratum is for no servers: it is not used")
		}
		return nil
	}(); err != nil {
		return fmt.Errorf("[time] %w", err)
	}
	if ns != nil {
		n := &NTPSettings{Address: ns.String("address", ":123")}
		err := func() error {
			if _, _, err := net.SplitHostPort(n.Address); err != nil {
				return fmt.Errorf("address: %v", err)
			}
			var err error
			if n.Rate, err = ns.Int("rate", 2, 0, 100000); err != nil {
				return err
			}
			n.Burst, err = ns.Int("burst", 16, 1, 100000)
			return err
		}()
		if err != nil {
			return fmt.Errorf("[ntp] %w", err)
		}
		t.NTP = n
	}
	if ks != nil {
		k := &NTSSettings{Address: ks.String("address", ":"+strconv.Itoa(timesync.KEPort)),
			CertFile: ks.String("tls_cert", ""), KeyFile: ks.String("tls_key", ""), NTPHost: ks.String("ntp_host", "")}
		err := func() error {
			if t.NTP == nil {
				return errors.New("needs [ntp]: its cookies are for the NTP server")
			}
			if _, _, err := net.SplitHostPort(k.Address); err != nil {
				return fmt.Errorf("address: %v", err)
			}
			if k.CertFile == "" || k.KeyFile == "" {
				return errors.New("tls_cert and tls_key: NTS-KE is TLS")
			}
			if _, err := tls.LoadX509KeyPair(k.CertFile, k.KeyFile); err != nil {
				return fmt.Errorf("tls_cert, tls_key: %v", err)
			}
			var err error
			if k.MaxClients, err = ks.Int("max_clients", 64, 1, 10000); err != nil {
				return err
			}
			k.MaxPerIP, err = ks.Int("max_per_ip", 8, 0, 1000)
			return err
		}()
		if err != nil {
			return fmt.Errorf("[nts] %w", err)
		}
		t.NTS = k
	}
	needPTP := ps != nil
	for _, s := range t.Sync.Servers {
		needPTP = needPTP || s.Kind == "ptp"
	}
	if needPTP {
		p := &timesync.PTPSettings{Address: ps.String("address", ""), Interface: ps.String("interface", ""),
			Master: ps != nil, UTCOffset: t.UTCOffset}
		err := func() error {
			if p.Address != "" && net.ParseIP(p.Address) == nil {
				return errors.New("address: an IP address (the ports are 319 and 320)")
			}
			if p.Interface != "" {
				if _, err := net.InterfaceByName(p.Interface); err != nil {
					return fmt.Errorf("interface: %v", err)
				}
			}
			var err error
			if p.Multicast, err = ps.Bool("multicast", p.Interface != ""); err != nil {
				return err
			}
			if p.Multicast && p.Interface == "" {
				return errors.New("multicast needs an interface")
			}
			if p.Unicast, err = ps.Bool("unicast", true); err != nil {
				return err
			}
			ints := []struct {
				key         string
				def, lo, hi int
				set         func(int)
			}{
				{"domain", 0, 0, 239, func(v int) { p.Domain = uint8(v) }},
				{"priority1", 128, 0, 255, func(v int) { p.Priority1 = uint8(v) }},
				{"priority2", 128, 0, 255, func(v int) { p.Priority2 = uint8(v) }},
				{"log_sync_interval", 0, -7, 4, func(v int) { p.SyncLog = int8(v) }},
				{"log_announce_interval", 1, -3, 4, func(v int) { p.AnnounceLog = int8(v) }},
				{"log_min_interval", -4, -7, 4, func(v int) { p.MinLog = int8(v) }},
				{"max_clients", 256, 1, 100000, func(v int) { p.MaxClients = v }},
			}
			for _, x := range ints {
				v, err := ps.Int(x.key, x.def, x.lo, x.hi)
				if err != nil {
					return err
				}
				x.set(v)
			}
			switch ps.String("timestamping", "software") {
			case "software":
			case "hardware":
				if p.Interface == "" {
					return errors.New("timestamping = hardware needs an interface (its network card's)")
				}
				p.Hardware = true
			default:
				return errors.New("timestamping: software or hardware")
			}
			return nil
		}()
		if err != nil {
			return fmt.Errorf("[ptp] %w", err)
		}
		t.PTP = p
	}
	if t.Sync.SetClock && c.RunAs != nil {
		c.Warnings = append(c.Warnings, "[time] set_clock: the BBS gives up root ([general] user), so it needs CAP_SYS_TIME kept (systemd: AmbientCapabilities=CAP_SYS_TIME)")
	}
	c.Time = t
	return nil
}

// String is for check's report.
func (t *TimeSettings) String() string {
	s := fmt.Sprintf("time: %d servers, poll %v, set clock %v", len(t.Sync.Servers), t.Sync.Poll, t.Sync.SetClock)
	if t.NTP != nil {
		s += ", ntp on " + t.NTP.Address
	}
	if t.NTS != nil {
		s += ", nts-ke on " + t.NTS.Address
	}
	if p := t.PTP; p != nil && p.Master {
		s += fmt.Sprintf(", ptp grandmaster (domain %d, multicast %v, unicast %v)", p.Domain, p.Multicast, p.Unicast)
	}
	return s
}
