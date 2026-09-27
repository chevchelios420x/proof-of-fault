package probe

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"
)

func TestTCPConnect(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	lo := netip.MustParseAddr("127.0.0.1")
	if r := TCPConnect(context.Background(), lo, port, time.Second); r.Kind != EchoReply {
		t.Fatalf("open port: %v", r.Kind)
	}
	l.Close()
	if r := TCPConnect(context.Background(), lo, port, time.Second); r.Kind != EchoReply {
		t.Fatalf("refused port should count as reachable: %v", r.Kind)
	}
}
