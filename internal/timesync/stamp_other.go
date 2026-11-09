//go:build !linux

package timesync

import (
	"errors"
	"net"
	"time"

	"golang.org/x/net/ipv4"
)

// EnableTimestamps: packets are stamped by the kernel on Linux only;
// elsewhere they are stamped as they are read and written.
func EnableTimestamps(c *net.UDPConn, hardware bool, iface string) error {
	if hardware {
		return errors.New("hardware timestamps are Linux's only")
	}
	return nil
}

func rxStamp([]byte) (time.Time, bool) { return time.Time{}, false }

func txStamp(*net.UDPConn) (time.Time, error) { return time.Time{}, errors.New("no transmit stamps") }

func joinGroup(c *net.UDPConn, iface *net.Interface, group net.IP) error {
	p := ipv4.NewPacketConn(c)
	if err := p.JoinGroup(iface, &net.UDPAddr{IP: group}); err != nil {
		return err
	}
	return p.SetMulticastInterface(iface)
}
