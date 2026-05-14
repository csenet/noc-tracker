package web

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/csenet/noc-tracker/tracker"
)

//go:embed static/*
var staticFS embed.FS

// floorplanCandidates is the list of filenames `/floorplan` will try, in
// order. We accept JPG/PNG/WEBP/SVG so the user can drop whatever format
// they have without renaming.
var floorplanCandidates = []struct {
	name string
	mime string
}{
	{"floorplan.png", "image/png"},
	{"floorplan.jpg", "image/jpeg"},
	{"floorplan.jpeg", "image/jpeg"},
	{"floorplan.webp", "image/webp"},
	{"floorplan.svg", "image/svg+xml"},
}

type Server struct {
	store      *tracker.Store
	tracker    *tracker.Tracker
	positions  *tracker.PositionStore
	adminToken string
}

func NewServer(store *tracker.Store, tr *tracker.Tracker, positions *tracker.PositionStore, adminToken string) *Server {
	return &Server{store: store, tracker: tr, positions: positions, adminToken: adminToken}
}

// isAdmin reports whether the request is authorized for admin-only mutating
// endpoints. When NOC_ADMIN_TOKEN is unset the server runs in
// "everyone-is-admin" mode for backward compatibility — protection is opt-in.
func (s *Server) isAdmin(r *http.Request) bool {
	if s.adminToken == "" {
		return true
	}
	return r.Header.Get("X-Admin-Token") == s.adminToken
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/clients", s.handleClients)
	mux.HandleFunc("/api/registrations", s.handleRegistrations)
	mux.HandleFunc("/api/registrations/", s.handleDeleteRegistration)
	mux.HandleFunc("/api/me", s.handleMe)
	mux.HandleFunc("/api/aps", s.handleAPs)
	mux.HandleFunc("/api/ap-positions", s.handleAPPositions)
	mux.HandleFunc("/api/auth", s.handleAuth)
	mux.HandleFunc("/floorplan", s.handleFloorplan)

	sub, err := fs.Sub(staticFS, "static")
	if err == nil {
		mux.Handle("/", http.FileServer(http.FS(sub)))
	}
	return mux
}

type entry struct {
	MAC         string  `json:"mac"`
	Name        string  `json:"name"`
	Owner       string  `json:"owner,omitempty"`
	Registered  bool    `json:"registered"`
	Online      bool    `json:"online"`
	HostName    string  `json:"host_name,omitempty"`
	IPAddress   string  `json:"ip_address,omitempty"`
	AccessPoint string  `json:"access_point,omitempty"`
	SSID        string  `json:"ssid,omitempty"`
	Band        string  `json:"band,omitempty"`
	SignalDbm   int     `json:"signal_dbm,omitempty"`
	Source      string  `json:"source,omitempty"`
	SiteName    string  `json:"site_name,omitempty"`
	SeenAt      *string `json:"seen_at,omitempty"`
}

func sightingToEntry(s tracker.Sighting, reg tracker.Registration, registered bool) entry {
	e := entry{
		MAC:         s.MAC,
		HostName:    s.HostName,
		IPAddress:   s.IPAddress,
		AccessPoint: s.AccessPoint,
		SSID:        s.SSID,
		Band:        s.Band,
		SignalDbm:   s.SignalDbm,
		Source:      string(s.Source),
		SiteName:    s.SiteName,
		Online:      true,
		Registered:  registered,
	}
	if registered {
		e.Name = reg.Name
		e.Owner = reg.Owner
	}
	if !s.SeenAt.IsZero() {
		seen := s.SeenAt.UTC().Format(time.RFC3339)
		e.SeenAt = &seen
	}
	return e
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	last, lastErr, count := s.tracker.Status()
	writeJSON(w, http.StatusOK, map[string]any{
		"last_poll":  last,
		"last_error": lastErr,
		"clients":    count,
	})
}

// handleClients returns the merged view: every registered MAC (with its
// current sighting if any), and — only when ?all=true — every unregistered
// client currently online too. Default is registered-only because including
// unregistered devices ballooned the response to 400+ rows on a busy event
// network, which made the browser sluggish during refresh ticks.
func (s *Server) handleClients(w http.ResponseWriter, r *http.Request) {
	includeUnregistered := r.URL.Query().Get("all") == "true"

	regs := s.store.All()
	regByMAC := map[string]tracker.Registration{}
	for _, reg := range regs {
		regByMAC[reg.MAC] = reg
	}

	out := []entry{}
	seen := map[string]bool{}

	for _, reg := range regs {
		seen[reg.MAC] = true
		if sighting, ok := s.tracker.SightingByMAC(reg.MAC); ok {
			out = append(out, sightingToEntry(sighting, reg, true))
		} else {
			out = append(out, entry{
				MAC:        reg.MAC,
				Name:       reg.Name,
				Owner:      reg.Owner,
				Registered: true,
				Online:     false,
			})
		}
	}

	if includeUnregistered {
		for _, sighting := range s.tracker.All() {
			if seen[sighting.MAC] {
				continue
			}
			out = append(out, sightingToEntry(sighting, tracker.Registration{}, false))
		}
	}

	writeJSON(w, http.StatusOK, out)
}

type registerReq struct {
	MAC   string `json:"mac"`
	Name  string `json:"name"`
	Owner string `json:"owner"`
}

func (s *Server) handleRegistrations(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.store.All())
	case http.MethodPost:
		var req registerReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		reg, err := s.store.Register(req.MAC, strings.TrimSpace(req.Name), strings.TrimSpace(req.Owner))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, reg)
	default:
		w.Header().Set("Allow", "GET, POST")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleDeleteRegistration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", "DELETE")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	mac := strings.TrimPrefix(r.URL.Path, "/api/registrations/")
	if err := s.store.Delete(mac); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleMe identifies the requester by source IP, looks up the matching
// client, and returns their current AP/location. The IP can be overridden
// with ?ip= for testing.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	ip := r.URL.Query().Get("ip")
	if ip == "" {
		ip = clientIP(r)
	}

	sighting, ok := s.tracker.SightingByIP(ip)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{
			"ip":    ip,
			"found": false,
		})
		return
	}
	reg, registered := s.store.Get(sighting.MAC)
	writeJSON(w, http.StatusOK, map[string]any{
		"ip":      ip,
		"found":   true,
		"client":  sightingToEntry(sighting, reg, registered),
	})
}

// handleAPs returns the sorted list of AP names currently observed in any
// sighting — the floorplan editor uses this to know which dots to render
// even before any have been positioned.
func (s *Server) handleAPs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.tracker.KnownAPs())
}

func (s *Server) handleAPPositions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.positions.All())
	case http.MethodPost:
		if !s.isAdmin(r) {
			writeError(w, http.StatusUnauthorized, "admin token required")
			return
		}
		var body map[string]tracker.APPosition
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if err := s.positions.Replace(body); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, s.positions.All())
	default:
		w.Header().Set("Allow", "GET, POST")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleFloorplan serves whichever floorplan.* file the operator dropped
// into web/static or into $NOC_FLOORPLAN_DIR. The frontend always links to
// /floorplan (no extension) so it doesn't have to care about the format.
//
// We check the runtime override directory first so a containerised deploy
// can swap the floorplan via a volume mount without rebuilding the image.
func (s *Server) handleFloorplan(w http.ResponseWriter, r *http.Request) {
	if dir := os.Getenv("NOC_FLOORPLAN_DIR"); dir != "" {
		for _, c := range floorplanCandidates {
			data, err := os.ReadFile(filepath.Join(dir, c.name))
			if err == nil {
				w.Header().Set("Content-Type", c.mime)
				w.Header().Set("Cache-Control", "no-cache")
				_, _ = w.Write(data)
				return
			}
		}
	}
	for _, c := range floorplanCandidates {
		data, err := staticFS.ReadFile("static/" + c.name)
		if err == nil {
			w.Header().Set("Content-Type", c.mime)
			w.Header().Set("Cache-Control", "no-cache")
			_, _ = w.Write(data)
			return
		}
	}
	http.NotFound(w, r)
}

// handleAuth tells the UI two things: whether the deployment requires an
// admin token (so we can hide editing controls entirely on read-only setups),
// and whether the current request's token is accepted.
func (s *Server) handleAuth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{
		"admin_required": s.adminToken != "",
		"is_admin":       s.isAdmin(r),
	})
}

func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		parts := strings.Split(fwd, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
