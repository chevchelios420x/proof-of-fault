package store

import (
	"archive/zip"
	"bufio"
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// A session export (".pof") is a ZIP with one JSON-lines file per table and
// a manifest with SHA-256 checksums. Line 1 of a table file lists the
// columns, every further line holds the values of one row. Columns are
// matched by name on import, so exports of older and newer versions can be
// read on Windows and Linux alike.

// ExportFormat identifies session exports.
const ExportFormat = "proof-of-fault-session"

// sessionTables are the tables that belong to a session (besides session).
var sessionTables = []string{"path_snapshot", "sample", "event", "incident", "docsis_snapshot", "outage"}

// Manifest describes an export.
type Manifest struct {
	Format     string            `json:"format"`
	Version    int               `json:"version"`
	App        string            `json:"app"` // exporting program and version
	ExportedAt int64             `json:"exportedAt"`
	Session    SessionInfo       `json:"session"`
	Rows       map[string]int    `json:"rows"`
	SHA256     map[string]string `json:"sha256"`
}

// ExportSession writes session id as ZIP to w.
func (s *Store) ExportSession(w io.Writer, id int64, app string) error {
	s.flush()
	info, err := s.Session(id)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(w)
	m := Manifest{Format: ExportFormat, Version: 1, App: app, ExportedAt: time.Now().UnixMilli(), Session: info,
		Rows: map[string]int{}, SHA256: map[string]string{}}
	dump := func(table, where string) error {
		f, err := zw.Create(table + ".jsonl")
		if err != nil {
			return err
		}
		h := sha256.New()
		bw := bufio.NewWriter(io.MultiWriter(f, h))
		n, err := s.dumpTable(bw, table, where, id)
		if err != nil {
			return fmt.Errorf("%s: %w", table, err)
		}
		if err := bw.Flush(); err != nil {
			return err
		}
		m.Rows[table], m.SHA256[table+".jsonl"] = n, hex.EncodeToString(h.Sum(nil))
		return nil
	}
	if err := dump("session", "id=?"); err != nil {
		return err
	}
	for _, t := range sessionTables {
		if err := dump(t, "session_id=?"); err != nil {
			return err
		}
	}
	f, err := zw.Create("manifest.json")
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		return err
	}
	return zw.Close()
}

func (s *Store) dumpTable(w io.Writer, table, where string, id int64) (int, error) {
	rows, err := s.db.Query(`SELECT * FROM `+table+` WHERE `+where, id)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return 0, err
	}
	enc := json.NewEncoder(w)
	if err := enc.Encode(cols); err != nil {
		return 0, err
	}
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	n := 0
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return n, err
		}
		for i, v := range vals {
			if b, ok := v.([]byte); ok {
				vals[i] = string(b)
			}
		}
		if err := enc.Encode(vals); err != nil {
			return n, err
		}
		n++
	}
	return n, rows.Err()
}

// ReadManifest returns the manifest of an export without importing it.
func ReadManifest(r io.ReaderAt, size int64) (Manifest, error) {
	var m Manifest
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return m, fmt.Errorf("keine gültige Export-Datei: %w", err)
	}
	f, err := zr.Open("manifest.json")
	if err != nil {
		return m, fmt.Errorf("keine proof-of-fault-Export-Datei (manifest.json fehlt)")
	}
	defer f.Close()
	if err := json.NewDecoder(f).Decode(&m); err != nil {
		return m, err
	}
	if m.Format != ExportFormat {
		return m, fmt.Errorf("unbekanntes Format %q", m.Format)
	}
	return m, nil
}

// ImportSession reads an export and stores it as a new session. Checksums
// are verified first; the session ID is newly assigned. The comment notes
// that the data was imported.
func (s *Store) ImportSession(r io.ReaderAt, size int64) (int64, error) {
	m, err := ReadManifest(r, size)
	if err != nil {
		return 0, err
	}
	zr, _ := zip.NewReader(r, size)
	files := map[string][]byte{}
	for _, t := range append([]string{"session"}, sessionTables...) {
		name := t + ".jsonl"
		f, err := zr.Open(name)
		if err != nil {
			return 0, fmt.Errorf("%s fehlt in der Export-Datei", name)
		}
		b, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			return 0, err
		}
		sum := sha256.Sum256(b)
		if want := m.SHA256[name]; want != "" && want != hex.EncodeToString(sum[:]) {
			return 0, fmt.Errorf("Prüfsumme von %s stimmt nicht – Datei beschädigt oder verändert", name)
		}
		files[t] = b
	}
	s.flush()
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	// Session row: new ID, comment marks the import.
	note := fmt.Sprintf("Importiert am %s aus %s (ursprüngliche Messung #%d)", time.Now().Format("02.01.2006 15:04"), m.App, m.Session.ID)
	newID, err := importRows(tx, "session", files["session"], func(cols []string, vals []any) {
		for i, c := range cols {
			switch c {
			case "id":
				vals[i] = nil
			case "note":
				if v, _ := vals[i].(string); v != "" {
					vals[i] = v + "\n" + note
				} else {
					vals[i] = note
				}
			}
		}
	})
	if err != nil {
		return 0, fmt.Errorf("session: %w", err)
	}
	if newID == 0 {
		return 0, fmt.Errorf("die Export-Datei enthält keine Messung")
	}
	for _, t := range sessionTables {
		_, err := importRows(tx, t, files[t], func(cols []string, vals []any) {
			for i, c := range cols {
				switch c {
				case "session_id":
					vals[i] = newID
				case "id":
					vals[i] = nil // events/incidents get new IDs
				}
			}
		})
		if err != nil {
			return 0, fmt.Errorf("%s: %w", t, err)
		}
	}
	return newID, tx.Commit()
}

// importRows inserts the rows of one table file and returns the last
// inserted row ID. Columns unknown to this database version are skipped.
func importRows(tx *sql.Tx, table string, data []byte, fix func(cols []string, vals []any)) (int64, error) {
	known := map[string]bool{}
	rows, err := tx.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var n string
		rows.Scan(&n)
		known[n] = true
	}
	rows.Close()

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber() // nanosecond timestamps exceed float64 precision
	var cols []string
	if err := dec.Decode(&cols); err != nil {
		return 0, err
	}
	var keep []int
	var names []string
	for i, c := range cols {
		if known[c] {
			keep = append(keep, i)
			names = append(names, c)
		}
	}
	stmt, err := tx.Prepare(`INSERT INTO ` + table + `(` + strings.Join(names, ",") + `) VALUES(` +
		strings.TrimSuffix(strings.Repeat("?,", len(names)), ",") + `)`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()
	var last int64
	args := make([]any, len(keep))
	for {
		var vals []any
		if err := dec.Decode(&vals); err == io.EOF {
			break
		} else if err != nil {
			return 0, err
		}
		if len(vals) != len(cols) {
			return 0, fmt.Errorf("Zeile mit %d statt %d Werten", len(vals), len(cols))
		}
		for i, v := range vals {
			if n, ok := v.(json.Number); ok {
				if x, err := n.Int64(); err == nil {
					vals[i] = x
				} else if f, err := n.Float64(); err == nil {
					vals[i] = f
				}
			}
		}
		fix(cols, vals)
		for j, i := range keep {
			args[j] = vals[i]
		}
		r, err := stmt.Exec(args...)
		if err != nil {
			return 0, err
		}
		last, _ = r.LastInsertId()
	}
	return last, nil
}
