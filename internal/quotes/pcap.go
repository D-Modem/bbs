package quotes

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Packets reads a capture file (pcap or pcapng, as IEX's HIST files are)
// and gives each UDP payload in it to f, in order.
func Packets(r io.Reader, f func(payload []byte) error) error {
	br := bufio.NewReaderSize(r, 1<<20)
	magic, err := br.Peek(4)
	if err != nil {
		return err
	}
	switch {
	case magic[0] == 0x0a && magic[1] == 0x0d && magic[2] == 0x0d && magic[3] == 0x0a:
		return pcapng(br, f)
	case binary.LittleEndian.Uint32(magic) == 0xa1b2c3d4, binary.LittleEndian.Uint32(magic) == 0xa1b23c4d,
		binary.BigEndian.Uint32(magic) == 0xa1b2c3d4, binary.BigEndian.Uint32(magic) == 0xa1b23c4d:
		return pcap(br, f)
	}
	return errors.New("not a pcap or pcapng file")
}

// pcap reads the classic format.
func pcap(r io.Reader, f func([]byte) error) error {
	var hdr [24]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return err
	}
	var bo binary.ByteOrder = binary.LittleEndian
	if m := binary.LittleEndian.Uint32(hdr[:]); m != 0xa1b2c3d4 && m != 0xa1b23c4d {
		bo = binary.BigEndian
	}
	link := bo.Uint32(hdr[20:]) & 0x0fffffff
	var rec [16]byte
	buf := make([]byte, 0, 65536)
	for {
		if _, err := io.ReadFull(r, rec[:]); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		n := bo.Uint32(rec[8:])
		if n > 1<<20 {
			return fmt.Errorf("pcap: a packet of %d bytes", n)
		}
		buf = buf[:n]
		if _, err := io.ReadFull(r, buf); err != nil {
			return err
		}
		if p := udpPayload(buf, link); p != nil {
			if err := f(p); err != nil {
				return err
			}
		}
	}
}

// pcapng reads the next generation format: blocks, each with its type and
// length; the interface description blocks say each interface's link type.
func pcapng(r io.Reader, f func([]byte) error) error {
	var bo binary.ByteOrder = binary.LittleEndian
	var links []uint32
	var head [8]byte
	buf := make([]byte, 0, 65536)
	for {
		if _, err := io.ReadFull(r, head[:]); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		typ := bo.Uint32(head[:])
		if typ == 0x0a0d0d0a {
			// a section header: its byte order is in its body
			var bom [4]byte
			if _, err := io.ReadFull(r, bom[:]); err != nil {
				return err
			}
			if binary.LittleEndian.Uint32(bom[:]) == 0x1a2b3c4d {
				bo = binary.LittleEndian
			} else {
				bo = binary.BigEndian
			}
			n := bo.Uint32(head[4:])
			if n < 16 || n > 1<<20 {
				return fmt.Errorf("pcapng: a section header of %d bytes", n)
			}
			if _, err := io.CopyN(io.Discard, r, int64(n)-12); err != nil {
				return err
			}
			links = links[:0]
			continue
		}
		n := bo.Uint32(head[4:])
		if n < 12 || n > 1<<24 {
			return fmt.Errorf("pcapng: a block of %d bytes", n)
		}
		if cap(buf) < int(n)-8 {
			buf = make([]byte, 0, n)
		}
		body := buf[:n-8]
		if _, err := io.ReadFull(r, body); err != nil {
			return err
		}
		body = body[:len(body)-4] // the length again
		switch typ {
		case 1: // interface description
			if len(body) >= 2 {
				links = append(links, uint32(bo.Uint16(body)))
			}
		case 6: // enhanced packet
			if len(body) < 20 {
				continue
			}
			iface, capLen := bo.Uint32(body), bo.Uint32(body[12:])
			if int(capLen) > len(body)-20 {
				continue
			}
			link := uint32(1)
			if int(iface) < len(links) {
				link = links[iface]
			}
			if p := udpPayload(body[20:20+capLen], link); p != nil {
				if err := f(p); err != nil {
					return err
				}
			}
		case 3: // simple packet
			if len(body) < 4 {
				continue
			}
			link := uint32(1)
			if len(links) > 0 {
				link = links[0]
			}
			if p := udpPayload(body[4:], link); p != nil {
				if err := f(p); err != nil {
					return err
				}
			}
		}
	}
}

// udpPayload is a captured frame's UDP payload (IPv4 or IPv6, over
// Ethernet with or without VLAN tags, raw IP, or Linux cooked capture);
// nil if it is something else.
func udpPayload(p []byte, link uint32) []byte {
	var et uint16
	switch link {
	case 1: // Ethernet
		if len(p) < 14 {
			return nil
		}
		et, p = binary.BigEndian.Uint16(p[12:]), p[14:]
		for (et == 0x8100 || et == 0x88a8) && len(p) >= 4 {
			et, p = binary.BigEndian.Uint16(p[2:]), p[4:]
		}
	case 101, 12, 14: // raw IP
		if len(p) == 0 {
			return nil
		}
		et = 0x0800
		if p[0]>>4 == 6 {
			et = 0x86dd
		}
	case 113: // Linux cooked
		if len(p) < 16 {
			return nil
		}
		et, p = binary.BigEndian.Uint16(p[14:]), p[16:]
	default:
		return nil
	}
	switch et {
	case 0x0800:
		if len(p) < 20 || p[0]>>4 != 4 || p[9] != 17 {
			return nil
		}
		ihl := int(p[0]&15) * 4
		if len(p) < ihl+8 || binary.BigEndian.Uint16(p[6:])&0x1fff != 0 {
			return nil // a fragment after the first
		}
		p = p[ihl:]
	case 0x86dd:
		if len(p) < 48 || p[6] != 17 {
			return nil
		}
		p = p[40:]
	default:
		return nil
	}
	ulen := int(binary.BigEndian.Uint16(p[4:]))
	if ulen < 8 || ulen > len(p) {
		ulen = len(p)
	}
	return p[8:ulen]
}
