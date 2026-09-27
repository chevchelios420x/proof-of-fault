//go:build windows

package probe

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The Windows ICMP helper API (iphlpapi.dll) sends echo requests without
// raw sockets and therefore without administrator rights; tracert.exe uses
// the same API.
var (
	iphlpapi        = windows.NewLazySystemDLL("iphlpapi.dll")
	procCreateFile  = iphlpapi.NewProc("IcmpCreateFile")
	procCloseHandle = iphlpapi.NewProc("IcmpCloseHandle")
	procSendEcho2   = iphlpapi.NewProc("IcmpSendEcho2")
)

// IP_STATUS codes from ipexport.h.
const (
	ipSuccess             = 0
	ipDestNetUnreachable  = 11002
	ipDestHostUnreachable = 11003
	ipDestProtUnreachable = 11004
	ipDestPortUnreachable = 11005
	ipReqTimedOut         = 11010
	ipTTLExpiredTransit   = 11013
	ipTTLExpiredReassem   = 11014
)

// ipOptionInformation mirrors IP_OPTION_INFORMATION; Go's natural alignment
// matches the C layout on both 386 and amd64/arm64.
type ipOptionInformation struct {
	TTL         uint8
	Tos         uint8
	Flags       uint8
	OptionsSize uint8
	OptionsData uintptr
}

// icmpEchoReply mirrors ICMP_ECHO_REPLY.
type icmpEchoReply struct {
	Address       [4]byte // network byte order
	Status        uint32
	RoundTripTime uint32 // milliseconds, too coarse; we measure ourselves
	DataSize      uint16
	Reserved      uint16
	Data          uintptr
	Options       ipOptionInformation
}

var payload = []byte("proof-of-fault--probe-payload-32")

type windowsProber struct{}

func newProber() (Prober, error) {
	if err := procSendEcho2.Find(); err != nil {
		return nil, fmt.Errorf("iphlpapi.dll: %w", err)
	}
	return windowsProber{}, nil
}

func (windowsProber) Close() error { return nil }

func (windowsProber) Probe(ctx context.Context, req Request) (Result, error) {
	if !req.Dst.Is4() {
		return Result{}, errors.New("probe: only IPv4 is supported on Windows yet")
	}
	timeout := req.Timeout
	if dl, ok := ctx.Deadline(); ok && time.Until(dl) < timeout {
		timeout = time.Until(dl)
	}
	if timeout <= 0 {
		return Result{Kind: Timeout, Sent: time.Now()}, ctx.Err()
	}

	// One handle per probe: cheap, and avoids any doubt about concurrent use.
	h, _, err := procCreateFile.Call()
	if windows.Handle(h) == windows.InvalidHandle {
		return Result{}, fmt.Errorf("IcmpCreateFile: %w", err)
	}
	defer procCloseHandle.Call(h)

	var opts ipOptionInformation
	var optsPtr uintptr
	if req.TTL > 0 {
		opts.TTL = uint8(req.TTL)
		optsPtr = uintptr(unsafe.Pointer(&opts))
	}

	// Reply buffer: one reply + payload + 8 bytes ICMP error + slack.
	buf := make([]byte, int(unsafe.Sizeof(icmpEchoReply{}))+len(payload)+8+256)
	dst := req.Dst.As4()
	dstAddr := *(*uint32)(unsafe.Pointer(&dst))

	sent := time.Now()
	n, _, callErr := procSendEcho2.Call(
		h,
		0, 0, 0, // no event / APC: synchronous
		uintptr(dstAddr),
		uintptr(unsafe.Pointer(&payload[0])),
		uintptr(len(payload)),
		optsPtr,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
		uintptr(timeout.Milliseconds()),
	)
	// Go's time uses QueryPerformanceCounter: sub-ms resolution, unlike
	// RoundTripTime which is whole milliseconds.
	rtt := time.Since(sent)
	res := Result{Sent: sent}

	reply := (*icmpEchoReply)(unsafe.Pointer(&buf[0]))
	if n == 0 {
		// Depending on the Windows version, ICMP errors (TTL expired,
		// unreachable) are reported via GetLastError; the reply buffer may
		// still carry the responder's address.
		errno, _ := callErr.(windows.Errno)
		k, ok := classify(uint32(errno))
		if !ok || k == Timeout {
			res.Kind = Timeout
			return res, nil
		}
		res.Kind, res.RTT = k, rtt
		if reply.Address != [4]byte{} {
			res.From = netip.AddrFrom4(reply.Address)
		}
		return res, nil
	}

	kind, ok := classify(reply.Status)
	if !ok {
		res.Kind = Timeout
		return res, nil
	}
	res.Kind = kind
	if kind != Timeout {
		res.RTT = rtt
		res.From = netip.AddrFrom4(reply.Address)
	}
	return res, nil
}

func classify(status uint32) (ReplyKind, bool) {
	switch status {
	case ipSuccess:
		return EchoReply, true
	case ipTTLExpiredTransit, ipTTLExpiredReassem:
		return TimeExceeded, true
	case ipDestNetUnreachable, ipDestHostUnreachable, ipDestProtUnreachable, ipDestPortUnreachable:
		return Unreachable, true
	case ipReqTimedOut:
		return Timeout, true
	}
	return Timeout, false
}
