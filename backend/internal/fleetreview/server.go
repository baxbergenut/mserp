package fleetreview

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

//go:embed web/*
var assets embed.FS

type Config struct {
	Origin, Directory, Username, PasswordHash string
	Secure                                    bool
	AllowApply                                bool
	Policy                                    Policy
}
type session struct {
	CSRF    string
	Expires time.Time
}
type Server struct {
	db       *DB
	cfg      Config
	mu       sync.Mutex
	state    State
	sessions map[string]session
	failures int
	window   time.Time
	applying bool
}

func NewServer(ctx context.Context, db *DB, cfg Config) (*Server, error) {
	if cfg.Origin == "" || cfg.Directory == "" || cfg.Username == "" || len(cfg.PasswordHash) < 50 {
		return nil, errors.New("portal origin, private state directory and credentials are required")
	}
	if !strings.HasPrefix(cfg.Origin, "https://") && cfg.Secure {
		return nil, errors.New("secure mode requires HTTPS origin")
	}
	if e := os.MkdirAll(cfg.Directory, 0700); e != nil {
		return nil, e
	}
	s := &Server{db: db, cfg: cfg, sessions: map[string]session{}}
	b, e := os.ReadFile(filepath.Join(cfg.Directory, "state.json"))
	if e == nil {
		e = json.Unmarshal(b, &s.state)
	} else if errors.Is(e, os.ErrNotExist) {
		var a, b Snapshot
		a, b, e = db.Snapshot(ctx)
		if e == nil {
			s.state = initial(a, b, cfg.Policy)
			e = s.save()
		}
	}
	if e != nil {
		return nil, e
	}
	if s.state.Batch != nil && s.state.Batch.State == "applying" {
		s.state.Batch.State = "needs_attention"
		s.state.Batch.Error = "The server restarted during submission. Recover this saved batch to verify both database receipts and finish any remaining commit."
		s.state.Version++
		if e = s.save(); e != nil {
			return nil, e
		}
	}
	return s, nil
}
func writePrivate(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".review-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(0600); e != nil {
		f.Close()
		return e
	}
	if _, e = f.Write(b); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if e = os.Rename(f.Name(), path); e != nil {
		return e
	}
	if runtime.GOOS != "windows" {
		dir, e := os.Open(filepath.Dir(path))
		if e != nil {
			return e
		}
		defer dir.Close()
		return dir.Sync()
	}
	return nil
}
func (s *Server) save() error {
	return writePrivate(filepath.Join(s.cfg.Directory, "state.json"), s.state)
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	sub, _ := fs.Sub(assets, "web")
	static := http.FileServer(http.FS(sub))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("GET /api/", s.api)
	mux.HandleFunc("POST /api/", s.api)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/drivers" || r.URL.Path == "/trucks" || r.URL.Path == "/review" {
			r.URL.Path = "/"
		}
		static.ServeHTTP(w, r)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if s.cfg.Secure {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			if r.Header.Get("Origin") != s.cfg.Origin || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				fail(w, 403, "Request origin or content type rejected")
				return
			}
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		mux.ServeHTTP(w, r)
	})
}
func jsonOut(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, message string) {
	jsonOut(w, status, map[string]string{"error": message})
}
func decode(r *http.Request, v any) error {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return errors.New("extra JSON")
	}
	return nil
}
func sessionKey(t string) string { h := sha256.Sum256([]byte(t)); return hex.EncodeToString(h[:]) }
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if decode(r, &in) != nil {
		fail(w, 400, "Invalid login request")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Since(s.window) > 15*time.Minute {
		s.window = time.Now()
		s.failures = 0
	}
	if s.failures >= 10 {
		w.Header().Set("Retry-After", "900")
		fail(w, 429, "Too many login attempts. Try again later.")
		return
	}
	e := bcrypt.CompareHashAndPassword([]byte(s.cfg.PasswordHash), []byte(in.Password))
	if e != nil || in.Username != s.cfg.Username {
		s.failures++
		fail(w, 401, "Incorrect username or password")
		return
	}
	s.failures = 0
	for key, v := range s.sessions {
		if time.Now().After(v.Expires) {
			delete(s.sessions, key)
		}
	}
	t := token()
	csrf := token()
	s.sessions[sessionKey(t)] = session{CSRF: csrf, Expires: time.Now().Add(8 * time.Hour)}
	http.SetCookie(w, &http.Cookie{Name: "fleet_review_session", Value: t, Path: "/", Secure: s.cfg.Secure, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 8 * 3600})
	jsonOut(w, 200, map[string]string{"csrf": csrf, "username": s.cfg.Username})
}
func (s *Server) api(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, e := r.Cookie("fleet_review_session")
	if e != nil {
		fail(w, 401, "Sign in to continue")
		return
	}
	session, ok := s.sessions[sessionKey(c.Value)]
	if !ok || time.Now().After(session.Expires) {
		fail(w, 401, "Session expired. Sign in again.")
		return
	}
	if r.Method != "GET" && r.Header.Get("X-CSRF-Token") != session.CSRF {
		fail(w, 403, "Request verification failed")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	switch r.Method + " " + r.URL.Path {
	case "GET /api/session":
		jsonOut(w, 200, map[string]string{"csrf": session.CSRF, "username": s.cfg.Username})
	case "POST /api/logout":
		delete(s.sessions, sessionKey(c.Value))
		http.SetCookie(w, &http.Cookie{Name: "fleet_review_session", Value: "", Path: "/", Secure: s.cfg.Secure, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
		jsonOut(w, 200, map[string]bool{"ok": true})
	case "GET /api/state":
		ready := ""
		if !s.cfg.AllowApply {
			ready = "Final application is disabled by the operator"
		} else if e := s.db.CheckApplyReady(ctx); e != nil {
			ready = publicError(e)
		}
		response := map[string]any{"state": s.state, "applyBlocker": ready, "applying": s.applying}
		if s.state.Batch != nil && s.state.Batch.State == "complete" {
			a, b, err := s.db.Snapshot(ctx)
			var links map[string]string
			var raw []byte
			if err == nil {
				err = s.db.Source.QueryRow(ctx, `SELECT identity_links FROM fleet_review.receipts WHERE batch_id=$1 AND company_id=$2`, s.state.Batch.ID, s.db.CompanyID).Scan(&raw)
			}
			if err == nil {
				err = json.Unmarshal(raw, &links)
			}
			if err != nil {
				fail(w, 503, "Batch receipts are saved, but current readback is unavailable. Retry before treating records as matching.")
				return
			}
			response["live"] = map[string]any{"source": a, "receiver": b, "links": links}
		}
		jsonOut(w, 200, response)
	case "POST /api/draft":
		if s.state.Batch != nil {
			fail(w, 409, "This batch is frozen. Finish or recover it before changing drafts.")
			return
		}
		var in struct {
			Version int `json:"version"`
			Row     Row `json:"row"`
		}
		if decode(r, &in) != nil {
			fail(w, 400, "Invalid draft")
			return
		}
		if in.Version != s.state.Version {
			fail(w, 409, "The review changed in another tab. Reload before saving.")
			return
		}
		found := -1
		for i, row := range s.state.Rows {
			if row.ID == in.Row.ID {
				found = i
				break
			}
		}
		if found < 0 {
			fail(w, 404, "Record not found")
			return
		}
		old := s.state.Rows[found]
		if in.Row.LocalID != old.LocalID || in.Row.Kind != old.Kind {
			fail(w, 400, "Source identity cannot change")
			return
		}
		clean := copyRecord(old.Values)
		keys := append(fields(old.Kind), "coverageEvidence", "coverageDecision", "reconcileCrew")
		for _, k := range keys {
			clean[k] = strings.TrimSpace(in.Row.Values[k])
		}
		for _, v := range clean {
			if len(v) > 1000 || strings.ContainsAny(v, "\x00\r\n") {
				fail(w, 400, "Field is too long or contains unsupported characters")
				return
			}
		}
		in.Row.Values = clean
		in.Row.Evidence = old.Evidence
		if len(in.Row.Note) > 2000 {
			fail(w, 400, "Note is too long")
			return
		}
		if in.Row.Reviewed {
			if e = validateRow(in.Row, s.state); e != nil {
				fail(w, 422, e.Error())
				return
			}
		}
		previous := s.state
		s.state.Rows = append([]Row(nil), s.state.Rows...)
		s.state.Rows[found] = in.Row
		s.state.Version++
		if e = s.save(); e != nil {
			s.state = previous
			fail(w, 500, "Draft could not be saved")
			return
		}
		jsonOut(w, 200, map[string]int{"version": s.state.Version})
	case "POST /api/refresh":
		if s.state.Batch != nil {
			fail(w, 409, "Finish or recover the frozen batch before refreshing")
			return
		}
		var in struct {
			Version int `json:"version"`
		}
		if decode(r, &in) != nil || in.Version != s.state.Version {
			fail(w, 409, "Reload the review before refreshing")
			return
		}
		a, b, e := s.db.Snapshot(ctx)
		if e != nil {
			fail(w, 503, "Could not read both systems. Existing drafts are retained.")
			return
		}
		next := initial(a, b, s.cfg.Policy)
		next.Version = s.state.Version + 1
		old := s.state
		for i, row := range next.Rows {
			for _, draft := range old.Rows {
				if draft.ID == row.ID {
					oldLocal, newLocal, oldRemote, newRemote := old.Source.Drivers, next.Source.Drivers, old.Receiver.Drivers, next.Receiver.Drivers
					if row.Kind == "truck" {
						oldLocal, newLocal, oldRemote, newRemote = old.Source.Trucks, next.Source.Trucks, old.Receiver.Trucks, next.Receiver.Trucks
					}
					unchanged := digest(find(oldLocal, row.LocalID)) == digest(find(newLocal, row.LocalID)) && digest(find(oldRemote, draft.RemoteID)) == digest(find(newRemote, draft.RemoteID))
					if !unchanged {
						draft.Reviewed = false
						draft.Evidence = "This record changed since review. Check the updated values before confirming again."
					}
					next.Rows[i] = draft
					break
				}
			}
		}
		s.state = next
		if e = s.save(); e != nil {
			s.state = old
			fail(w, 500, "Refresh could not be saved")
			return
		}
		jsonOut(w, 200, map[string]bool{"ok": true})
	case "POST /api/preflight":
		var in struct {
			Version int `json:"version"`
		}
		if decode(r, &in) != nil || in.Version != s.state.Version {
			fail(w, 409, "Reload before final review")
			return
		}
		if s.state.Batch != nil {
			fail(w, 409, "A batch already exists")
			return
		}
		if e = validateBatch(s.state); e != nil {
			fail(w, 422, e.Error())
			return
		}
		if !s.cfg.AllowApply {
			fail(w, 409, "Final application is disabled")
			return
		}
		if e = s.db.CheckApplyReady(ctx); e != nil {
			fail(w, 409, publicError(e))
			return
		}
		a, b, e := s.db.Snapshot(ctx)
		if e != nil {
			fail(w, 503, "Could not verify both systems")
			return
		}
		if digest(a) != digest(s.state.Source) || digest(b) != digest(s.state.Receiver) {
			fail(w, 409, "Live records changed. Refresh and review the updated values.")
			return
		}
		jsonOut(w, 200, map[string]string{"hash": batchHash(s.state)})
	case "POST /api/submit":
		var in struct {
			Version      int    `json:"version"`
			Hash         string `json:"hash"`
			Confirmation string `json:"confirmation"`
		}
		if decode(r, &in) != nil || in.Version != s.state.Version || in.Hash != batchHash(s.state) || in.Confirmation != "APPLY REVIEWED CHANGES" {
			fail(w, 409, "Review confirmation is missing or stale")
			return
		}
		if s.state.Batch != nil {
			jsonOut(w, 200, s.state.Batch)
			return
		}
		if !s.cfg.AllowApply {
			fail(w, 409, "Final application is disabled")
			return
		}
		if e = validateBatch(s.state); e != nil {
			fail(w, 422, e.Error())
			return
		}
		s.state.Batch = &Batch{ID: id(), Hash: in.Hash, State: "applying", Started: now()}
		s.state.Version++
		if e = s.save(); e != nil {
			s.state.Batch = nil
			s.state.Version--
			fail(w, 500, "Batch could not be saved; nothing was applied")
			return
		}
		s.startApply()
		jsonOut(w, 202, s.state.Batch)
	case "POST /api/retry":
		if !s.cfg.AllowApply || s.state.Batch == nil || s.applying || s.state.Batch.State == "complete" {
			fail(w, 409, "No recoverable batch")
			return
		}
		s.startApply()
		jsonOut(w, 202, s.state.Batch)
	case "POST /api/reset":
		if s.applying || s.state.Batch == nil || s.state.Batch.State == "complete" {
			fail(w, 409, "This batch cannot return to review")
			return
		}
		for _, pool := range []queryer{s.db.Source, s.db.Receiver} {
			var committed bool
			if e = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM fleet_review.receipts WHERE batch_id=$1 AND company_id=$2)`, s.state.Batch.ID, s.db.CompanyID).Scan(&committed); e != nil {
				fail(w, 503, "Cannot establish whether the batch committed; retain it for recovery")
				return
			}
			if committed {
				fail(w, 409, "A database already committed this batch. Recover the same batch; do not replace it.")
				return
			}
		}
		if e = writePrivate(filepath.Join(s.cfg.Directory, "batch-"+s.state.Batch.ID+".json"), s.state); e != nil {
			fail(w, 500, "Could not archive the uncommitted review")
			return
		}
		old := s.state
		s.state.Batch = nil
		s.state.Version++
		if e = s.save(); e != nil {
			s.state = old
			fail(w, 500, "Could not reopen review")
			return
		}
		jsonOut(w, 200, map[string]bool{"ok": true})
	default:
		fail(w, 404, "Not found")
	}
}
func publicError(e error) string {
	var pe *pgconn.PgError
	if errors.As(e, &pe) {
		return "Database validation rejected a change. Check duplicate identities, required fields, payroll locks and crew conflicts."
	}
	var ce *pgconn.ConnectError
	if errors.As(e, &ce) {
		return "A database connection is unavailable; retry the same batch after recovery."
	}
	return e.Error()
}
func (s *Server) startApply() {
	s.applying = true
	s.state.Batch.State = "applying"
	s.state.Batch.Error = ""
	snapshot := s.state
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		e := s.db.Apply(ctx, snapshot, s.cfg.Username)
		s.mu.Lock()
		defer s.mu.Unlock()
		s.applying = false
		if e != nil {
			s.state.Batch.State = "needs_attention"
			s.state.Batch.Error = publicError(e)
			slog.Warn("reconciliation batch needs attention", "batch", s.state.Batch.ID)
		} else {
			s.state.Batch.State = "complete"
			s.state.Batch.Finished = now()
			s.state.Batch.Error = ""
		}
		s.state.Version++
		if e = s.save(); e != nil {
			slog.Error("could not save batch result; durable database receipts must be checked", "batch", s.state.Batch.ID)
		}
	}()
}

func Credentials() (username, password, hash string, e error) {
	password = token()[:32]
	username = "reviewer"
	b, e := bcrypt.GenerateFromPassword([]byte(password), 12)
	return username, password, string(b), e
}
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.applying {
		return fmt.Errorf("a reconciliation batch is running")
	}
	return nil
}
