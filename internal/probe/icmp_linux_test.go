//go:build linux

package probe

import (
	"context"
	"net/netip"
	"testing"
	"time"
)

func TestLinuxLoopbackEcho(t *testing.T) {
	p, err := New()
	if err != nil {
		t.Skip(err)
	}
	res, err := p.Probe(context.Background(), Request{Dst: netip.MustParseAddr("127.0.0.1"), Timeout: time.Second})
	if err != nil || res.Kind != EchoReply {
		t.Fatalf("got %v %v", res, err)
	}
	// TTL 1 to a remote address: the first router answers "time exceeded"
	// (or nothing in a sandbox without network) – must not hang or panic.
	p.Probe(context.Background(), Request{Dst: netip.MustParseAddr("192.0.2.1"), TTL: 1, Timeout: 300 * time.Millisecond})
}
