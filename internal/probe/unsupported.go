//go:build !windows

package probe

// Linux (SOCK_DGRAM ICMP + IP_RECVERR) and macOS (SOCK_DGRAM ICMP) follow.
func newProber() (Prober, error) { return nil, ErrUnsupported }
