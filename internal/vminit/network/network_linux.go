package network

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"

	"golang.org/x/sys/unix"
)

// Configure gives the interface name the address addr, brings it up, and routes every address it
// does not reach directly through gw. It fails when the VM has no such interface.
func Configure(name string, addr netip.Prefix, gw netip.Addr) error {
	index, err := interfaceIndex(name)
	if err != nil {
		return err
	}
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC, unix.NETLINK_ROUTE)
	if err != nil {
		return fmt.Errorf("open a netlink socket: %w", err)
	}
	defer func() { _ = unix.Close(fd) }()
	for _, m := range []struct {
		what string
		msg  []byte
	}{
		{"give " + name + " the address " + addr.String(), addrMessage(1, index, addr)},
		{"bring " + name + " up", linkUpMessage(2, index)},
		{"route through " + gw.String(), routeMessage(3, index, gw)},
	} {
		if err := request(fd, m.msg); err != nil {
			return fmt.Errorf("%s: %w", m.what, err)
		}
	}
	return nil
}

// interfaceIndex returns the index of the interface name.
func interfaceIndex(name string) (int32, error) {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return 0, fmt.Errorf("find %s: %w", name, err)
	}
	defer func() { _ = unix.Close(fd) }()
	ifr, err := unix.NewIfreq(name)
	if err != nil {
		return 0, fmt.Errorf("find %s: %w", name, err)
	}
	if err := unix.IoctlIfreq(fd, unix.SIOCGIFINDEX, ifr); errors.Is(err, unix.ENODEV) {
		return 0, fmt.Errorf("the VM has no %s, the network card cove attaches", name)
	} else if err != nil {
		return 0, fmt.Errorf("find %s: %w", name, err)
	}
	return int32(ifr.Uint32()), nil //nolint:gosec // G115: the kernel gives an index as an int.
}

// request sends msg to the kernel and reads its acknowledgement: the error it carries, or nil.
func request(fd int, msg []byte) error {
	if err := unix.Sendto(fd, msg, 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return fmt.Errorf("send: %w", err)
	}
	buf := make([]byte, unix.Getpagesize())
	n, _, err := unix.Recvfrom(fd, buf, 0)
	if err != nil {
		return fmt.Errorf("receive: %w", err)
	}
	return ackError(buf[:n])
}

// ackError returns the error the acknowledgement ack carries, nil for none.
func ackError(ack []byte) error {
	if len(ack) < unix.SizeofNlMsghdr+4 {
		return fmt.Errorf("an answer of %d bytes", len(ack))
	}
	if typ := binary.NativeEndian.Uint16(ack[4:6]); typ != unix.NLMSG_ERROR {
		return fmt.Errorf("an answer of type %d, not an acknowledgement", typ)
	}
	// The error is negated, zero for success.
	//nolint:gosec // G115: the field is signed.
	if errno := -int32(binary.NativeEndian.Uint32(ack[unix.SizeofNlMsghdr:])); errno != 0 {
		return unix.Errno(errno) //nolint:gosec // G115: an errno is positive.
	}
	return nil
}

// createFlags ask the kernel to create what a message names, to refuse it when it exists, and to
// acknowledge it.
const createFlags = unix.NLM_F_REQUEST | unix.NLM_F_ACK | unix.NLM_F_CREATE | unix.NLM_F_EXCL

// addrMessage returns the RTM_NEWADDR that gives the interface index the address addr.
func addrMessage(seq uint32, index int32, addr netip.Prefix) []byte {
	b := header(unix.RTM_NEWADDR, createFlags, seq)
	b = append(b, unix.AF_INET, byte(addr.Bits()), 0, unix.RT_SCOPE_UNIVERSE) //nolint:gosec // G115: 32 bits at most.
	b = binary.NativeEndian.AppendUint32(b, uint32(index))                    //nolint:gosec // G115: an index is positive.
	ip := addr.Addr().As4()
	b = attr(b, unix.IFA_LOCAL, ip[:])
	b = attr(b, unix.IFA_ADDRESS, ip[:])
	return sized(b)
}

// linkUpMessage returns the RTM_NEWLINK that brings the interface index up.
func linkUpMessage(seq uint32, index int32) []byte {
	b := header(unix.RTM_NEWLINK, unix.NLM_F_REQUEST|unix.NLM_F_ACK, seq)
	b = append(b, unix.AF_UNSPEC, 0)
	b = binary.NativeEndian.AppendUint16(b, 0)
	b = binary.NativeEndian.AppendUint32(b, uint32(index)) //nolint:gosec // G115: an index is positive.
	// The flags, then the mask of the flags changed.
	b = binary.NativeEndian.AppendUint32(b, unix.IFF_UP)
	b = binary.NativeEndian.AppendUint32(b, unix.IFF_UP)
	return sized(b)
}

// routeMessage returns the RTM_NEWROUTE of the default route, through gw on the interface index.
func routeMessage(seq uint32, index int32, gw netip.Addr) []byte {
	b := header(unix.RTM_NEWROUTE, createFlags, seq)
	// The family, the lengths of the destination and the source, the type of service, the table,
	// the protocol, the scope, the type, and the flags.
	b = append(b, unix.AF_INET, 0, 0, 0, unix.RT_TABLE_MAIN, unix.RTPROT_BOOT, unix.RT_SCOPE_UNIVERSE, unix.RTN_UNICAST)
	b = binary.NativeEndian.AppendUint32(b, 0)
	ip := gw.As4()
	b = attr(b, unix.RTA_GATEWAY, ip[:])
	b = attr(b, unix.RTA_OIF, binary.NativeEndian.AppendUint32(nil, uint32(index))) //nolint:gosec // G115: positive.
	return sized(b)
}

// header returns the header of a message of type typ, its length left to sized.
func header(typ, flags uint16, seq uint32) []byte {
	b := make([]byte, 4, unix.SizeofNlMsghdr)
	b = binary.NativeEndian.AppendUint16(b, typ)
	b = binary.NativeEndian.AppendUint16(b, flags)
	b = binary.NativeEndian.AppendUint32(b, seq)
	// The port of the sender: zero lets the kernel take the one of the socket.
	return binary.NativeEndian.AppendUint32(b, 0)
}

// attr appends to b the attribute typ holding data, padded to 4 bytes.
func attr(b []byte, typ uint16, data []byte) []byte {
	b = binary.NativeEndian.AppendUint16(b, uint16(unix.SizeofRtAttr+len(data))) //nolint:gosec // G115: a few bytes.
	b = binary.NativeEndian.AppendUint16(b, typ)
	b = append(b, data...)
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

// sized writes the length of the message b in its header.
func sized(b []byte) []byte {
	binary.NativeEndian.PutUint32(b[0:4], uint32(len(b))) //nolint:gosec // G115: a few bytes.
	return b
}
