package confui

import (
	"fmt"
	"strings"
	"testing"

	bbssec "github.com/D-Modem/bbs"
	"github.com/D-Modem/bbs/internal/bbs"
	"github.com/D-Modem/bbs/internal/confedit"
	"github.com/D-Modem/bbs/internal/vhost"
)

// mixedCheck is the BBS's check, as far as it goes here: [msn] on an IP of
// its own and [ymsg] on every address of the same port can't both listen.
func mixedCheck(data []byte) ([]string, error) {
	s := string(data)
	if strings.Contains(s, "https_address = 192.168.77.11:443") && strings.Contains(s, "https_address = :443") {
		return nil, fmt.Errorf("%w: port 443 is on every address for [ymsg] and on 192.168.77.11:443 for [msn]",
			vhost.ErrMixedAddresses)
	}
	return nil, nil
}

// TestRefuseMixedAddresses: with MSN's HTTPS server on an IP of its own,
// putting Yahoo!'s on every address of the same port is refused and put
// back; on an IP of its own it is taken.
func TestRefuseMixedAddresses(t *testing.T) {
	doc, err := confedit.Parse([]byte("[msn]\nhttps_address = 192.168.77.11:443\n[ymsg]\nhttps_address = 192.168.77.12:443\n"))
	if err != nil {
		t.Fatal(err)
	}
	orig, _ := confedit.Parse(doc.Bytes())
	e := &Editor{UI: &script{t: t}, Tools: bbs.ConfigTools{Check: mixedCheck}, doc: doc, orig: orig,
		cat: confedit.NewCatalog(bbssec.Sample)}
	it := e.field(sec("ymsg"), "https_address")
	err = it.set(":443")
	if err == nil || !strings.Contains(err.Error(), "on every address") {
		t.Fatalf("every address beside an IP of its own: %v", err)
	}
	if v, _ := e.doc.Get(sec("ymsg"), "https_address"); v != "192.168.77.12:443" {
		t.Errorf("not put back: %q", v)
	}
	if err := it.set("192.168.77.13:443"); err != nil {
		t.Fatalf("an IP of its own: %v", err)
	}
	if v, _ := e.doc.Get(sec("ymsg"), "https_address"); v != "192.168.77.13:443" {
		t.Errorf("kept %q", v)
	}
	// a file mixed already is put right an edit at a time
	e.doc.Set(sec("ymsg"), "https_address", ":443")
	if err := e.field(sec("ymsg"), "max_clients").set("100"); err != nil {
		t.Errorf("an edit beside a mix already there: %v", err)
	}
}
