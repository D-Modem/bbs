package cis

import (
	"testing"

	"github.com/D-Modem/bbs/internal/store"
)

// TestSLP: the vectors of SECURE-LOGIN.md, checked there against WinCIM
// 3.0.2's own routines.
func TestSLP(t *testing.T) {
	k := store.CISKeyOf("Test123")
	host := make([]byte, 96)
	for i := range host {
		host[i] = byte(i*3 + 1)
	}
	micro := make([]byte, 130)
	for i := range micro {
		micro[i] = byte(255 - i*5)
	}
	if got := slpEncode(slpUser(k, host, micro)); got != "Np0kkHKUf23uvI403FLnxDk=" {
		t.Errorf("UserResponse %s", got)
	}
	if got := slpEncode(slpHost(k, host, micro)); got != "oGVMq5T24l/3+UTs2i1QiS8=" {
		t.Errorf("HostResponse %s", got)
	}
	enc := slpEncode(micro)
	if len(enc) < 77 || enc[76] != '\r' {
		t.Errorf("not split at 76: %q", enc)
	}
	back, err := slpDecode(enc + ".")
	if err != nil || string(back) != string(micro) {
		t.Errorf("decoded %v", err)
	}
	bad := []byte(enc)
	bad[3] ^= 1
	if _, err := slpDecode(string(bad)); err == nil {
		t.Error("a bad checksum passed")
	}
}
