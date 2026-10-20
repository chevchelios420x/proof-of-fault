package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The web server delivers the user interface and the API. Every request
// except /api/ping and /api/login needs the access key (a UUID created at
// the first start): as session cookie after login or as "Authorization:
// Bearer <key>". State-changing requests additionally need the header
// "X-POF: 1", which browsers do not send cross-site without a CORS
// preflight (that is never allowed) – protection against CSRF.

const cookieName = "pof_session"

// server wires the App to HTTP.
type server struct {
	app    *App
	cfg    *agentConfig
	hub    *hub
	ui     fs.FS
	stop   func() // ends the process (update / uninstall)
	tls    bool
	method map[string]reflect.Value
}

// rpcHidden are methods that must not be called through /api/call.
var rpcHidden = map[string]bool{}

func newServer(app *App, cfg *agentConfig, h *hub, ui fs.FS, stop func()) *server {
	s := &server{app: app, cfg: cfg, hub: h, ui: ui, stop: stop, tls: cfg.TLS, method: map[string]reflect.Value{}}
	v := reflect.ValueOf(app)
	t := v.Type()
	for i := 0; i < t.NumMethod(); i++ {
		m := t.Method(i)
		if !rpcHidden[m.Name] {
			s.method[m.Name] = v.Method(i)
		}
	}
	return s
}

func (s *server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/ping", s.ping)
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("POST /api/logout", s.logout)
	mux.HandleFunc("GET /api/auth", s.auth(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	mux.HandleFunc("POST /api/call/{name}", s.auth(s.call))
	mux.HandleFunc("GET /api/events", s.auth(s.events))
	mux.HandleFunc("GET /api/export", s.auth(s.export))
	mux.HandleFunc("POST /api/import", s.auth(s.importFile))
	mux.HandleFunc("GET /api/report/period", s.auth(s.period))
	mux.HandleFunc("POST /api/fritzdebug", s.auth(s.fritzDebug))
	mux.HandleFunc("POST /api/shutdown", s.auth(s.shutdown))
	mux.HandleFunc("/", s.static)
	return securityHeaders(mux)
}

func securityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		h.ServeHTTP(w, r)
	})
}

func (s *server) authorized(r *http.Request) bool {
	key := ""
	if c, err := r.Cookie(cookieName); err == nil {
		key = c.Value
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		key = strings.TrimPrefix(h, "Bearer ")
	}
	return key != "" && subtle.ConstantTimeCompare([]byte(key), []byte(s.cfg.Token)) == 1
}

func (s *server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			jsonError(w, http.StatusUnauthorized, "Anmeldung erforderlich")
			return
		}
		if r.Method != http.MethodGet && r.Header.Get("X-POF") != "1" {
			jsonError(w, http.StatusForbidden, "fehlender Header X-POF")
			return
		}
		next(w, r)
	}
}

func jsonError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

func (s *server) ping(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"app": "proof-of-fault", "version": Version})
}

// loginLimiter slows down guessing of the access key.
var loginLimiter = struct {
	sync.Mutex
	fails map[string][]time.Time
}{fails: map[string][]time.Time{}}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	loginLimiter.Lock()
	recent := loginLimiter.fails[ip][:0]
	for _, t := range loginLimiter.fails[ip] {
		if time.Since(t) < 10*time.Minute {
			recent = append(recent, t)
		}
	}
	loginLimiter.fails[ip] = recent
	blocked := len(recent) >= 10
	loginLimiter.Unlock()
	if blocked {
		jsonError(w, http.StatusTooManyRequests, "zu viele Fehlversuche – bitte 10 Minuten warten")
		return
	}
	var req struct {
		Token string `json:"token"`
	}
	json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req)
	tok := strings.TrimSpace(req.Token)
	if subtle.ConstantTimeCompare([]byte(tok), []byte(s.cfg.Token)) != 1 {
		loginLimiter.Lock()
		loginLimiter.fails[ip] = append(loginLimiter.fails[ip], time.Now())
		loginLimiter.Unlock()
		time.Sleep(500 * time.Millisecond)
		jsonError(w, http.StatusUnauthorized, "Zugangsschlüssel falsch")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: s.cfg.Token, Path: "/", HttpOnly: true,
		Secure: s.tls, SameSite: http.SameSiteStrictMode, MaxAge: 400 * 24 * 3600})
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.tls, SameSite: http.SameSiteStrictMode})
	w.WriteHeader(http.StatusNoContent)
}

var errType = reflect.TypeOf((*error)(nil)).Elem()

// call invokes an exported App method. The body is a JSON array with the
// arguments; the answer is {"result": …} or {"error": …}.
func (s *server) call(w http.ResponseWriter, r *http.Request) {
	m, ok := s.method[r.PathValue("name")]
	if !ok {
		jsonError(w, http.StatusNotFound, "unbekannte Funktion")
		return
	}
	var raw []json.RawMessage
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&raw); err != nil && err != io.EOF {
		jsonError(w, http.StatusBadRequest, "ungültige Argumente")
		return
	}
	t := m.Type()
	if len(raw) != t.NumIn() {
		jsonError(w, http.StatusBadRequest, fmt.Sprintf("%d Argumente erwartet", t.NumIn()))
		return
	}
	args := make([]reflect.Value, t.NumIn())
	for i := range args {
		p := reflect.New(t.In(i))
		if err := json.Unmarshal(raw[i], p.Interface()); err != nil {
			jsonError(w, http.StatusBadRequest, fmt.Sprintf("Argument %d: %v", i+1, err))
			return
		}
		args[i] = p.Elem()
	}
	out := m.Call(args)
	var result any
	for _, v := range out {
		if v.Type().Implements(errType) && v.Type() == errType {
			if !v.IsNil() {
				jsonError(w, http.StatusInternalServerError, v.Interface().(error).Error())
				return
			}
			continue
		}
		result = v.Interface()
	}
	writeJSON(w, map[string]any{"result": result})
}

// events streams monitor events as server-sent events.
func (s *server) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		jsonError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	ch := s.hub.subscribe()
	defer s.hub.unsubscribe(ch)
	fmt.Fprint(w, "retry: 3000\n\n")
	fl.Flush()
	keep := time.NewTicker(20 * time.Second)
	defer keep.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			w.Write(msg)
			fl.Flush()
		case <-keep.C:
			fmt.Fprint(w, ": keep-alive\n\n")
			fl.Flush()
		}
	}
}

func attachment(w http.ResponseWriter, name, ctype string) {
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("Cache-Control", "no-store")
}

func (s *server) export(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "id fehlt")
		return
	}
	format := r.URL.Query().Get("format")
	if err := s.app.ready(); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	name, err := s.app.exportName(id, format)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Render into memory first so errors still produce a proper status.
	var buf bytes.Buffer
	if err := s.app.export(&buf, id, format); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	ctype := map[string]string{"html": "text/html; charset=utf-8", "csv": "text/csv; charset=utf-8",
		"events": "text/csv; charset=utf-8", "pof": "application/zip"}[format]
	if format == "html" && r.URL.Query().Get("view") == "1" {
		w.Header().Set("Content-Type", ctype)
	} else {
		attachment(w, name, ctype)
	}
	w.Write(buf.Bytes())
}

func (s *server) importFile(w http.ResponseWriter, r *http.Request) {
	b, err := io.ReadAll(io.LimitReader(r.Body, 4<<30))
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, err := s.app.importSession(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, map[string]any{"result": id})
}

func (s *server) period(w http.ResponseWriter, r *http.Request) {
	var buf bytes.Buffer
	if err := s.app.periodReport(&buf); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if r.URL.Query().Get("download") == "1" {
		attachment(w, "proof-of-fault_nachweis_"+time.Now().Format("2006-01-02_1504")+".html", "text/html; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}
	w.Write(buf.Bytes())
}

func (s *server) fritzDebug(w http.ResponseWriter, r *http.Request) {
	var req struct{ URL, User, Password string }
	json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&req)
	zb, err := s.app.fritzDebug(req.URL, req.User, req.Password)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	attachment(w, "fritzbox-debug_"+time.Now().Format("2006-01-02_1504")+".zip", "application/zip")
	w.Write(zb)
}

func (s *server) shutdown(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
	go func() {
		time.Sleep(200 * time.Millisecond)
		s.stop()
	}()
}

// static serves the built user interface; unknown paths get index.html.
func (s *server) static(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, "/")
	if p == "" {
		p = "index.html"
	}
	if _, err := fs.Stat(s.ui, p); err != nil {
		p = "index.html"
	}
	if p == "index.html" {
		w.Header().Set("Cache-Control", "no-store")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable") // hashed asset names
	}
	http.ServeFileFS(w, r, s.ui, p)
}

// hub fans events out to all connected browsers.
type hub struct {
	mu   sync.Mutex
	subs map[chan []byte]bool
}

func newHub() *hub { return &hub{subs: map[chan []byte]bool{}} }

func (h *hub) subscribe() chan []byte {
	ch := make(chan []byte, 256)
	h.mu.Lock()
	h.subs[ch] = true
	h.mu.Unlock()
	return ch
}

func (h *hub) unsubscribe(ch chan []byte) {
	h.mu.Lock()
	delete(h.subs, ch)
	h.mu.Unlock()
}

// emit sends an event to every browser; slow ones drop messages instead of
// blocking the measurement.
func (h *hub) emit(ev string, data any) {
	b, err := json.Marshal(data)
	if err != nil {
		return
	}
	msg := []byte("event: " + ev + "\ndata: " + string(b) + "\n\n")
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- msg:
		default:
		}
	}
}

// serve runs the web server until ctx ends.
func serve(ctx context.Context, s *server, cfg *agentConfig, dir string) error {
	srv := &http.Server{Addr: cfg.Listen, Handler: s.handler(), ReadHeaderTimeout: 10 * time.Second}
	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return fmt.Errorf("Webserver %s: %w", cfg.Listen, err)
	}
	go func() {
		<-ctx.Done()
		sc, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		srv.Shutdown(sc)
	}()
	log.Printf("Weboberfläche: %s", cfg.URL(""))
	if cfg.TLS {
		cert, key, err := ensureCert(dir)
		if err != nil {
			return err
		}
		err = srv.ServeTLS(ln, cert, key)
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
	if err := srv.Serve(ln); err != http.ErrServerClosed {
		return err
	}
	return nil
}
