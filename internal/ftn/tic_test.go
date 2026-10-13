package ftn

import (
	"strings"
	"testing"
)

func TestTIC(t *testing.T) {
	in := "Area fsx_dat\r\nAreadesc fsxNet data\r\nOrigin 21:1/100\r\nFrom 21:1/100\r\nTo Test, 21:1/200\r\n" +
		"File FSXNET.Z83\r\nLfile fsxnet.z83\r\nReplaces FSXNET.Z*\r\nDesc fsxNet nodelist, day 283\r\n" +
		"Ldesc The weekly nodelist\r\nLdesc for fsxNet\r\nSize 1234\r\nCrc 1A2B3C4D\r\nCreated by bbs-sec\r\n" +
		"Path 21:1/100 1760000000 Fri Oct 10 2026\r\nSeenby 21:1/100\r\nSeenby 21:1/200\r\nPw SECRET\r\nUnknown thing\r\n\x1a"
	tc, err := ParseTIC([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if tc.Area != "FSX_DAT" || tc.Origin.String() != "21:1/100" || tc.Name() != "fsxnet.z83" || tc.Size != 1234 ||
		!tc.HasCRC || tc.CRC != 0x1A2B3C4D || len(tc.LDesc) != 2 || len(tc.Path) != 1 || len(tc.SeenBy) != 2 || tc.Pw != "SECRET" {
		t.Fatalf("%+v", tc)
	}
	if !tc.Seen(Addr{Zone: 21, Net: 1, Node: 200}) || tc.Seen(Addr{Zone: 21, Net: 1, Node: 300}) {
		t.Error("Seen")
	}
	again, err := ParseTIC(tc.Bytes())
	if err != nil || again.Area != tc.Area || again.CRC != tc.CRC || len(again.SeenBy) != 2 || again.Desc != tc.Desc ||
		strings.Join(again.LDesc, "|") != "The weekly nodelist|for fsxNet" {
		t.Fatalf("again %+v %v", again, err)
	}
	if _, err := ParseTIC([]byte("Area X\r\n")); err == nil {
		t.Error("no File taken")
	}
}
