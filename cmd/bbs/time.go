package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/server"
	"github.com/D-Modem/bbs/internal/sockets"
	"github.com/D-Modem/bbs/internal/tcpserv"
	"github.com/D-Modem/bbs/internal/timesync"
)

// startTime starts what [time], [ntp], [nts] and [ptp] ask: the clock
// following its servers, and the NTP, NTS-KE and PTP servers.
func startTime(ctx context.Context, b *bbs.BBS, t *server.TimeSettings, lg *log.Logger, stop func()) error {
	if t == nil {
		return nil
	}
	clock := &timesync.Clock{Local: t.Sync.Local, SetClock: t.Sync.SetClock, Log: lg.Printf}
	refuse := func(ip net.IP) bool { no, _ := b.Guard.Refused(ip.String()); return no }
	run := func(name string, f func() error) {
		go func() {
			if err := f(); err != nil {
				lg.Printf("%s: %v", name, err)
				stop()
			}
		}()
	}
	var port *timesync.PTP
	if t.PTP != nil {
		port = &timesync.PTP{S: *t.PTP, Clock: clock, Refuse: refuse, Log: lg.Printf}
		if err := port.Open(); err != nil {
			return err
		}
	}
	if len(t.Sync.Servers) > 0 {
		f := &timesync.Follower{S: t.Sync, Clock: clock, PTP: port, Log: lg.Printf}
		if err := f.Prepare(); err != nil {
			return fmt.Errorf("time: %w", err)
		}
		lg.Printf("time: following %d servers, every %v", len(t.Sync.Servers), t.Sync.Poll)
		go f.Run(ctx)
	}
	if port != nil {
		if port.S.Master {
			lg.Printf("ptp: grandmaster on %v (domain %d, multicast %v, unicast %v)", port.S.Ports(), port.S.Domain,
				port.S.Multicast, port.S.Unicast)
		}
		run("ptp", func() error { return port.Run(ctx) })
	}
	if n := t.NTP; n != nil {
		c, err := sockets.ListenUDP(n.Address)
		if err != nil {
			return fmt.Errorf("ntp: %w", err)
		}
		s := &timesync.NTPServer{Clock: clock, Refuse: refuse, Rate: float64(n.Rate), Burst: n.Burst, Log: lg.Printf}
		if t.NTS != nil {
			if s.Keys, err = timesync.NewKeys(ctx); err != nil {
				return err
			}
		}
		lg.Printf("ntp: answering on %s (nts %v)", c.LocalAddr(), s.Keys != nil)
		run("ntp", func() error { return s.Serve(ctx, c) })
		if k := t.NTS; k != nil {
			_, p, _ := net.SplitHostPort(n.Address)
			np, _ := strconv.Atoi(p)
			ke := &timesync.KEServer{CertFile: k.CertFile, KeyFile: k.KeyFile, NTPHost: k.NTPHost, NTPPort: np, Keys: s.Keys}
			tc, err := ke.TLSConfig()
			if err != nil {
				return fmt.Errorf("nts: %w", err)
			}
			ln, err := sockets.Listen("tcp", k.Address)
			if err != nil {
				return fmt.Errorf("nts: %w", err)
			}
			ts := &tcpserv.Server{Name: "nts", MaxClients: k.MaxClients, MaxPerIP: k.MaxPerIP, Timeout: 15 * time.Second,
				Log: lg, Refuse: func(c net.Conn) bool { return b.Refuse("nts", c.RemoteAddr()) },
				Handle: func(c net.Conn) { ke.Handle(tls.Server(c, tc)) }}
			run("nts", func() error { return ts.Serve(ctx, ln) })
		}
	}
	return nil
}

// timeCmd is bbs time: the configured servers asked once, or the one
// named (KIND HOST).
func timeCmd(cfg *server.Config, args []string) {
	var list []timesync.Server
	rootCAs := ""
	ptps := timesync.PTPSettings{UTCOffset: 37}
	if t := cfg.Time; t != nil {
		list, rootCAs = t.Sync.Servers, t.Sync.RootCAs
		if t.PTP != nil {
			ptps = *t.PTP
		}
		ptps.UTCOffset = t.UTCOffset
	}
	ptps.Master = false
	if len(args) > 0 {
		s, err := timesync.ParseServers(joinArgs(args))
		if err != nil {
			fatal(err)
		}
		list = s
	}
	if len(list) == 0 {
		fatal(fmt.Errorf("no servers: [time] servers, or bbs time KIND HOST (ntp, nts, ptp)"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	failed := false
	for _, s := range list {
		var port *timesync.PTP
		if s.Kind == "ptp" {
			port = &timesync.PTP{S: ptps, Clock: &timesync.Clock{}}
			if err := port.Open(); err != nil {
				fmt.Printf("%-32s %v (ports 319 and 320: root's, or CAP_NET_BIND_SERVICE)\n", s, err)
				failed = true
				continue
			}
		}
		sm, err := timesync.Query(ctx, s, rootCAs, port)
		if port != nil {
			port.Close()
		}
		if err != nil {
			fmt.Printf("%-32s %v\n", s, err)
			failed = true
			continue
		}
		fmt.Printf("%-32s offset %+v, round trip %v, stratum %d, leap %d\n", s,
			sm.Offset.Round(time.Microsecond), sm.Delay.Round(time.Microsecond), sm.Stratum, sm.Leap)
	}
	if failed {
		os.Exit(1)
	}
}

func joinArgs(a []string) string {
	s := ""
	for i := 0; i+1 < len(a); i += 2 {
		if s != "" {
			s += ","
		}
		s += a[i] + " " + a[i+1]
	}
	if len(a)%2 == 1 {
		if s != "" {
			s += ","
		}
		s += a[len(a)-1]
	}
	return s
}
