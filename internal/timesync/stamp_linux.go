package timesync

import (
	"encoding/binary"
	"errors"
	"net"
	"time"

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
		flags := unix.SOF_TIMESTAMPING_TX_SOFTWARE | unix.SOF_TIMESTAMPING_RX_SOFTWARE | unix.SOF_TIMESTAMPING_SOFTWARE
		if hardware {
			if stamping(int(fd))&unix.SOF_TIMESTAMPING_TX_HARDWARE != 0 {
				return // on already: as root, before
			}
			cfg := &unix.HwTstampConfig{Tx_type: unix.HWTSTAMP_TX_ON, Rx_filter: unix.HWTSTAMP_FILTER_ALL}
			if serr = unix.IoctlSetHwTstamp(int(fd), iface, cfg); serr != nil {
				// some cards stamp PTP's event packets alone
				cfg.Rx_filter = unix.HWTSTAMP_FILTER_PTP_V2_EVENT
				if serr = unix.IoctlSetHwTstamp(int(fd), iface, cfg); serr != nil {
					return
				}
			}
			flags = unix.SOF_TIMESTAMPING_TX_HARDWARE | unix.SOF_TIMESTAMPING_RX_HARDWARE | unix.SOF_TIMESTAMPING_RAW_HARDWARE
		}
		flags |= unix.SOF_TIMESTAMPING_OPT_TSONLY
		// the 64-bit stamps (Linux 5.1), else the old ones
		if serr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_TIMESTAMPING_NEW, flags); serr != nil {
			serr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_TIMESTAMPING_OLD, flags)
		}
	})
	if err != nil {
		return err
	}
	return serr
}

// stamping is the socket's SO_TIMESTAMPING flags.
func stamping(fd int) int {
	if v, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_TIMESTAMPING_NEW); err == nil {
		return v
	}
	v, _ := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_TIMESTAMPING_OLD)
	return v
}

// stampOf is the time in a SO_TIMESTAMPING control message: of its three
// stamps, the software one or else the network card's.
func stampOf(oob []byte) (time.Time, bool) {
	msgs, err := unix.ParseSocketControlMessage(oob)
	if err != nil {
		return time.Time{}, false
	}
	for _, m := range msgs {
		if m.Header.Level != unix.SOL_SOCKET {
			continue
		}
		var ts [3]time.Time
		switch m.Header.Type {
		case unix.SO_TIMESTAMPING_NEW: // struct __kernel_timespec: two int64s
			if len(m.Data) < 48 {
				continue
			}
			for i := range ts {
				s := int64(binary.NativeEndian.Uint64(m.Data[i*16:]))
				ns := int64(binary.NativeEndian.Uint64(m.Data[i*16+8:]))
				ts[i] = time.Unix(s, ns)
			}
		case unix.SO_TIMESTAMPING_OLD: // struct timespec: two longs
			n := len(m.Data) / 6
			if n != 4 && n != 8 {
				continue
			}
			for i := range ts {
				s, ns := word(m.Data[2*i*n:], n), word(m.Data[(2*i+1)*n:], n)
				ts[i] = time.Unix(s, ns)
			}
		default:
			continue
		}
		for _, t := range []time.Time{ts[0], ts[2]} {
			if t.Unix() != 0 || t.Nanosecond() != 0 {
				return t, true
			}
		}
	}
	return time.Time{}, false
}

func word(b []byte, n int) int64 {
	if n == 4 {
		return int64(int32(binary.NativeEndian.Uint32(b)))
	}
	return int64(binary.NativeEndian.Uint64(b))
}

// rxStamp is a received packet's stamp from its control messages.
func rxStamp(oob []byte) (time.Time, bool) { return stampOf(oob) }

// txWait is how long the stamp of a packet sent is waited for.
const txWait = 10 * time.Millisecond

// txStamp is the stamp of the packet c sent last: read from the socket's
// error queue, which is emptied (a stamp late from before is not taken
// for this one's).
func txStamp(c *net.UDPConn) (time.Time, error) {
	rc, err := c.SyscallConn()
	if err != nil {
		return time.Time{}, err
	}
	var t time.Time
	found := false
	err = rc.Control(func(fd uintptr) {
		oob := make([]byte, 256)
		deadline := time.Now().Add(txWait)
		for {
			_, oobn, _, _, err := unix.Recvmsg(int(fd), nil, oob, unix.MSG_ERRQUEUE|unix.MSG_DONTWAIT)
			if err == nil {
				if st, ok := stampOf(oob[:oobn]); ok {
					t, found = st, true
				}
				continue
			}
			if found || !errors.Is(err, unix.EAGAIN) {
				return
			}
			left := time.Until(deadline)
			if left <= 0 {
				return
			}
			to := unix.NsecToTimespec(left.Nanoseconds())
			unix.Ppoll([]unix.PollFd{{Fd: int32(fd), Events: unix.POLLERR}}, &to, nil)
		}
	})
	if err != nil {
		return time.Time{}, err
	}
	if !found {
		return time.Time{}, errors.New("no transmit stamp")
	}
	return t, nil
}

// joinGroup has c take a multicast group's packets on iface, and send
// multicast there.
func joinGroup(c *net.UDPConn, iface *net.Interface, group net.IP) error {
	rc, err := c.SyscallConn()
	if err != nil {
		return err
	}
	var serr error
	if err := rc.Control(func(fd uintptr) {
		mreq := &unix.IPMreqn{Ifindex: int32(iface.Index)}
		copy(mreq.Multiaddr[:], group.To4())
		if serr = unix.SetsockoptIPMreqn(int(fd), unix.IPPROTO_IP, unix.IP_ADD_MEMBERSHIP, mreq); serr != nil {
			return
		}
		serr = unix.SetsockoptIPMreqn(int(fd), unix.IPPROTO_IP, unix.IP_MULTICAST_IF, &unix.IPMreqn{Ifindex: int32(iface.Index)})
	}); err != nil {
		return err
	}
	return serr
}
