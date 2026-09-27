// Package store persists measurement sessions in an embedded SQLite database
// (WAL mode, pure Go driver: no CGO required).
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/chevchelios420x/proof-of-fault/internal/config"
)

const schema = `
CREATE TABLE IF NOT EXISTS session (
	id         INTEGER PRIMARY KEY,
	target     TEXT NOT NULL,
	target_ip  TEXT NOT NULL,
	started_ns INTEGER NOT NULL,
	ended_ns   INTEGER,
	host_info  TEXT
);
CREATE TABLE IF NOT EXISTS path_snapshot (
	session_id INTEGER NOT NULL,
	ts_ns      INTEGER NOT NULL,
	hops       TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS sample (
	session_id INTEGER NOT NULL,
	zone       TEXT NOT NULL,
	ts_ns      INTEGER NOT NULL,
	rtt_us     INTEGER            -- NULL = loss
);
CREATE INDEX IF NOT EXISTS sample_idx ON sample(session_id, zone, ts_ns);
CREATE TABLE IF NOT EXISTS hop_zone (
	addr TEXT PRIMARY KEY,
	zone TEXT NOT NULL            -- user override, remembered across sessions
);
CREATE TABLE IF NOT EXISTS hop_pref (
	addr    TEXT PRIMARY KEY,
	name    TEXT NOT NULL DEFAULT '', -- optional user label
	watched INTEGER NOT NULL DEFAULT 0 -- own line in the live chart
);
CREATE TABLE IF NOT EXISTS event (
	id         INTEGER PRIMARY KEY,
	session_id INTEGER NOT NULL,
	ts_ns      INTEGER NOT NULL,
	end_ns     INTEGER NOT NULL DEFAULT 0,
	kind       TEXT NOT NULL,     -- session | path | path_change | zones | loss | loss_hop | spike | outage_start | outage_end
	severity   TEXT NOT NULL,     -- info | ok | warn | crit
	zone       TEXT NOT NULL DEFAULT '',
	ttl        INTEGER NOT NULL DEFAULT 0,
	addr       TEXT NOT NULL DEFAULT '',
	title      TEXT NOT NULL,
	detail     TEXT NOT NULL DEFAULT '',
	count      INTEGER NOT NULL DEFAULT 0,
	value_ms   REAL NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS event_idx ON event(session_id, ts_ns);
CREATE TABLE IF NOT EXISTS custom_point (
	host    TEXT PRIMARY KEY,       -- IP or host name entered by the user
	name    TEXT NOT NULL DEFAULT '',
	zone    TEXT NOT NULL DEFAULT '',
	enabled INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE IF NOT EXISTS incident (
	id         INTEGER PRIMARY KEY,
	session_id INTEGER NOT NULL,
	start_ns   INTEGER NOT NULL,
	end_ns     INTEGER NOT NULL,
	class      TEXT NOT NULL,
	data       TEXT NOT NULL      -- JSON: Incident
);
CREATE INDEX IF NOT EXISTS incident_idx ON incident(session_id, start_ns);
CREATE TABLE IF NOT EXISTS setting (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS outage (
	session_id INTEGER NOT NULL,
	zone       TEXT NOT NULL,     -- zone the fault is attributed to
	start_ns   INTEGER NOT NULL,
	end_ns     INTEGER            -- NULL = still ongoing
);
`

// Sample is one probe result. RTT < 0 means loss.
type Sample struct {
	SessionID int64
	Zone      string
	At        time.Time
	RTT       time.Duration
}

// Store wraps the database and a batching writer.
type Store struct {
	db *sql.DB

	mu      sync.Mutex
	pending []Sample
	stop    chan struct{}
	done    chan struct{}
}

// DefaultPath returns %AppData%\proof-of-fault\data.db (or the OS equivalent).
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "proof-of-fault")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "data.db"), nil
}

// Open opens (or creates) the database at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema: %w", err)
	}
	// Migrations for databases of older versions (errors = column exists).
	db.Exec(`ALTER TABLE custom_point ADD COLUMN tcp INTEGER NOT NULL DEFAULT 0`)
	db.Exec(`ALTER TABLE custom_point ADD COLUMN port INTEGER NOT NULL DEFAULT 443`)
	s := &Store{db: db, stop: make(chan struct{}), done: make(chan struct{})}
	go s.flushLoop()
	return s, nil
}

// Close flushes pending samples and closes the database.
func (s *Store) Close() error {
	close(s.stop)
	<-s.done
	return s.db.Close()
}

// AddSample queues a sample; it is written within one second.
func (s *Store) AddSample(x Sample) {
	s.mu.Lock()
	s.pending = append(s.pending, x)
	s.mu.Unlock()
}

func (s *Store) flushLoop() {
	defer close(s.done)
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			s.flush()
		case <-s.stop:
			s.flush()
			return
		}
	}
}

func (s *Store) flush() {
	s.mu.Lock()
	batch := s.pending
	s.pending = nil
	s.mu.Unlock()
	if len(batch) == 0 {
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		return
	}
	stmt, err := tx.Prepare(`INSERT INTO sample(session_id, zone, ts_ns, rtt_us) VALUES(?,?,?,?)`)
	if err != nil {
		tx.Rollback()
		return
	}
	defer stmt.Close()
	for _, x := range batch {
		var rtt any
		if x.RTT >= 0 {
			rtt = x.RTT.Microseconds()
		}
		stmt.Exec(x.SessionID, x.Zone, x.At.UnixNano(), rtt)
	}
	tx.Commit()
}

// CreateSession starts a new session.
func (s *Store) CreateSession(target, ip string, hostInfo any) (int64, error) {
	hi, _ := json.Marshal(hostInfo)
	r, err := s.db.Exec(`INSERT INTO session(target, target_ip, started_ns, host_info) VALUES(?,?,?,?)`,
		target, ip, time.Now().UnixNano(), string(hi))
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

// EndSession marks a session as finished and closes open outages.
func (s *Store) EndSession(id int64) error {
	s.flush()
	now := time.Now().UnixNano()
	if _, err := s.db.Exec(`UPDATE outage SET end_ns=? WHERE session_id=? AND end_ns IS NULL`, now, id); err != nil {
		return err
	}
	_, err := s.db.Exec(`UPDATE session SET ended_ns=? WHERE id=?`, now, id)
	return err
}

// SavePath stores a snapshot of the discovered path (JSON).
func (s *Store) SavePath(id int64, at time.Time, hops any) error {
	b, err := json.Marshal(hops)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO path_snapshot(session_id, ts_ns, hops) VALUES(?,?,?)`, id, at.UnixNano(), string(b))
	return err
}

// StartOutage records the beginning of an outage attributed to zone.
func (s *Store) StartOutage(id int64, zone string, at time.Time) error {
	_, err := s.db.Exec(`INSERT INTO outage(session_id, zone, start_ns) VALUES(?,?,?)`, id, zone, at.UnixNano())
	return err
}

// EndOutage closes the open outage of zone.
func (s *Store) EndOutage(id int64, zone string, at time.Time) error {
	_, err := s.db.Exec(`UPDATE outage SET end_ns=? WHERE session_id=? AND zone=? AND end_ns IS NULL`, at.UnixNano(), id, zone)
	return err
}

// SessionInfo describes a stored session.
type SessionInfo struct {
	ID        int64  `json:"id"`
	Target    string `json:"target"`
	TargetIP  string `json:"targetIp"`
	StartedAt int64  `json:"startedAt"` // unix ms
	EndedAt   int64  `json:"endedAt"`   // unix ms, 0 = running/aborted
	HostInfo  string `json:"hostInfo"`
}

// Sessions lists sessions, newest first.
func (s *Store) Sessions() ([]SessionInfo, error) {
	rows, err := s.db.Query(`SELECT id, target, target_ip, started_ns, COALESCE(ended_ns,0), COALESCE(host_info,'') FROM session ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionInfo
	for rows.Next() {
		var x SessionInfo
		var st, en int64
		if err := rows.Scan(&x.ID, &x.Target, &x.TargetIP, &st, &en, &x.HostInfo); err != nil {
			return nil, err
		}
		x.StartedAt = st / 1e6
		x.EndedAt = en / 1e6
		out = append(out, x)
	}
	return out, rows.Err()
}

// Session returns one session.
func (s *Store) Session(id int64) (SessionInfo, error) {
	all, err := s.Sessions()
	if err != nil {
		return SessionInfo{}, err
	}
	for _, x := range all {
		if x.ID == id {
			return x, nil
		}
	}
	return SessionInfo{}, fmt.Errorf("session %d not found", id)
}

// Samples returns all samples of a session in time order.
func (s *Store) Samples(id int64) ([]Sample, error) {
	s.flush()
	rows, err := s.db.Query(`SELECT zone, ts_ns, rtt_us FROM sample WHERE session_id=? ORDER BY ts_ns`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Sample
	for rows.Next() {
		var zone string
		var ts int64
		var rtt sql.NullInt64
		if err := rows.Scan(&zone, &ts, &rtt); err != nil {
			return nil, err
		}
		x := Sample{SessionID: id, Zone: zone, At: time.Unix(0, ts), RTT: -1}
		if rtt.Valid {
			x.RTT = time.Duration(rtt.Int64) * time.Microsecond
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// Outage is a stored outage.
type Outage struct {
	Zone    string  `json:"zone"`
	Start   int64   `json:"start"` // unix ms
	End     int64   `json:"end"`   // unix ms, 0 = ongoing
	Seconds float64 `json:"seconds"`
}

// Outages returns the outages of a session.
func (s *Store) Outages(id int64) ([]Outage, error) {
	rows, err := s.db.Query(`SELECT zone, start_ns, COALESCE(end_ns,0) FROM outage WHERE session_id=? ORDER BY start_ns`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Outage
	for rows.Next() {
		var o Outage
		var st, en int64
		if err := rows.Scan(&o.Zone, &st, &en); err != nil {
			return nil, err
		}
		o.Start, o.End = st/1e6, en/1e6
		end := en
		if end == 0 {
			end = time.Now().UnixNano()
		}
		o.Seconds = float64(end-st) / 1e9
		out = append(out, o)
	}
	return out, rows.Err()
}

// LatestPath returns the most recent path snapshot as raw JSON.
func (s *Store) LatestPath(id int64) (string, error) {
	var hops string
	err := s.db.QueryRow(`SELECT hops FROM path_snapshot WHERE session_id=? ORDER BY ts_ns DESC LIMIT 1`, id).Scan(&hops)
	if err == sql.ErrNoRows {
		return "[]", nil
	}
	return hops, err
}

// PathChanges returns the number of stored path snapshots minus one.
func (s *Store) PathChanges(id int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM path_snapshot WHERE session_id=?`, id).Scan(&n)
	if n > 0 {
		n--
	}
	return n, err
}

// HopZones returns the user's zone overrides by hop address.
func (s *Store) HopZones() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT addr, zone FROM hop_zone`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var a, z string
		if err := rows.Scan(&a, &z); err != nil {
			return nil, err
		}
		out[a] = z
	}
	return out, rows.Err()
}

// SetHopZone remembers a zone override; an empty zone removes it.
func (s *Store) SetHopZone(addr, zone string) error {
	if zone == "" {
		_, err := s.db.Exec(`DELETE FROM hop_zone WHERE addr=?`, addr)
		return err
	}
	_, err := s.db.Exec(`INSERT INTO hop_zone(addr, zone) VALUES(?,?) ON CONFLICT(addr) DO UPDATE SET zone=excluded.zone`, addr, zone)
	return err
}

// HopPref holds per-hop user preferences.
type HopPref struct {
	Name    string
	Watched bool
}

// HopPrefs returns the stored per-hop preferences by address.
func (s *Store) HopPrefs() (map[string]HopPref, error) {
	rows, err := s.db.Query(`SELECT addr, name, watched FROM hop_pref`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]HopPref{}
	for rows.Next() {
		var a string
		var p HopPref
		if err := rows.Scan(&a, &p.Name, &p.Watched); err != nil {
			return nil, err
		}
		out[a] = p
	}
	return out, rows.Err()
}

// HopNames returns the user labels by hop address.
func (s *Store) HopNames() map[string]string {
	prefs, _ := s.HopPrefs()
	out := map[string]string{}
	for a, p := range prefs {
		if p.Name != "" {
			out[a] = p.Name
		}
	}
	return out
}

// SetHopName stores a label for a hop ("" removes it).
func (s *Store) SetHopName(addr, name string) error {
	_, err := s.db.Exec(`INSERT INTO hop_pref(addr, name) VALUES(?,?) ON CONFLICT(addr) DO UPDATE SET name=excluded.name`, addr, name)
	return err
}

// SetHopWatched stores whether a hop gets its own chart line.
func (s *Store) SetHopWatched(addr string, on bool) error {
	_, err := s.db.Exec(`INSERT INTO hop_pref(addr, watched) VALUES(?,?) ON CONFLICT(addr) DO UPDATE SET watched=excluded.watched`, addr, on)
	return err
}

// Event is one entry of the session's event log.
type Event struct {
	ID       int64   `json:"id"`
	T        int64   `json:"t"`   // unix ms
	End      int64   `json:"end"` // unix ms, 0 = point in time
	Kind     string  `json:"kind"`
	Severity string  `json:"severity"`
	Zone     string  `json:"zone"` // zone the event is attributed to
	TTL      int     `json:"ttl"`  // hop from which on the event was observed
	Addr     string  `json:"addr"`
	Title    string  `json:"title"`
	Detail   string  `json:"detail"`
	Count    int     `json:"count"`   // affected seconds / probes
	ValueMs  float64 `json:"valueMs"` // e.g. peak RTT of a spike
}

// AddEvent stores an event and returns it with its ID.
func (s *Store) AddEvent(sid int64, e Event) (Event, error) {
	r, err := s.db.Exec(`INSERT INTO event(session_id, ts_ns, end_ns, kind, severity, zone, ttl, addr, title, detail, count, value_ms)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, sid, e.T*1e6, e.End*1e6, e.Kind, e.Severity, e.Zone, e.TTL, e.Addr, e.Title, e.Detail, e.Count, e.ValueMs)
	if err != nil {
		return e, err
	}
	e.ID, _ = r.LastInsertId()
	return e, nil
}

// Events returns the event log of a session in time order.
func (s *Store) Events(sid int64) ([]Event, error) {
	rows, err := s.db.Query(`SELECT id, ts_ns, end_ns, kind, severity, zone, ttl, addr, title, detail, count, value_ms
		FROM event WHERE session_id=? ORDER BY ts_ns, id`, sid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var t, end int64
		if err := rows.Scan(&e.ID, &t, &end, &e.Kind, &e.Severity, &e.Zone, &e.TTL, &e.Addr, &e.Title, &e.Detail, &e.Count, &e.ValueMs); err != nil {
			return nil, err
		}
		e.T, e.End = t/1e6, end/1e6
		out = append(out, e)
	}
	return out, rows.Err()
}

// CustomPoint is a user-defined extra measuring point (e.g. a LAN device).
type CustomPoint struct {
	Host    string `json:"host"`
	Name    string `json:"name"`
	Zone    string `json:"zone"`
	Enabled bool   `json:"enabled"`
	TCP     bool   `json:"tcp"`  // additionally check a TCP connect every second
	Port    int    `json:"port"` // TCP port (default 443)
}

// CustomPoints returns all user-defined measuring points.
func (s *Store) CustomPoints() ([]CustomPoint, error) {
	rows, err := s.db.Query(`SELECT host, name, zone, enabled, tcp, port FROM custom_point ORDER BY rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CustomPoint
	for rows.Next() {
		var p CustomPoint
		if err := rows.Scan(&p.Host, &p.Name, &p.Zone, &p.Enabled, &p.TCP, &p.Port); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SaveCustomPoint inserts or updates a measuring point.
func (s *Store) SaveCustomPoint(p CustomPoint) error {
	if p.Port <= 0 || p.Port > 65535 {
		p.Port = 443
	}
	_, err := s.db.Exec(`INSERT INTO custom_point(host, name, zone, enabled, tcp, port) VALUES(?,?,?,?,?,?)
		ON CONFLICT(host) DO UPDATE SET name=excluded.name, zone=excluded.zone, enabled=excluded.enabled, tcp=excluded.tcp, port=excluded.port`,
		p.Host, p.Name, p.Zone, p.Enabled, p.TCP, p.Port)
	return err
}

// DeleteCustomPoint removes a measuring point.
func (s *Store) DeleteCustomPoint(host string) error {
	_, err := s.db.Exec(`DELETE FROM custom_point WHERE host=?`, host)
	return err
}

// IncidentSeries is one row of an incident matrix.
type IncidentSeries struct {
	Key    string    `json:"key"`
	Label  string    `json:"label"`
	Zone   string    `json:"zone"`
	Custom bool      `json:"custom"` // user-defined measuring point
	Target bool      `json:"target"` // main target
	States string    `json:"states"` // per second: '.' ok, 's' slow, 'x' no answer, '-' not probed
	RTT    []float64 `json:"rtt"`    // per second in ms, -1 = no answer / not probed
	Lost   int       `json:"lost"`   // lost seconds within the problem period
}

// Incident is one disruption with the state of every measuring point per
// second, including a few seconds before and after.
type Incident struct {
	ID       int64            `json:"id"`
	T        int64            `json:"t"`   // start of the problem (unix ms)
	End      int64            `json:"end"` // end of the problem (unix ms)
	Seconds  int              `json:"seconds"`
	Class    string           `json:"class"` // lan | isp | isp_core | target | alt | device
	Title    string           `json:"title"`
	Detail   string           `json:"detail"`
	Origin   string           `json:"origin"` // first failing point on the path
	Columns  []int64          `json:"columns"`
	PreRoll  int              `json:"preRoll"`  // columns before the problem
	PostRoll int              `json:"postRoll"` // columns after the problem
	Series   []IncidentSeries `json:"series"`
	Classes  map[string]int   `json:"classes"` // seconds per class within the incident
}

// AddIncident stores an incident.
func (s *Store) AddIncident(sid int64, in Incident) (Incident, error) {
	b, err := json.Marshal(in)
	if err != nil {
		return in, err
	}
	r, err := s.db.Exec(`INSERT INTO incident(session_id, start_ns, end_ns, class, data) VALUES(?,?,?,?,?)`,
		sid, in.T*1e6, in.End*1e6, in.Class, string(b))
	if err != nil {
		return in, err
	}
	in.ID, _ = r.LastInsertId()
	return in, nil
}

// Incidents returns the incidents of a session in time order.
func (s *Store) Incidents(sid int64) ([]Incident, error) {
	rows, err := s.db.Query(`SELECT id, data FROM incident WHERE session_id=? ORDER BY start_ns`, sid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Incident
	for rows.Next() {
		var id int64
		var data string
		if err := rows.Scan(&id, &data); err != nil {
			return nil, err
		}
		var in Incident
		if err := json.Unmarshal([]byte(data), &in); err != nil {
			continue
		}
		in.ID = id
		out = append(out, in)
	}
	return out, rows.Err()
}

// Settings returns the stored settings (defaults for missing values).
func (s *Store) Settings() config.Settings {
	var raw string
	cfg := config.Defaults()
	if err := s.db.QueryRow(`SELECT value FROM setting WHERE key='settings'`).Scan(&raw); err == nil {
		json.Unmarshal([]byte(raw), &cfg)
	}
	return cfg.Normalize()
}

// SaveSettings stores the settings.
func (s *Store) SaveSettings(cfg config.Settings) error {
	b, err := json.Marshal(cfg.Normalize())
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO setting(key, value) VALUES('settings', ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, string(b))
	return err
}

// DeleteSession removes a session with all its data.
func (s *Store) DeleteSession(id int64) error {
	s.flush()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	for _, t := range []string{"sample", "path_snapshot", "outage", "event", "incident"} {
		if _, err := tx.Exec(`DELETE FROM `+t+` WHERE session_id=?`, id); err != nil {
			tx.Rollback()
			return err
		}
	}
	if _, err := tx.Exec(`DELETE FROM session WHERE id=?`, id); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// DeleteSessionsBefore removes finished sessions that started before t and
// returns how many were deleted.
func (s *Store) DeleteSessionsBefore(t time.Time) (int, error) {
	rows, err := s.db.Query(`SELECT id FROM session WHERE started_ns < ? AND ended_ns IS NOT NULL`, t.UnixNano())
	if err != nil {
		return 0, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if err := s.DeleteSession(id); err != nil {
			return 0, err
		}
	}
	if len(ids) > 0 {
		s.db.Exec(`VACUUM`)
	}
	return len(ids), nil
}
