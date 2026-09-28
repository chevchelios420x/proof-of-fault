package monitor

import (
	"fmt"
	"strings"
	"time"

	"github.com/chevchelios420x/proof-of-fault/internal/config"
	"github.com/chevchelios420x/proof-of-fault/internal/docsis"
	"github.com/chevchelios420x/proof-of-fault/internal/secret"
	"github.com/chevchelios420x/proof-of-fault/internal/store"
)

// EvDocsis carries a new DOCSIS reading to the UI.
const EvDocsis = "docsis"

// Event kinds of the DOCSIS diagnostics.
const (
	KindDocsis      = "docsis"       // status change or new uncorrectable errors
	KindDocsisError = "docsis_error" // reading failed
)

// FritzClient builds a FRITZ!Box client from the settings (nil if the
// access is not configured).
func FritzClient(cfg config.Settings) *docsis.Client {
	if cfg.Fritz.URL == "" || cfg.Fritz.Password == "" {
		return nil
	}
	return docsis.NewClient(cfg.Fritz.URL, cfg.Fritz.User, secret.Unprotect(cfg.Fritz.Password))
}

type docsisResult struct {
	snap   docsis.Snapshot
	err    error
	reason string
}

// docsisEnabled reports whether DOCSIS readings are active.
func (r *runner) docsisEnabled() bool {
	return r.cfg.Access == "docsis" && r.cfg.Fritz.Password != "" && r.cfg.Fritz.URL != ""
}

// pollDocsis starts a reading in the background (at most one at a time;
// reasons of skipped triggers are merged into the next one).
func (r *runner) pollDocsis(reason string) {
	if !r.docsisEnabled() || r.ctx == nil {
		return
	}
	if r.docsisBusy {
		if !strings.Contains(r.docsisPending, reason) {
			r.docsisPending = strings.TrimPrefix(r.docsisPending+", "+reason, ", ")
		}
		return
	}
	if r.fritz == nil || r.fritzKey != r.cfg.Fritz.URL+"|"+r.cfg.Fritz.User+"|"+r.cfg.Fritz.Password {
		r.fritz = FritzClient(r.cfg)
		r.fritzKey = r.cfg.Fritz.URL + "|" + r.cfg.Fritz.User + "|" + r.cfg.Fritz.Password
	}
	c := r.fritz
	r.docsisBusy = true
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		snap, err := c.Fetch()
		select {
		case r.docsisRes <- docsisResult{snap: snap, err: err, reason: reason}:
		case <-r.ctx.Done():
		}
	}()
}

// handleDocsis stores a finished reading and logs noteworthy changes.
func (r *runner) handleDocsis(res docsisResult) {
	r.docsisBusy = false
	defer func() {
		if r.docsisPending != "" {
			p := r.docsisPending
			r.docsisPending = ""
			r.pollDocsis(p)
		}
	}()
	now := time.Now()
	if res.err != nil {
		if !r.docsisFailing {
			r.docsisFailing = true
			r.logEvent(store.Event{Kind: KindDocsisError, Severity: "warn", Title: "DOCSIS-Werte der FRITZ!Box nicht abrufbar",
				Detail: res.err.Error() + " (Anlass: " + res.reason + "). Wird beim nächsten Intervall erneut versucht."})
		}
		return
	}
	if r.docsisFailing {
		r.docsisFailing = false
		r.logEvent(store.Event{Kind: KindDocsis, Severity: "ok", Title: "DOCSIS-Werte wieder abrufbar"})
	}
	s := res.snap
	s.T, s.Reason = now.UnixMilli(), res.reason
	s.ApplyDelta(r.lastDocsis)
	r.m.store.AddDocsis(r.sid, now, s.Reason, s.Status, s)
	r.m.emit(EvDocsis, s)

	prevStatus := "good"
	if r.lastDocsis != nil {
		prevStatus = r.lastDocsis.Status
	}
	if s.Status != prevStatus || s.NonCorrDelta > 0 {
		sev := map[string]string{"good": "ok", "warning": "warn", "critical": "crit"}[s.Status]
		title := fmt.Sprintf("DOCSIS: Leitungswerte %s", map[string]string{"good": "in Ordnung", "warning": "auffällig", "critical": "kritisch"}[s.Status])
		if s.NonCorrDelta > 0 {
			title += fmt.Sprintf(", %d neue nicht korrigierbare Fehler", s.NonCorrDelta)
		}
		detail := fmt.Sprintf("Anlass: %s. Downstream %d Kanäle (Pegel %.1f … %.1f dBmV, SNR/MER min. %.1f dB), Upstream %d Kanäle (Sendepegel %.1f … %.1f dBmV).",
			s.Reason, len(s.DS), s.DSPowerMin, s.DSPowerMax, s.SNRMin, len(s.US), s.USPowerMin, s.USPowerMax)
		if len(s.Issues) > 0 {
			detail += " Auffällig: " + strings.Join(s.Issues, "; ") + "."
		}
		r.logEvent(store.Event{Kind: KindDocsis, Severity: sev, Zone: "ISP_EDGE", Title: title, Detail: detail})
	}
	r.lastDocsis = &s
}
