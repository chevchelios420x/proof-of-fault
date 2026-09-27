// Package probe sends single network probes (ICMP echo, optionally TTL-limited)
// without administrator privileges. Each operating system has its own
// implementation behind the Prober interface.
package probe

import (
	"context"
	"errors"
	"net/netip"
	"time"
)

// ReplyKind classifies the answer to a probe.
type ReplyKind int

const (
	Timeout      ReplyKind = iota // no answer within the timeout
	EchoReply                     // destination answered
	TimeExceeded                  // an intermediate hop answered (TTL expired)
	Unreachable                   // destination/network unreachable
)

func (k ReplyKind) String() string {
	switch k {
	case EchoReply:
		return "echo"
	case TimeExceeded:
		return "ttl-exceeded"
	case Unreachable:
		return "unreachable"
	default:
		return "timeout"
	}
}

// Request describes a single probe.
type Request struct {
	Dst     netip.Addr
	TTL     int // 0 = system default
	Timeout time.Duration
}

// Result is the outcome of a single probe. RTT and From are only valid when
// Kind != Timeout.
type Result struct {
	Sent time.Time
	RTT  time.Duration
	From netip.Addr
	Kind ReplyKind
}

// Prober sends probes. Implementations must be safe for concurrent use.
type Prober interface {
	Probe(ctx context.Context, req Request) (Result, error)
	Close() error
}

// ErrUnsupported is returned by New on platforms without an implementation yet.
var ErrUnsupported = errors.New("probe: platform not supported yet")

// New returns the best unprivileged prober for the current platform.
func New() (Prober, error) { return newProber() }
