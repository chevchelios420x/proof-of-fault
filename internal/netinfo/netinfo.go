// Package netinfo captures the network configuration of the measuring
// computer (addresses, gateway, DNS, routing and ARP table) so that reports
// document the measurement setup and can be compared.
package netinfo

import (
	"context"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// Snapshot is the network configuration at one point in time.
type Snapshot struct {
	TakenAt   int64    `json:"takenAt"` // unix ms
	Hostname  string   `json:"hostname"`
	OS        string   `json:"os"`
	LocalIP   string   `json:"localIp"`   // source address used towards the target
	Interface string   `json:"interface"` // network adapter of LocalIP
	MAC       string   `json:"mac"`
	MTU       int      `json:"mtu"`
	Prefix    string   `json:"prefix"` // e.g. 192.168.0.6/24
	Gateways  []string `json:"gateways"`
	DNS       []string `json:"dns"`
	Routes    string   `json:"routes"`   // raw routing table
	ARP       string   `json:"arp"`      // raw ARP / neighbour table
	IPConfig  string   `json:"ipconfig"` // raw adapter configuration
}

// command lines per OS: routing table, ARP table, adapter configuration.
var commands = map[string][3][]string{
	"windows": {{"route", "print", "-4"}, {"arp", "-a"}, {"ipconfig", "/all"}},
	"linux":   {{"ip", "-4", "route"}, {"ip", "neigh"}, {"ip", "addr"}},
	"darwin":  {{"netstat", "-rn", "-f", "inet"}, {"arp", "-an"}, {"ifconfig"}},
}

// Capture takes a snapshot. dst is the measurement target; it determines
// which local address/adapter is reported. Errors only leave fields empty.
func Capture(dst netip.Addr) Snapshot {
	s := Snapshot{TakenAt: time.Now().UnixMilli(), OS: runtime.GOOS + "/" + runtime.GOARCH}
	s.Hostname, _ = os.Hostname()

	// A UDP "connection" sends nothing but makes the OS pick the source
	// address and route towards dst.
	if c, err := net.Dial("udp", netip.AddrPortFrom(dst, 53).String()); err == nil {
		if ua, ok := c.LocalAddr().(*net.UDPAddr); ok {
			s.LocalIP = ua.IP.String()
		}
		c.Close()
	}
	if ifs, err := net.Interfaces(); err == nil {
		for _, ifc := range ifs {
			addrs, _ := ifc.Addrs()
			for _, a := range addrs {
				if ipn, ok := a.(*net.IPNet); ok && ipn.IP.String() == s.LocalIP {
					s.Interface, s.MAC, s.MTU, s.Prefix = ifc.Name, ifc.HardwareAddr.String(), ifc.MTU, ipn.String()
				}
			}
		}
	}

	cmds := commands[runtime.GOOS]
	if len(cmds[0]) > 0 {
		s.Routes = run(cmds[0])
		s.ARP = run(cmds[1])
		s.IPConfig = run(cmds[2])
	}
	s.Gateways = parseGateways(s.Routes)
	s.DNS = parseDNS(s.IPConfig)
	if len(s.DNS) == 0 {
		if b, err := os.ReadFile("/etc/resolv.conf"); err == nil {
			for _, l := range strings.Split(string(b), "\n") {
				if f := strings.Fields(l); len(f) == 2 && f[0] == "nameserver" {
					s.DNS = append(s.DNS, f[1])
				}
			}
		}
	}
	return s
}

func run(argv []string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return "(" + strings.Join(argv, " ") + ": " + err.Error() + ")"
	}
	return strings.TrimSpace(decode(out))
}

var ipv4 = regexp.MustCompile(`\b(\d{1,3}\.){3}\d{1,3}\b`)

// parseGateways finds default gateways in a routing table of any of the
// supported formats.
func parseGateways(routes string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(g string) {
		if _, err := netip.ParseAddr(g); err == nil && !seen[g] {
			seen[g] = true
			out = append(out, g)
		}
	}
	for _, l := range strings.Split(routes, "\n") {
		f := strings.Fields(l)
		switch {
		case len(f) >= 3 && f[0] == "0.0.0.0" && f[1] == "0.0.0.0": // Windows
			add(f[2])
		case len(f) >= 3 && f[0] == "default" && f[1] == "via": // Linux
			add(f[2])
		case len(f) >= 2 && f[0] == "default": // macOS
			add(f[1])
		}
	}
	return out
}

// parseDNS collects DNS servers from "ipconfig /all" (English or German):
// the line naming the DNS servers plus following lines that hold only an
// address.
func parseDNS(ipconfig string) []string {
	var out []string
	seen := map[string]bool{}
	in := false
	for _, l := range strings.Split(ipconfig, "\n") {
		t := strings.TrimSpace(l)
		low := strings.ToLower(t)
		switch {
		case strings.Contains(low, "dns-server") || strings.Contains(low, "dns servers"):
			in = true
			if i := strings.Index(t, ":"); i >= 0 {
				t = t[i+1:]
			}
		case in && ipv4.MatchString(t) && !strings.Contains(t, ":"):
		default:
			in = false
			continue
		}
		for _, ip := range ipv4.FindAllString(t, -1) {
			if !seen[ip] {
				seen[ip] = true
				out = append(out, ip)
			}
		}
	}
	return out
}
