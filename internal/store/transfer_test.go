package store

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"
)

func TestExportImportRoundTrip(t *testing.T) {
	src, err := Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	id, _ := src.CreateSession("example.org", "192.0.2.1", map[string]string{"os": "test"})
	src.SetSessionNote(id, "Testnotiz")
	t0 := time.Unix(1_760_000_000, 123456789)
	src.AddSample(Sample{SessionID: id, Zone: "WAN", At: t0, RTT: 12345 * time.Microsecond})
	src.AddSample(Sample{SessionID: id, Zone: "WAN", At: t0.Add(time.Second), RTT: -1})
	src.AddEvent(id, Event{T: t0.UnixMilli(), Kind: "loss", Severity: "warn", Title: "Verlust"})
	src.AddIncident(id, Incident{T: t0.UnixMilli(), End: t0.UnixMilli() + 1000, Class: "isp", Title: "x"})
	src.StartOutage(id, "WAN", t0)
	src.EndSession(id)

	var buf bytes.Buffer
	if err := src.ExportSession(&buf, id, "test"); err != nil {
		t.Fatal(err)
	}
	dst, err := Open(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Close()
	dst.CreateSession("other", "192.0.2.9", nil) // IDs must not collide
	nid, err := dst.ImportSession(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if nid == id {
		t.Fatalf("expected new id, got %d", nid)
	}
	ss, _ := dst.Samples(nid)
	if len(ss) != 2 || !ss[0].At.Equal(t0) || ss[0].RTT != 12345*time.Microsecond || ss[1].RTT >= 0 {
		t.Fatalf("samples: %+v", ss)
	}
	info, _ := dst.Session(nid)
	if info.Target != "example.org" || info.EndedAt == 0 || !bytes.Contains([]byte(info.Note), []byte("Testnotiz")) {
		t.Fatalf("session: %+v", info)
	}
	evs, _ := dst.Events(nid)
	ins, _ := dst.Incidents(nid)
	os, _ := dst.Outages(nid)
	if len(evs) != 1 || len(ins) != 1 || ins[0].Class != "isp" || len(os) != 1 {
		t.Fatalf("events %d incidents %d outages %d", len(evs), len(ins), len(os))
	}

	// A modified file must be rejected.
	b := buf.Bytes()
	bad := bytes.Replace(b, []byte("example.org"), []byte("exbmple.org"), 1)
	if bytes.Equal(bad, b) {
		t.Skip("compressed – cannot tamper in place")
	}
	if _, err := dst.ImportSession(bytes.NewReader(bad), int64(len(bad))); err == nil {
		t.Fatal("tampered export accepted")
	}
}
