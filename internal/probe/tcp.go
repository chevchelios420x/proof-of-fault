package probe

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"syscall"
	"time"
)

// TCPConnect measures the time of a TCP handshake to dst:port (about one
// round trip) and closes the connection immediately. A refused connection
// (RST) also proves the host is reachable and counts as answer. Needs no
// special privileges on any platform.
func TCPConnect(ctx context.Context, dst netip.Addr, port int, timeout time.Duration) Result {
	d := net.Dialer{Timeout: timeout}
	sent := time.Now()
	c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(dst.String(), strconv.Itoa(port)))
	rtt := time.Since(sent)
	res := Result{Sent: sent}
	switch {
	case err == nil:
		c.Close()
		res.Kind, res.RTT, res.From = EchoReply, rtt, dst
	case errors.Is(err, syscall.ECONNREFUSED):
		res.Kind, res.RTT, res.From = EchoReply, rtt, dst
	default:
		res.Kind = Timeout
	}
	return res
}
