package main

import (
	"context"
	"crypto/tls"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

//go:embed all:frontend/dist
var assets embed.FS

const usage = `proof-of-fault – Nachweis von Internet-Störungen

Aufruf: proof-of-fault [Befehl] [Optionen]

Ohne Befehl (Doppelklick): richtet beim ersten Start den Hintergrunddienst
ein (Autostart, ohne Administratorrechte), startet ihn und öffnet die
Weboberfläche.

Befehle:
  run         Dienst im Vordergrund ausführen (wird vom Autostart benutzt)
  install     Dienst einrichten/aktualisieren und starten
  uninstall   Dienst stoppen und Autostart entfernen (Daten bleiben)
  status      Adresse, Zugangsschlüssel und Zustand anzeigen
  new-key     neuen Zugangsschlüssel erzeugen (alle Browser abmelden)

Optionen für install/run:
  -listen ADRESSE   z. B. 0.0.0.0:8443 (alle Netze) oder 127.0.0.1:8443
  -tls=true|false   HTTPS mit selbst signiertem Zertifikat
`

func main() {
	cmd := ""
	args := os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	fl := flag.NewFlagSet("proof-of-fault", flag.ContinueOnError)
	fl.SetOutput(io.Discard)
	listen := fl.String("listen", "", "")
	useTLS := fl.String("tls", "", "")
	if err := fl.Parse(args); err != nil {
		fail(usage)
	}

	dir, err := dataDir()
	if err != nil {
		fail("Datenverzeichnis: " + err.Error())
	}
	cfg, err := loadAgentConfig(dir)
	if err != nil {
		fail(err.Error())
	}
	if *listen != "" || *useTLS != "" {
		if *listen != "" {
			cfg.Listen = *listen
		}
		if *useTLS != "" {
			cfg.TLS = *useTLS == "true" || *useTLS == "1"
		}
		if err := cfg.save(dir); err != nil {
			fail(err.Error())
		}
	}

	switch cmd {
	case "run":
		if err := run(dir, cfg); err != nil {
			fail(err.Error())
		}
	case "", "install":
		launch(dir, cfg, cmd == "install")
	case "uninstall":
		stopRunning(cfg)
		if err := uninstallService(); err != nil {
			fail("Autostart entfernen: " + err.Error())
		}
		say("Dienst gestoppt und Autostart entfernt. Die Messdaten liegen weiter in " + dir + ".")
	case "status":
		v, _ := runningVersion(cfg)
		state := "läuft nicht"
		if v != "" {
			state = "läuft (v" + v + ")"
		}
		say(statusText(cfg, state))
	case "new-key":
		cfg.Token = newUUID()
		if err := cfg.save(dir); err != nil {
			fail(err.Error())
		}
		stopRunning(cfg)
		startService()
		say("Neuer Zugangsschlüssel: " + cfg.Token)
	case "help", "-h", "--help":
		say(usage)
	default:
		fail("Unbekannter Befehl " + cmd + "\n\n" + usage)
	}
}

func statusText(cfg *agentConfig, state string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "proof-of-fault v%s – Dienst %s\n\n", Version, state)
	fmt.Fprintf(&b, "Weboberfläche:      %s\n", cfg.URL(""))
	for _, u := range cfg.LANURLs() {
		fmt.Fprintf(&b, "                    %s\n", u)
	}
	fmt.Fprintf(&b, "Zugangsschlüssel:   %s\n", cfg.Token)
	fmt.Fprintf(&b, "Direkt angemeldet:  %s\n", cfg.URL(cfg.Token))
	if cfg.TLS {
		b.WriteString("\nDer Browser warnt beim ersten Aufruf vor dem selbst signierten Zertifikat –\neinmal bestätigen; die Verbindung ist trotzdem verschlüsselt.\n")
	}
	return b.String()
}

// run is the background service: measurement engine plus web server.
func run(dir string, cfg *agentConfig) error {
	if f, err := openLog(dir); err == nil {
		log.SetOutput(f)
		defer f.Close()
	}
	log.Printf("proof-of-fault v%s startet", Version)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	h := newHub()
	app := NewApp(h.emit)
	app.startup(ctx)
	defer app.shutdown(ctx)
	if app.initErr != nil {
		log.Printf("Fehler: %v", app.initErr) // shown in the web interface
	}
	ui, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		return err
	}
	s := newServer(app, cfg, h, ui, cancel)
	return serve(ctx, s, cfg, dir)
}

func openLog(dir string) (*os.File, error) {
	p := filepath.Join(dir, "proof-of-fault.log")
	if st, err := os.Stat(p); err == nil && st.Size() > 5<<20 {
		os.Rename(p, p+".1")
	}
	return os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
}

// httpClient talks to the local service (self-signed certificate).
var httpClient = &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{
	TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // own local service
}}

// runningVersion returns the version of the service running at the
// configured address ("" if none answers).
func runningVersion(cfg *agentConfig) (string, error) {
	resp, err := httpClient.Get(strings.TrimSuffix(cfg.URL(""), "/") + "/api/ping")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var x struct{ App, Version string }
	if err := json.NewDecoder(resp.Body).Decode(&x); err != nil || x.App != "proof-of-fault" {
		return "", fmt.Errorf("Port %s ist von einem anderen Programm belegt", cfg.Listen)
	}
	return x.Version, nil
}

// stopRunning asks a running service to exit and waits for it.
func stopRunning(cfg *agentConfig) {
	if v, _ := runningVersion(cfg); v == "" {
		return
	}
	stopServiceManager() // systemd would restart it otherwise
	req, _ := http.NewRequest(http.MethodPost, strings.TrimSuffix(cfg.URL(""), "/")+"/api/shutdown", nil)
	req.Header.Set("Authorization", "Bearer "+cfg.Token)
	req.Header.Set("X-POF", "1")
	if resp, err := httpClient.Do(req); err == nil {
		resp.Body.Close()
	}
	for i := 0; i < 50; i++ {
		if v, _ := runningVersion(cfg); v == "" {
			time.Sleep(300 * time.Millisecond) // let the process release its files
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// launch is the double-click / install path: make sure the service of this
// version is installed and running, then open the web interface.
func launch(dir string, cfg *agentConfig, force bool) {
	v, err := runningVersion(cfg)
	if err != nil && v == "" && strings.Contains(err.Error(), "belegt") {
		fail(err.Error() + ". Anderen Port wählen: proof-of-fault install -listen 127.0.0.1:8444")
	}
	if v != Version || force || !serviceInstalled() {
		if v != "" {
			stopRunning(cfg)
		}
		if err := installService(); err != nil {
			fail("Dienst einrichten: " + err.Error())
		}
		if err := startService(); err != nil {
			fail("Dienst starten: " + err.Error())
		}
		ok := false
		for i := 0; i < 50 && !ok; i++ {
			time.Sleep(200 * time.Millisecond)
			v, _ = runningVersion(cfg)
			ok = v == Version
		}
		if !ok {
			fail("Der Dienst startet nicht. Details im Protokoll: " + filepath.Join(dir, "proof-of-fault.log"))
		}
	}
	url := cfg.URL(cfg.Token)
	if !openBrowser(url) {
		say(statusText(cfg, "läuft (v"+Version+")"))
	} else if hasConsole() {
		say(statusText(cfg, "läuft (v"+Version+")"))
	}
}
