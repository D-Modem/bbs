package main

import (
	"fmt"
	"net"

	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/certs"
	"github.com/D-Modem/bbs/internal/dnsd"
	"github.com/D-Modem/bbs/internal/msn"
	"github.com/D-Modem/bbs/internal/oscar"
	"github.com/D-Modem/bbs/internal/server"
	"github.com/D-Modem/bbs/internal/vhost"
	"github.com/D-Modem/bbs/internal/ymsg"
)

// sharedPort is the port p, handing each connection to its front end by
// the name (or path) the client asked for.
func sharedPort(b *bbs.BBS, cfg *server.Config, p server.SharedPort, ms *msn.Server, ys *ymsg.Server,
	osc *oscar.Server) (*vhost.Port, error) {
	vp := &vhost.Port{Address: p.Address, TLS: p.TLS, MaxClients: 512, MaxPerIP: 16, Log: b.Log,
		Refuse: func(c net.Conn) bool { return b.Refuse("web", c.RemoteAddr()) }}
	cert := func(who, certFile, keyFile string) (*certs.Source, error) {
		if !p.TLS {
			return nil, nil
		}
		src, err := certs.New(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("%s: tls_cert, tls_key: %w", who, err)
		}
		return src, nil
	}
	for _, m := range p.Members {
		rt := &vhost.Route{Name: m, Names: dnsd.Services[m]}
		var src *certs.Source
		var err error
		switch m {
		case "msn":
			rt.HTTP = ms
			rt.Paths = []string{"/rdr/pprdr.asp$", "/login2.srf$", "/rst.srf$"}
			src, err = cert(m, cfg.MSN.CertFile, cfg.MSN.KeyFile)
		case "ymsg":
			rt.HTTP = ys
			rt.Paths = []string{"/capacity$", "/config/pwtoken_", "/ycontent", "/relay$", "/notifyft$", "/ft/"}
			src, err = cert(m, cfg.YMSG.CertFile, cfg.YMSG.KeyFile)
		case "oscar":
			rt.Names = append(append([]string{}, dnsd.Services["oscar"]...), dnsd.Services["aol"]...)
			rt.HTTP = osc.WebHandler()
			rt.Paths = []string{"/auth/", "/aim/", "/$"}
			rt.Conn = osc.HandleConn
			rt.SSLv2 = true
			src, err = cert(m, cfg.OSCAR.TLSCert, cfg.OSCAR.TLSKey)
		default:
			return nil, fmt.Errorf("shared %s: %s can't share a port", p.Address, m)
		}
		if err != nil {
			return nil, err
		}
		if src != nil {
			rt.Cert = src.GetCertificate
		}
		vp.Routes = append(vp.Routes, rt)
	}
	b.Log.Printf("shared %s: %v, by the names asked for", p.Address, p.Members)
	return vp, nil
}
