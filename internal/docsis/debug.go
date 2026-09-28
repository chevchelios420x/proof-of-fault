package docsis

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// debugPages are data.lua pages read for the debug export. Reading a page
// never changes settings on the box (no "apply"); unknown pages just fail.
var debugPages = []string{
	// overview / device
	"overview", "home", "boxinfo", "update", "energy", "ecoStat", "log",
	// internet connection
	"netMoni", "netCnt", "inetStat", "inetMon", "ipv6", "dnsSrv",
	// cable (DOCSIS)
	"docInfo", "docOv", "docStat",
	// DSL
	"dslOv", "dslStat", "dslSpec", "dslSpectrum", "dslLab",
	// fibre
	"fiberOv", "fiberStat", "gponInfo",
	// mobile (LTE/5G)
	"mobile", "mobileOv", "lteOv", "lteInfo", "lteStat", "lteCells", "wwanStat",
	// home network
	"netDev", "homeNet", "meshNet",
}

// debugFiles are fixed files; the XML descriptions need no login.
// debugPause spaces the page requests (tests shorten it).
var debugPause = 150 * time.Millisecond

var debugFiles = []string{
	"/jason_boxinfo.xml", "/tr064/tr64desc.xml", "/igddesc.xml", "/login_sid.lua?version=2",
}

var (
	sidRe    = regexp.MustCompile(`(?i)("?sid"?\s*[:=]\s*"?)([0-9a-f]{16})`)
	secretRe = regexp.MustCompile(`(?i)("(?:[^"]*(?:pass|psk|secret|pin|token|key)[^"]*)"\s*:\s*)"[^"]*"`)
)

// redact removes the session ID and values of password-like fields.
func (c *Client) redact(b []byte) []byte {
	if c.sid != "" {
		b = bytes.ReplaceAll(b, []byte(c.sid), []byte("SID-ENTFERNT"))
	}
	b = sidRe.ReplaceAll(b, []byte(`${1}SID-ENTFERNT`))
	return secretRe.ReplaceAll(b, []byte(`${1}"***ENTFERNT***"`))
}

type debugEntry struct {
	Name   string `json:"name"`
	File   string `json:"file"`
	Status string `json:"status"` // ok | error text
	Bytes  int    `json:"bytes"`
}

// DebugDump logs in, reads all known pages read-only and returns a ZIP
// archive with the (redacted) raw answers and a manifest.
func (c *Client) DebugDump(appVersion string) ([]byte, error) {
	if err := c.Login(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	var entries []debugEntry
	add := func(name, file string, body []byte, err error) {
		e := debugEntry{Name: name, File: file, Status: "ok"}
		if err != nil {
			e.Status = err.Error()
		}
		if len(body) > 0 {
			body = c.redact(body)
			e.Bytes = len(body)
			if w, werr := zw.Create(file); werr == nil {
				w.Write(body)
			}
		}
		entries = append(entries, e)
	}

	for _, f := range debugFiles {
		body, err := c.get(f)
		name := strings.TrimPrefix(strings.SplitN(f, "?", 2)[0], "/")
		add(f, "files/"+strings.ReplaceAll(name, "/", "_"), body, err)
	}
	for _, p := range debugPages {
		body, err := c.rawPage(p)
		add("data.lua page="+p, "data.lua/"+p+".json", pretty(body), err)
		time.Sleep(debugPause) // be gentle with the box
	}
	// The parsed DOCSIS view as the app sees it (if it is a cable box).
	if s, err := c.Fetch(); err == nil {
		b, _ := json.MarshalIndent(s, "", "  ")
		add("proof-of-fault DOCSIS-Auswertung", "parsed/docsis.json", b, nil)
	} else {
		add("proof-of-fault DOCSIS-Auswertung", "parsed/docsis.json", nil, err)
	}

	man := map[string]any{
		"created":    time.Now().Format(time.RFC3339),
		"app":        "proof-of-fault " + appVersion,
		"box":        c.Model(),
		"url":        c.BaseURL,
		"note":       "Rohdaten der FRITZ!Box zur Fehlersuche. Session-ID und Kennwort-Felder wurden entfernt; enthalten sein können trotzdem Gerätenamen, MAC-/IP-Adressen, Telefonnummern (Ereignisprotokoll) und Anschlussdaten. Nicht öffentlich weitergeben.",
		"entries":    entries,
		"readOnly":   true,
		"pagesTried": len(debugPages),
	}
	b, _ := json.MarshalIndent(man, "", "  ")
	if w, err := zw.Create("manifest.json"); err == nil {
		w.Write(b)
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func pretty(b []byte) []byte {
	var v any
	if json.Unmarshal(b, &v) != nil {
		return b
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return b
	}
	return out
}

// get fetches a fixed path (no session needed).
func (c *Client) get(path string) ([]byte, error) {
	resp, err := c.http.Get(c.BaseURL + path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err == nil && resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return body, err
}

// rawPage returns the complete data.lua answer of a page (not only "data").
func (c *Client) rawPage(page string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sid == "" {
		if err := c.loginLocked(); err != nil {
			return nil, err
		}
	}
	form := url.Values{"xhr": {"1"}, "sid": {c.sid}, "lang": {"de"}, "page": {page}, "xhrId": {"all"}, "no_sidrenew": {""}}
	resp, err := c.http.PostForm(c.BaseURL+"/data.lua", form)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return body, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &env) != nil {
		return body, fmt.Errorf("keine JSON-Antwort (Seite unbekannt?)")
	}
	if len(env.Data) <= 2 {
		return body, fmt.Errorf("leere Daten (Seite auf diesem Modell nicht vorhanden?)")
	}
	return body, nil
}
