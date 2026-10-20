package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// agentConfig is the configuration of the background service (agent.json
// next to the database).
type agentConfig struct {
	Listen string `json:"listen"` // address of the web server
	TLS    bool   `json:"tls"`    // HTTPS with a self-signed certificate
	Token  string `json:"token"`  // access key (UUID)
}

// URL returns the address of the web interface, optionally with the access
// key (opens logged in).
func (c *agentConfig) URL(token string) string {
	scheme := "http"
	if c.TLS {
		scheme = "https"
	}
	host, port, _ := net.SplitHostPort(c.Listen)
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	u := scheme + "://" + net.JoinHostPort(host, port) + "/"
	if token != "" {
		u += "#key=" + token
	}
	return u
}

// LANURLs returns the addresses under which other devices reach the agent.
func (c *agentConfig) LANURLs() []string {
	host, port, _ := net.SplitHostPort(c.Listen)
	if host != "" && host != "0.0.0.0" && host != "::" {
		return nil
	}
	scheme := "http"
	if c.TLS {
		scheme = "https"
	}
	var out []string
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil && !ipn.IP.IsLoopback() {
			out = append(out, scheme+"://"+net.JoinHostPort(ipn.IP.String(), port)+"/")
		}
	}
	return out
}

func dataDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "proof-of-fault")
	return dir, os.MkdirAll(dir, 0o700)
}

// loadAgentConfig reads agent.json and creates it with defaults (and a new
// access key) on the first start. Windows listens only locally (the browser
// runs on the same PC), Linux on all addresses with HTTPS (VM / server).
func loadAgentConfig(dir string) (*agentConfig, error) {
	p := filepath.Join(dir, "agent.json")
	c := &agentConfig{Listen: "0.0.0.0:8443", TLS: true}
	if runtime.GOOS == "windows" {
		c = &agentConfig{Listen: "127.0.0.1:8443", TLS: false}
	}
	b, err := os.ReadFile(p)
	if err == nil {
		if err := json.Unmarshal(b, c); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
	}
	if c.Token == "" {
		c.Token = newUUID()
		if err := c.save(dir); err != nil {
			return nil, err
		}
	}
	return c, nil
}

func (c *agentConfig) save(dir string) error {
	b, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(filepath.Join(dir, "agent.json"), b, 0o600)
}

// newUUID returns a random UUID (version 4).
func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// ensureCert returns certificate and key files for HTTPS and creates a
// self-signed certificate (10 years) for this host and its addresses on
// the first start.
func ensureCert(dir string) (string, string, error) {
	cert, key := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if _, err := os.Stat(cert); err == nil {
		if _, err := os.Stat(key); err == nil {
			return cert, key, nil
		}
	}
	pk, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	host, _ := os.Hostname()
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "proof-of-fault " + host, Organization: []string{"proof-of-fault"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1)},
	}
	if host != "" {
		tmpl.DNSNames = append(tmpl.DNSNames, host)
	}
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLoopback() {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ipn.IP)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &pk.PublicKey, pk)
	if err != nil {
		return "", "", err
	}
	kb, err := x509.MarshalECPrivateKey(pk)
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(key, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), 0o600); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return "", "", err
	}
	return cert, key, nil
}
