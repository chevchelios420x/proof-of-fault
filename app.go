package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/chevchelios420x/proof-of-fault/internal/monitor"
	"github.com/chevchelios420x/proof-of-fault/internal/path"
	"github.com/chevchelios420x/proof-of-fault/internal/probe"
	"github.com/chevchelios420x/proof-of-fault/internal/report"
	"github.com/chevchelios420x/proof-of-fault/internal/store"
)

// App is the API exposed to the frontend. It stays thin: all logic lives in
// internal/.
type App struct {
	ctx     context.Context
	store   *store.Store
	monitor *monitor.Monitor
	initErr error
}

func NewApp() *App { return &App{} }

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
	a.monitor = monitor.New(p, st, func(ev string, data any) { runtime.EventsEmit(ctx, ev, data) })
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

// Export writes the session as "csv" or "html" to a user-chosen file and
// returns the path ("" if cancelled).
func (a *App) Export(id int64, format string) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	r, series, samples, err := report.Build(a.store, id)
	if err != nil {
		return "", err
	}
	name := fmt.Sprintf("proof-of-fault_%d_%s.%s", id, time.UnixMilli(r.Session.StartedAt).Format("2006-01-02_1504"), format)
	filter := runtime.FileFilter{DisplayName: "HTML-Bericht (*.html)", Pattern: "*.html"}
	if format == "csv" {
		filter = runtime.FileFilter{DisplayName: "CSV (*.csv)", Pattern: "*.csv"}
	}
	dst, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		DefaultFilename: name, Filters: []runtime.FileFilter{filter},
	})
	if err != nil || dst == "" {
		return "", err
	}
	f, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	defer f.Close()
	switch format {
	case "csv":
		err = report.WriteCSV(f, samples)
	case "html":
		err = report.WriteHTML(f, r, series)
	default:
		err = fmt.Errorf("unbekanntes Format %q", format)
	}
	if err != nil {
		return "", err
	}
	return dst, f.Close()
}
