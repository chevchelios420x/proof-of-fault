package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/chevchelios420x/proof-of-fault/internal/config"
	"github.com/chevchelios420x/proof-of-fault/internal/monitor"
	"github.com/chevchelios420x/proof-of-fault/internal/path"
	"github.com/chevchelios420x/proof-of-fault/internal/probe"
	"github.com/chevchelios420x/proof-of-fault/internal/report"
	"github.com/chevchelios420x/proof-of-fault/internal/secret"
	"github.com/chevchelios420x/proof-of-fault/internal/store"
)

// App is the API exposed to the frontend. It stays thin: all logic lives in
// internal/.
type App struct {
	ctx     context.Context
	store   *store.Store
	monitor *monitor.Monitor
	initErr error
	emit    func(ev string, data any) // to all connected browsers
}

func NewApp(emit func(ev string, data any)) *App { return &App{emit: emit} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	dbPath, err := store.DefaultPath()
	if err != nil {
		a.initErr = err
		return
	}
	st, err := store.Open(dbPath)
	if err != nil {
		a.initErr = fmt.Errorf("Datenbank %s: %w", dbPath, err)
		return
	}
	a.store = st
	p, err := probe.New()
	if err != nil {
		a.initErr = err
		return
	}
	a.monitor = monitor.New(p, st, a.emit)

	cfg := a.monitor.Settings()
	if cfg.RetentionDays > 0 {
		st.DeleteSessionsBefore(time.Now().AddDate(0, 0, -cfg.RetentionDays))
	}
	if cfg.AutoStart && cfg.LastTarget != "" {
		a.monitor.Start(cfg.LastTarget)
	}
}

// GetSettings returns the current settings.
func (a *App) GetSettings() (config.Settings, error) {
	if err := a.ready(); err != nil {
		return config.Defaults(), err
	}
	return forUI(a.monitor.Settings()), nil
}

// forUI removes the stored (protected) password before settings go to the UI.
func forUI(cfg config.Settings) config.Settings {
	cfg.Fritz.HasPassword = cfg.Fritz.Password != ""
	cfg.Fritz.Password = ""
	return cfg
}

// SaveSettings stores and applies settings.
func (a *App) SaveSettings(cfg config.Settings) (config.Settings, error) {
	if err := a.ready(); err != nil {
		return cfg, err
	}
	cfg.LastTarget = a.monitor.Settings().LastTarget
	if err := a.monitor.SaveSettings(cfg); err != nil {
		return forUI(cfg), err
	}
	return forUI(a.monitor.Settings()), nil
}

// FritzTest is the result of a FRITZ!Box connection test.
type FritzTest struct {
	OK     bool     `json:"ok"`
	Error  string   `json:"error"`
	Model  string   `json:"model"`
	DS     int      `json:"ds"`
	US     int      `json:"us"`
	Status string   `json:"status"`
	Issues []string `json:"issues"`
}

// TestFritz logs in and reads the DOCSIS data once. An empty password uses
// the stored one.
func (a *App) TestFritz(url, user, password, access string) FritzTest {
	if err := a.ready(); err != nil {
		return FritzTest{Error: err.Error()}
	}
	cfg := a.monitor.Settings()
	cfg.Fritz.URL, cfg.Fritz.User = url, user
	if password != "" {
		cfg.Fritz.Password = secret.Protect(password)
	}
	c := monitor.FritzClient(cfg)
	if c == nil {
		return FritzTest{Error: "Bitte Adresse und Kennwort der FRITZ!Box eingeben."}
	}
	res := FritzTest{Model: c.Model()}
	if access != "docsis" {
		// DSL, fibre and LTE/5G have no line data yet: check the login only.
		if err := c.Login(); err != nil {
			res.Error = err.Error()
			return res
		}
		res.OK = true
		return res
	}
	s, err := c.Fetch()
	if err != nil {
		res.Error = err.Error()
		return res
	}
	res.OK, res.DS, res.US, res.Status, res.Issues = true, len(s.DS), len(s.US), s.Status, s.Issues
	return res
}

// fritzDebug reads all known FRITZ!Box pages once (read-only) and returns
// them as ZIP with session ID and password fields redacted. An empty
// password uses the stored one.
func (a *App) fritzDebug(url, user, password string) ([]byte, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	cfg := a.monitor.Settings()
	cfg.Fritz.URL, cfg.Fritz.User = url, user
	if password != "" {
		cfg.Fritz.Password = secret.Protect(password)
	}
	c := monitor.FritzClient(cfg)
	if c == nil {
		return nil, fmt.Errorf("bitte Adresse und Kennwort der FRITZ!Box eingeben")
	}
	return c.DebugDump("v" + Version)
}

// GetDocsis returns all DOCSIS readings of a session.
func (a *App) GetDocsis(id int64) ([]json.RawMessage, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	return a.store.DocsisSnapshots(id)
}

// DefaultSettings returns the factory settings (for "reset").
func (a *App) DefaultSettings() config.Settings { return config.Defaults() }

// SetSessionNote stores the comment of a session.
func (a *App) SetSessionNote(id int64, note string) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.store.SetSessionNote(id, note)
}

// DeleteSession removes a stored measurement (not the running one).
func (a *App) DeleteSession(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	if st := a.monitor.Status(); st.SessionID == id && st.State != "idle" && st.State != "error" {
		return fmt.Errorf("die laufende Messung kann nicht gelöscht werden")
	}
	return a.store.DeleteSession(id)
}

func (a *App) shutdown(context.Context) {
	if a.monitor != nil {
		a.monitor.Stop()
	}
	if a.store != nil {
		a.store.Close()
	}
}

func (a *App) ready() error {
	return a.initErr
}

// StartMonitoring starts a new measurement session.
func (a *App) StartMonitoring(target string) error {
	if err := a.ready(); err != nil {
		return err
	}
	if target == "" {
		return fmt.Errorf("bitte eine Ziel-IP oder Domain eingeben")
	}
	return a.monitor.Start(target)
}

// StopMonitoring ends the running session.
func (a *App) StopMonitoring() {
	if a.monitor != nil {
		a.monitor.Stop()
	}
}

// SetHopWatched toggles the individual latency line of a hop.
func (a *App) SetHopWatched(addr string, on bool) error {
	if err := a.ready(); err != nil {
		return err
	}
	a.monitor.SetHopWatched(addr, on)
	return nil
}

// SetHopName sets an optional label for a hop, shown in chart and report.
func (a *App) SetHopName(addr, name string) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.monitor.SetHopName(addr, name)
}

// SaveCustomPoint adds or updates a user-defined measuring point
// (IP or host name, optional name, zone "" = automatic).
func (a *App) SaveCustomPoint(host, name, zone string, enabled, tcp bool, port int) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.monitor.SaveCustomPoint(store.CustomPoint{Host: host, Name: name, Zone: zone, Enabled: enabled, TCP: tcp, Port: port})
}

// AddSuggestedPoints re-adds the suggested backup targets.
func (a *App) AddSuggestedPoints() error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.monitor.AddSuggestedPoints()
}

// DeleteCustomPoint removes a user-defined measuring point.
func (a *App) DeleteCustomPoint(host string) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.monitor.DeleteCustomPoint(host)
}

// SetHopZone assigns a zone to a hop (remembered per hop address); an empty
// zone restores the automatic classification.
func (a *App) SetHopZone(addr, zone string) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.monitor.SetHopZone(addr, path.Zone(zone))
}

// GetStatus returns the monitor status.
func (a *App) GetStatus() (monitor.Status, error) {
	if err := a.ready(); err != nil {
		return monitor.Status{State: "error", Message: err.Error()}, nil
	}
	return a.monitor.Status(), nil
}

// ListSessions returns stored sessions, newest first.
func (a *App) ListSessions() ([]store.SessionInfo, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	return a.store.Sessions()
}

// LiveData is the event log and diagnosis of a (running) session.
type LiveData struct {
	Diagnosis report.Diagnosis `json:"diagnosis"`
	Events    []store.Event    `json:"events"`
	Incidents []store.Incident `json:"incidents"`
}

// GetLive returns event log and diagnosis without loading all samples.
func (a *App) GetLive(id int64) (LiveData, error) {
	if err := a.ready(); err != nil {
		return LiveData{}, err
	}
	d, evs, ins, err := report.Live(a.store, id)
	return LiveData{Diagnosis: d, Events: evs, Incidents: ins}, err
}

// SessionData bundles report and chart series for the UI.
type SessionData struct {
	Report report.Report   `json:"report"`
	Series []report.Series `json:"series"`
}

// GetSession evaluates a stored session.
func (a *App) GetSession(id int64) (SessionData, error) {
	if err := a.ready(); err != nil {
		return SessionData{}, err
	}
	r, s, _, err := report.Build(a.store, id)
	return SessionData{Report: r, Series: s}, err
}

// exportName returns the download file name of an export.
func (a *App) exportName(id int64, format string) (string, error) {
	info, err := a.store.Session(id)
	if err != nil {
		return "", err
	}
	base := fmt.Sprintf("proof-of-fault_%d_%s", id, time.UnixMilli(info.StartedAt).Format("2006-01-02_1504"))
	switch format {
	case "html", "csv":
		return base + "." + format, nil
	case "events":
		return base + "_ereignisse.csv", nil
	case "pof":
		return base + ".pof", nil
	}
	return "", fmt.Errorf("unbekanntes Format %q", format)
}

// export writes a session as "html" report, "csv" samples, "events" CSV or
// "pof" (complete, importable export).
func (a *App) export(w io.Writer, id int64, format string) error {
	if err := a.ready(); err != nil {
		return err
	}
	if format == "pof" {
		return a.store.ExportSession(w, id, "proof-of-fault v"+Version)
	}
	r, series, samples, err := report.Build(a.store, id)
	if err != nil {
		return err
	}
	switch format {
	case "csv":
		return report.WriteCSV(w, samples)
	case "events":
		return report.WriteEventsCSV(w, r.Events)
	case "html":
		return report.WriteHTML(w, r, series)
	}
	return fmt.Errorf("unbekanntes Format %q", format)
}

// importSession stores an export file as new session and returns its ID.
func (a *App) importSession(r io.ReaderAt, size int64) (int64, error) {
	if err := a.ready(); err != nil {
		return 0, err
	}
	return a.store.ImportSession(r, size)
}

// periodReport writes the report over 24 hours, 7, 30 and 90 days.
func (a *App) periodReport(w io.Writer) error {
	if err := a.ready(); err != nil {
		return err
	}
	r, err := report.BuildPeriods(a.store, time.Now(), report.PeriodDays, "v"+Version)
	if err != nil {
		return err
	}
	return report.WritePeriodHTML(w, r)
}

// LivePoint is one sample for the live chart.
type LivePoint struct {
	Zone  string  `json:"zone"`
	T     int64   `json:"t"`     // unix ms
	RTTMs float64 `json:"rttMs"` // -1 = loss
}

// GetRecent returns the samples of the last minutes of a session, so a
// browser that connects later can fill the live chart.
func (a *App) GetRecent(id int64, minutes int) ([]LivePoint, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	if minutes <= 0 || minutes > 24*60 {
		minutes = 90
	}
	ss, err := a.store.SamplesSince(id, time.Now().Add(-time.Duration(minutes)*time.Minute).UnixMilli())
	if err != nil {
		return nil, err
	}
	out := make([]LivePoint, len(ss))
	for i, s := range ss {
		out[i] = LivePoint{Zone: s.Zone, T: s.At.UnixMilli(), RTTMs: -1}
		if s.RTT >= 0 {
			out[i].RTTMs = float64(s.RTT) / float64(time.Millisecond)
		}
	}
	return out, nil
}
