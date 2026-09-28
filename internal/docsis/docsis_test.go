package docsis

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const sample = `{"channelDs":{"docsis30":[
 {"channelID":1,"frequency":"114.000","powerLevel":"4.2","mse":"-37.5","modulation":"256QAM","corrErrors":120,"nonCorrErrors":0},
 {"channelID":2,"frequency":"122.000","powerLevel":"-7.0","mse":"-30.0","modulation":"256QAM","corrErrors":"50","nonCorrErrors":"3"}],
 "docsis31":[{"channelID":33,"frequency":"134.975 - 324.975","powerLevel":4.0,"mer":38.5,"modulation":"4096QAM","corrErrors":45,"nonCorrErrors":0}]},
"channelUs":{"docsis30":[{"channelID":1,"frequency":"51.000","powerLevel":"44.0","modulation":"64QAM","multiplex":"SC-QAM"}],
 "docsis31":[{"channelID":5,"frequency":"18.000 - 44.000","powerLevel":"39.0","modulation":"OFDMA"}]}}`

func TestParseAndRate(t *testing.T) {
	s, err := Parse(json.RawMessage(sample))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.DS) != 3 || len(s.US) != 2 {
		t.Fatalf("channels: %d/%d", len(s.DS), len(s.US))
	}
	if s.DS[1].Status != "critical" || !strings.Contains(s.DS[1].Issue, "SNR") {
		t.Errorf("channel 2 should be critical (power -7, SNR 30): %+v", s.DS[1])
	}
	if s.US[1].Power != 45 || s.US[1].Status != "good" {
		t.Errorf("US 3.1 power offset: %+v", s.US[1])
	}
	if s.Corr != 215 || s.NonCorr != 3 || s.Status != "critical" || s.SNRMin != 30 {
		t.Errorf("summary: %+v", s)
	}
	prev := s
	s2, _ := Parse(json.RawMessage(strings.Replace(sample, `"nonCorrErrors":"3"`, `"nonCorrErrors":"13"`, 1)))
	s2.ApplyDelta(&prev)
	if s2.NonCorrDelta != 10 {
		t.Errorf("delta: %+v", s2)
	}
}

func TestChallengeResponse(t *testing.T) {
	// Example from the AVM documentation (PBKDF2 login, password "1example!").
	got, err := challengeResponse("2$10000$5A1711$2000$5A1722", "1example!")
	if err != nil {
		t.Fatal(err)
	}
	if got != "5A1722$1798a1672bca7c6463d6b245f82b53703b0f50813401b03e4045a5861e689adb" {
		t.Errorf("pbkdf2 response: %s", got)
	}
	md5, _ := challengeResponse("1234567z", "äbc")
	if md5 != "1234567z-9e224a41eeefa284df7bb0f26c2913e2" {
		t.Errorf("md5 response: %s", md5)
	}
}

func TestRedact(t *testing.T) {
	c := &Client{sid: "0123456789abcdef"}
	in := []byte(`{"sid":"0123456789abcdef","wlan":{"psk":"geheim","name":"x"},"Password":"abc","url":"/x?sid=0123456789abcdef"}`)
	out := string(c.redact(in))
	for _, bad := range []string{"0123456789abcdef", "geheim", `"abc"`} {
		if strings.Contains(out, bad) {
			t.Errorf("not redacted %q in %s", bad, out)
		}
	}
	if !strings.Contains(out, `"name":"x"`) {
		t.Errorf("too much redacted: %s", out)
	}
}

func TestDebugDumpAgainstFakeBox(t *testing.T) {
	const sid = "fedcba9876543210"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/login_sid.lua" && r.URL.Query().Get("response") == "":
			fmt.Fprint(w, `<SessionInfo><SID>0000000000000000</SID><Challenge>2$10000$5A1711$2000$5A1722</Challenge><BlockTime>0</BlockTime><Users><User last="1">admin</User></Users></SessionInfo>`)
		case r.URL.Path == "/login_sid.lua":
			if r.URL.Query().Get("response") != "5A1722$1798a1672bca7c6463d6b245f82b53703b0f50813401b03e4045a5861e689adb" {
				fmt.Fprint(w, `<SessionInfo><SID>0000000000000000</SID></SessionInfo>`)
				return
			}
			fmt.Fprint(w, `<SessionInfo><SID>`+sid+`</SID></SessionInfo>`)
		case r.URL.Path == "/data.lua":
			r.ParseForm()
			if r.Form.Get("sid") != sid {
				fmt.Fprint(w, `<html>login</html>`)
				return
			}
			if r.Form.Get("page") == "docInfo" {
				fmt.Fprint(w, `{"sid":"`+sid+`","data":`+sample+`}`)
				return
			}
			fmt.Fprint(w, `{"data":{}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	debugPause = 0
	c := NewClient(srv.URL, "", "1example!")
	zb, err := c.DebugDump("test")
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(zb), int64(len(zb)))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		if strings.Contains(string(b), sid) {
			t.Errorf("%s still contains the SID", f.Name)
		}
	}
	for _, want := range []string{"manifest.json", "data.lua/docInfo.json", "parsed/docsis.json"} {
		if !names[want] {
			t.Errorf("missing %s (have %v)", want, names)
		}
	}
}
