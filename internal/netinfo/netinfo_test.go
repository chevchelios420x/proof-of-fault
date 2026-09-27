package netinfo

import (
	"reflect"
	"testing"
)

func TestParseGateways(t *testing.T) {
	win := `IPv4-Routentabelle
===========================================================================
Aktive Routen:
     Netzwerkziel    Netzwerkmaske          Gateway    Schnittstelle Metrik
          0.0.0.0          0.0.0.0      192.168.0.1      192.168.0.6     25
      192.168.0.0    255.255.255.0   Auf Verbindung      192.168.0.6    281`
	if g := parseGateways(win); !reflect.DeepEqual(g, []string{"192.168.0.1"}) {
		t.Errorf("windows: %v", g)
	}
	if g := parseGateways("default via 10.0.0.1 dev eth0\n10.0.0.0/24 dev eth0"); !reflect.DeepEqual(g, []string{"10.0.0.1"}) {
		t.Errorf("linux: %v", g)
	}
}

func TestParseDNS(t *testing.T) {
	de := `   Standardgateway . . . . . . . . . : 192.168.0.1
   DNS-Server  . . . . . . . . . . . : 192.168.0.1
                                       1.1.1.1
   NetBIOS über TCP/IP . . . . . . . : Aktiviert`
	if d := parseDNS(de); !reflect.DeepEqual(d, []string{"192.168.0.1", "1.1.1.1"}) {
		t.Errorf("german: %v", d)
	}
	en := `   DNS Servers . . . . . . . . . . . : 192.168.0.1
   NetBIOS over Tcpip. . . . . . . . : Enabled`
	if d := parseDNS(en); !reflect.DeepEqual(d, []string{"192.168.0.1"}) {
		t.Errorf("english: %v", d)
	}
}
