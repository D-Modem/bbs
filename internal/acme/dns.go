package acme

import (
	"bufio"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/challenge/dns01"
	"github.com/go-acme/lego/v4/providers/dns/acmedns"
	"github.com/go-acme/lego/v4/providers/dns/cloudflare"
	"github.com/go-acme/lego/v4/providers/dns/desec"
	"github.com/go-acme/lego/v4/providers/dns/digitalocean"
	"github.com/go-acme/lego/v4/providers/dns/duckdns"
	legoexec "github.com/go-acme/lego/v4/providers/dns/exec"
	"github.com/go-acme/lego/v4/providers/dns/gandiv5"
	"github.com/go-acme/lego/v4/providers/dns/godaddy"
	"github.com/go-acme/lego/v4/providers/dns/hetzner"
	"github.com/go-acme/lego/v4/providers/dns/httpreq"
	"github.com/go-acme/lego/v4/providers/dns/namecheap"
	"github.com/go-acme/lego/v4/providers/dns/ovh"
	"github.com/go-acme/lego/v4/providers/dns/pdns"
	"github.com/go-acme/lego/v4/providers/dns/porkbun"
	"github.com/go-acme/lego/v4/providers/dns/rfc2136"
	"github.com/go-acme/lego/v4/providers/dns/route53"
)

// DNSProviders are the DNS APIs built in, by lego's names for them; each
// takes its credentials from lego's environment variables (dns_env_file).
// Any other is reached through acme.sh's scripts (acmesh:dns_xx) or a
// program (exec:PATH).
var DNSProviders = map[string]func() (challenge.Provider, error){
	"acme-dns":     func() (challenge.Provider, error) { return acmedns.NewDNSProvider() },
	"cloudflare":   func() (challenge.Provider, error) { return cloudflare.NewDNSProvider() },
	"desec":        func() (challenge.Provider, error) { return desec.NewDNSProvider() },
	"digitalocean": func() (challenge.Provider, error) { return digitalocean.NewDNSProvider() },
	"duckdns":      func() (challenge.Provider, error) { return duckdns.NewDNSProvider() },
	"gandiv5":      func() (challenge.Provider, error) { return gandiv5.NewDNSProvider() },
	"godaddy":      func() (challenge.Provider, error) { return godaddy.NewDNSProvider() },
	"hetzner":      func() (challenge.Provider, error) { return hetzner.NewDNSProvider() },
	"httpreq":      func() (challenge.Provider, error) { return httpreq.NewDNSProvider() },
	"namecheap":    func() (challenge.Provider, error) { return namecheap.NewDNSProvider() },
	"ovh":          func() (challenge.Provider, error) { return ovh.NewDNSProvider() },
	"pdns":         func() (challenge.Provider, error) { return pdns.NewDNSProvider() },
	"porkbun":      func() (challenge.Provider, error) { return porkbun.NewDNSProvider() },
	"rfc2136":      func() (challenge.Provider, error) { return rfc2136.NewDNSProvider() },
	"route53":      func() (challenge.Provider, error) { return route53.NewDNSProvider() },
}

// ProviderNames lists the DNS providers built in.
func ProviderNames() []string {
	var out []string
	for n := range DNSProviders {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// CheckDNS tells whether a dns setting names a provider there is.
func CheckDNS(name string) error {
	switch {
	case name == "":
		return errors.New("challenge = dns needs dns (a provider, exec:PROGRAM or acmesh:dns_xx)")
	case strings.HasPrefix(name, "exec:"), strings.HasPrefix(name, "acmesh:dns_"):
		return nil
	case DNSProviders[name] != nil:
		return nil
	}
	return fmt.Errorf("dns = %s: not built in (%s; or exec:PROGRAM, acmesh:dns_xx)", name, strings.Join(ProviderNames(), ", "))
}

// dnsProvider makes the DNS-01 provider called name, its credentials from
// the environment and dns_env_file.
func (m *Manager) dnsProvider(name string) (challenge.Provider, error) {
	if err := m.loadEnv(); err != nil {
		return nil, err
	}
	switch {
	case strings.HasPrefix(name, "exec:"):
		cfg := legoexec.NewDefaultConfig()
		cfg.Program = strings.TrimPrefix(name, "exec:")
		return legoexec.NewDNSProviderConfig(cfg)
	case strings.HasPrefix(name, "acmesh:"):
		return m.acmesh(strings.TrimPrefix(name, "acmesh:"))
	case DNSProviders[name] != nil:
		return DNSProviders[name]()
	}
	return nil, CheckDNS(name)
}

// loadEnv puts dns_env_file's KEY=value lines in the environment, where
// lego's providers and acme.sh's scripts look for their credentials (once;
// those already set by the service manager stay).
func (m *Manager) loadEnv() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.envSet || m.S.DNSEnvFile == "" {
		return nil
	}
	env, err := ReadEnv(m.S.DNSEnvFile)
	if err != nil {
		return err
	}
	for k, v := range env {
		if _, ok := os.LookupEnv(k); !ok {
			os.Setenv(k, v)
		}
	}
	m.envSet = true
	return nil
}

// ReadEnv reads KEY=value lines (# comments, export and quotes allowed, as
// a shell or systemd's EnvironmentFile has them).
func ReadEnv(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("dns_env_file: %w", err)
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		k = strings.TrimSpace(k)
		if !ok || k == "" || strings.ContainsAny(k, " \t") {
			return nil, fmt.Errorf("%s:%d: not KEY=value", path, n)
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		out[k] = v
	}
	return out, sc.Err()
}

//go:embed acmesh.sh
var acmeshShim string

// acmeshProvider adds and removes the TXT record with one of acme.sh's
// dnsapi scripts (dns_cf, dns_gd, ...): acme.sh's own functions are loaded
// for it, so its saved credentials (account.conf) and its environment
// variables both work.
type acmeshProvider struct {
	m   *Manager
	api string
}

func (m *Manager) acmesh(api string) (challenge.Provider, error) {
	if m.S.ACMEsh == "" {
		return nil, errors.New("dns = acmesh:...: set acmesh, the path of acme.sh")
	}
	if _, err := os.Stat(m.S.ACMEsh); err != nil {
		return nil, fmt.Errorf("acmesh: %w", err)
	}
	if strings.ContainsAny(api, "/ ") || !strings.HasPrefix(api, "dns_") {
		return nil, fmt.Errorf("acmesh:%s: not a dnsapi name like dns_cf", api)
	}
	return &acmeshProvider{m: m, api: api}, nil
}

func (p *acmeshProvider) run(action, domain, keyAuth string) error {
	info := dns01.GetChallengeInfo(domain, keyAuth)
	fqdn := strings.TrimSuffix(info.EffectiveFQDN, ".")
	cmd := exec.Command("/bin/sh", "-c", acmeshShim, "acmesh", p.m.S.ACMEsh, p.api, action, fqdn, info.Value)
	cmd.Env = os.Environ()
	home := p.m.S.ACMEshHome
	if home == "" {
		home = filepath.Dir(p.m.S.ACMEsh)
	}
	cmd.Env = append(cmd.Env, "LE_WORKING_DIR="+home)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("acme.sh %s_%s: %v: %s", p.api, action, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (p *acmeshProvider) Present(domain, token, keyAuth string) error {
	return p.run("add", domain, keyAuth)
}

func (p *acmeshProvider) CleanUp(domain, token, keyAuth string) error {
	return p.run("rm", domain, keyAuth)
}

// Timeout gives acme.sh's slower DNS APIs time to publish the record.
func (p *acmeshProvider) Timeout() (timeout, interval time.Duration) {
	return 5 * time.Minute, 10 * time.Second
}
