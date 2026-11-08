package timesync

import (
	"errors"
	"net"
	"time"

	"github.com/facebook/time/timestamp"
	"golang.org/x/sys/unix"
)

// EnableTimestamps has the kernel (software) or the network card
// (hardware: iface's, which needs CAP_NET_ADMIN: done as root, before the
// BBS gives it up) stamp the packets c sends and receives.
func EnableTimestamps(c *net.UDPConn, hardware bool, iface string) error {
	rc, err := c.SyscallConn()
	if err != nil {
		return err
	}
	var serr error
	err = rc.Control(func(fd uintptr) {
		if !hardware {
			serr = timestamp.EnableSWTimestamps(int(fd))
			return
		}
		if already(int(fd)) {
			return // as root, before
		}
		ifc, err := net.InterfaceByName(iface)
		if err != nil {
			serr = err
			return
		}
		serr = timestamp.EnableHWTimestamps(int(fd), ifc)
	})
	if err != nil {
		return err
	}
	return serr
}

// already: hardware stamps are on (set as root, the socket handed down).
func already(fd int) bool {
	v, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_TIMESTAMPING_NEW)
	if err != nil {
		v, err = unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_TIMESTAMPING)
	}
	return err == nil && v&unix.SOF_TIMESTAMPING_TX_HARDWARE != 0
}

// rxStamp is a received packet's stamp from its control messages.
func rxStamp(oob []byte) (time.Time, bool) {
	t, err := timestamp.ReadRXTimestamp(oob, len(oob))
	return t, err == nil && !t.IsZero()
}

// txStamp is the stamp of the packet c sent last.
func txStamp(c *net.UDPConn) (time.Time, error) {
	rc, err := c.SyscallConn()
	if err != nil {
		return time.Time{}, err
	}
	var t time.Time
	var serr error
	if err := rc.Control(func(fd uintptr) { t, _, serr = timestamp.ReadTXtimestamp(int(fd)) }); err != nil {
		return time.Time{}, err
	}
	if serr == nil && t.IsZero() {
		serr = errors.New("no transmit stamp")
	}
	return t, serr
}

// joinGroup has c take a multicast group's packets on iface.
func joinGroup(c *net.UDPConn, iface *net.Interface, group net.IP) error {
	rc, err := c.SyscallConn()
	if err != nil {
		return err
	}
	var serr error
	if err := rc.Control(func(fd uintptr) {
		if serr = timestamp.JoinMulticast(int(fd), iface, group); serr != nil {
			return
		}
		mreq := &unix.IPMreqn{Ifindex: int32(iface.Index)}
		serr = unix.SetsockoptIPMreqn(int(fd), unix.IPPROTO_IP, unix.IP_MULTICAST_IF, mreq)
	}); err != nil {
		return err
	}
	return serr
}
