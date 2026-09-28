// Package docsis reads DOCSIS signal data (channel levels, SNR/MER,
// codeword errors) from a cable FRITZ!Box and rates it.
//
// Login and query follow DOCSight by Dennis Braun (MIT License,
// https://github.com/itsDNNS/docsight): login_sid.lua with PBKDF2 (MD5
// fallback) and the data.lua page "docInfo".
package docsis

import (
	"crypto/md5"
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
)

// Client talks to one FRITZ!Box. It keeps the session ID and logs in again
// when the box rejects it. Safe for concurrent use.
type Client struct {
	BaseURL  string // e.g. http://192.168.178.1
	User     string // may be empty: the box's last used user is taken
	Password string

	mu   sync.Mutex
	sid  string
	http *http.Client
}

// NewClient creates a client for the given box.
func NewClient(baseURL, user, password string) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL != "" && !strings.Contains(baseURL, "://") {
		baseURL = "http://" + baseURL
	}
	return &Client{BaseURL: baseURL, User: user, Password: password, http: &http.Client{Timeout: 10 * time.Second}}
}

type sessionInfo struct {
	SID       string `xml:"SID"`
	Challenge string `xml:"Challenge"`
	BlockTime int    `xml:"BlockTime"`
	Users     []struct {
		Name string `xml:",chardata"`
		Last int    `xml:"last,attr"`
	} `xml:"Users>User"`
}

func (c *Client) getSessionInfo(q url.Values) (sessionInfo, error) {
	var si sessionInfo
	resp, err := c.http.Get(c.BaseURL + "/login_sid.lua?" + q.Encode())
	if err != nil {
		return si, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return si, fmt.Errorf("login_sid.lua: HTTP %d", resp.StatusCode)
	}
	err = xml.NewDecoder(resp.Body).Decode(&si)
	return si, err
}

// challengeResponse answers the login challenge (PBKDF2 "2$…" or MD5).
func challengeResponse(challenge, password string) (string, error) {
	if strings.HasPrefix(challenge, "2$") {
		p := strings.Split(challenge, "$")
		if len(p) != 5 {
			return "", fmt.Errorf("unerwartete Login-Challenge %q", challenge)
		}
		iter1, err1 := strconv.Atoi(p[1])
		salt1, err2 := hex.DecodeString(p[2])
		iter2, err3 := strconv.Atoi(p[3])
		salt2, err4 := hex.DecodeString(p[4])
		if err := errors.Join(err1, err2, err3, err4); err != nil {
			return "", fmt.Errorf("Login-Challenge: %w", err)
		}
		h1, err := pbkdf2.Key(sha256.New, password, salt1, iter1, 32)
		if err != nil {
			return "", err
		}
		h2, err := pbkdf2.Key(sha256.New, string(h1), salt2, iter2, 32)
		if err != nil {
			return "", err
		}
		return p[4] + "$" + hex.EncodeToString(h2), nil
	}
	// Legacy MD5 over UTF-16LE "challenge-password".
	u := utf16.Encode([]rune(challenge + "-" + password))
	b := make([]byte, 0, len(u)*2)
	for _, r := range u {
		b = append(b, byte(r), byte(r>>8))
	}
	sum := md5.Sum(b)
	return challenge + "-" + hex.EncodeToString(sum[:]), nil
}

// Login authenticates and stores the session ID.
func (c *Client) Login() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.loginLocked()
}

func (c *Client) loginLocked() error {
	if c.BaseURL == "" {
		return errors.New("keine FRITZ!Box-Adresse eingestellt")
	}
	si, err := c.getSessionInfo(url.Values{"version": {"2"}})
	if err != nil {
		return fmt.Errorf("FRITZ!Box nicht erreichbar: %w", err)
	}
	user := c.User
	if user == "" {
		for _, u := range si.Users {
			if u.Last == 1 || user == "" {
				user = u.Name
			}
		}
	}
	resp, err := challengeResponse(si.Challenge, c.Password)
	if err != nil {
		return err
	}
	si2, err := c.getSessionInfo(url.Values{"version": {"2"}, "username": {user}, "response": {resp}})
	if err != nil {
		return err
	}
	if si2.SID == "" || si2.SID == "0000000000000000" {
		if si2.BlockTime > 0 {
			return fmt.Errorf("FRITZ!Box-Anmeldung fehlgeschlagen (Benutzer %q), Box sperrt weitere Versuche für %d s", user, si2.BlockTime)
		}
		return fmt.Errorf("FRITZ!Box-Anmeldung fehlgeschlagen (Benutzer %q) – Benutzername/Kennwort prüfen", user)
	}
	c.sid = si2.SID
	return nil
}

// dataPage fetches a data.lua page ("docInfo", …) and returns its "data"
// object. An expired session is renewed once.
func (c *Client) dataPage(page string) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for attempt := 0; attempt < 2; attempt++ {
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
		body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		var env struct {
			Data json.RawMessage `json:"data"`
		}
		if resp.StatusCode == http.StatusOK && json.Unmarshal(body, &env) == nil && len(env.Data) > 2 {
			return env.Data, nil
		}
		// Invalid or expired session: the box answers with a login page.
		c.sid = ""
	}
	return nil, fmt.Errorf("FRITZ!Box lieferte keine Daten für %q", page)
}

// Model reads the model name without login (TR-064 description).
func (c *Client) Model() string {
	resp, err := c.http.Get(c.BaseURL + "/tr064/tr64desc.xml")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var d struct {
		Model    string `xml:"device>modelName"`
		Friendly string `xml:"device>friendlyName"`
		Version  string `xml:"systemVersion>Display"`
	}
	if xml.NewDecoder(resp.Body).Decode(&d) != nil {
		return ""
	}
	m := d.Model
	if m == "" {
		m = d.Friendly
	}
	if d.Version != "" {
		m += " (FRITZ!OS " + d.Version + ")"
	}
	return m
}

// Fetch reads and rates the current DOCSIS data.
func (c *Client) Fetch() (Snapshot, error) {
	raw, err := c.dataPage("docInfo")
	if err != nil {
		return Snapshot{}, err
	}
	return Parse(raw)
}
