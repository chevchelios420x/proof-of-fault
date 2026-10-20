//go:build linux

package probe

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strings"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Linux allows ICMP echo without root through "ping sockets"
// (SOCK_DGRAM + IPPROTO_ICMP) for all groups listed in
// net.ipv4.ping_group_range; Ubuntu permits every group by default. The
// kernel sets the ICMP identifier per socket and delivers only matching
// replies. ICMP errors (TTL expired, unreachable) arrive on the socket's
// error queue when IP_RECVERR is enabled.

type linuxProber struct{}

var seq atomic.Uint32

func newProber() (Prober, error) {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, unix.IPPROTO_ICMP)
	if err != nil {
		rng, _ := os.ReadFile("/proc/sys/net/ipv4/ping_group_range")
		return nil, fmt.Errorf("ICMP ohne root nicht erlaubt (net.ipv4.ping_group_range = %s): einmalig als Administrator "+
			"`sudo sysctl -w net.ipv4.ping_group_range=\"0 2147483647\"` ausführen und in /etc/sysctl.d/ dauerhaft eintragen: %w",
			strings.TrimSpace(string(rng)), err)
	}
	unix.Close(fd)
	return linuxProber{}, nil
}

func (linuxProber) Close() error { return nil }

func (linuxProber) Probe(ctx context.Context, req Request) (Result, error) {
	if !req.Dst.Is4() {
		return Result{}, errors.New("probe: only IPv4 is supported yet")
	}
	timeout := req.Timeout
	if dl, ok := ctx.Deadline(); ok && time.Until(dl) < timeout {
		timeout = time.Until(dl)
	}
	if timeout <= 0 {
		return Result{Kind: Timeout, Sent: time.Now()}, ctx.Err()
	}
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, unix.IPPROTO_ICMP)
	if err != nil {
		return Result{}, fmt.Errorf("ping socket: %w", err)
	}
	defer unix.Close(fd)
	if req.TTL > 0 {
		if err := unix.SetsockoptInt(fd, unix.IPPROTO_IP, unix.IP_TTL, req.TTL); err != nil {
			return Result{}, err
		}
	}
	if err := unix.SetsockoptInt(fd, unix.IPPROTO_IP, unix.IP_RECVERR, 1); err != nil {
		return Result{}, err
	}

	sq := uint16(seq.Add(1))
	pkt := make([]byte, 8+len(payload))
	pkt[0] = 8 // echo request; identifier and checksum are set by the kernel
	binary.BigEndian.PutUint16(pkt[6:], sq)
	copy(pkt[8:], payload)
	sa := &unix.SockaddrInet4{Addr: req.Dst.As4()}

	sent := time.Now()
	if err := unix.Sendto(fd, pkt, 0, sa); err != nil {
		// Immediate errors (no route) count as unreachable.
		return Result{Sent: sent, Kind: Unreachable, RTT: time.Since(sent)}, nil
	}
	deadline := sent.Add(timeout)
	buf := make([]byte, 1500)
	oob := make([]byte, 512)
	for {
		left := time.Until(deadline)
		if left <= 0 || ctx.Err() != nil {
			return Result{Sent: sent, Kind: Timeout}, nil
		}
		pfd := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		ms := int(left / time.Millisecond)
		if ms > 200 {
			ms = 200 // wake up regularly to notice ctx cancellation
		}
		n, err := unix.Poll(pfd, ms)
		if err != nil && err != unix.EINTR {
			return Result{}, err
		}
		if n <= 0 {
			continue
		}
		rtt := time.Since(sent)
		if pfd[0].Revents&unix.POLLERR != 0 {
			_, oobn, _, _, err := unix.Recvmsg(fd, buf, oob, unix.MSG_ERRQUEUE|unix.MSG_DONTWAIT)
			if err != nil {
				continue
			}
			if kind, from, ok := parseErrQueue(oob[:oobn]); ok {
				return Result{Sent: sent, RTT: rtt, Kind: kind, From: from}, nil
			}
			continue
		}
		if pfd[0].Revents&unix.POLLIN != 0 {
			m, from, err := unix.Recvfrom(fd, buf, unix.MSG_DONTWAIT)
			if err != nil || m < 8 || buf[0] != 0 || binary.BigEndian.Uint16(buf[6:]) != sq {
				continue // not our reply
			}
			res := Result{Sent: sent, RTT: rtt, Kind: EchoReply, From: req.Dst}
			if s4, ok := from.(*unix.SockaddrInet4); ok {
				res.From = netip.AddrFrom4(s4.Addr)
			}
			return res, nil
		}
	}
}

// parseErrQueue reads the sock_extended_err of an ICMP error.
func parseErrQueue(oob []byte) (ReplyKind, netip.Addr, bool) {
	msgs, err := unix.ParseSocketControlMessage(oob)
	if err != nil {
		return 0, netip.Addr{}, false
	}
	for _, m := range msgs {
		if m.Header.Level != unix.IPPROTO_IP || m.Header.Type != unix.IP_RECVERR {
			continue
		}
		size := int(unsafe.Sizeof(unix.SockExtendedErr{}))
		if len(m.Data) < size {
			continue
		}
		ee := (*unix.SockExtendedErr)(unsafe.Pointer(&m.Data[0]))
		if ee.Origin != unix.SO_EE_ORIGIN_ICMP {
			continue
		}
		var from netip.Addr
		// The offending address (sockaddr_in) follows the struct.
		if len(m.Data) >= size+8 && binary.LittleEndian.Uint16(m.Data[size:]) == unix.AF_INET {
			from = netip.AddrFrom4([4]byte(m.Data[size+4 : size+8]))
		}
		switch ee.Type {
		case 11: // time exceeded
			return TimeExceeded, from, true
		case 3: // destination unreachable
			return Unreachable, from, true
		}
	}
	return 0, netip.Addr{}, false
}
