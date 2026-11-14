// Package server wires the HTTP API and the embedded frontend.
package server

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"time"

	"github.com/vinci7/sshud/internal/auth"
	"github.com/vinci7/sshud/internal/hub"
	"github.com/vinci7/sshud/internal/store"
)

// Options configures the HTTP server.
type Options struct {
	Demo    bool
	Version string
	UI      fs.FS // built frontend; index.html is served for unknown paths (SPA)
	Hub     *hub.Hub
	// Hosts are host names to answer to besides localhost and IP addresses.
	Hosts []string
	// Machines lists the configured machines (required).
	Machines Lister
	// Admin edits the machine list; nil makes it read-only (demo mode).
	Admin Admin
	// Auth guards the API; nil means no login at all (demo mode, --auth none).
	Auth *auth.Auth
}

// Lister lists the configured machines in display order.
type Lister interface {
	List() ([]store.Machine, error)
}

// Machine is a configured machine as shown to the dashboard. Users and
// ports appear only in the admin API.
type Machine struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Host string `json:"host"`
}

// New returns the root handler.
func New(opts Options) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/meta", func(w http.ResponseWriter, r *http.Request) {
		m := map[string]any{
			"version":  opts.Version,
			"demo":     opts.Demo,
			"readOnly": opts.Admin == nil,
			"auth":     "none",
			"loggedIn": true,
		}
		if a := opts.Auth; a != nil {
			need, _ := a.NeedsSetup()
			m["auth"], m["setup"], m["loggedIn"] = "password", need, a.Valid(r)
		}
		writeJSON(w, http.StatusOK, m)
	})
	mux.HandleFunc("GET /api/machines", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, opts.machines())
	})
	mux.HandleFunc("POST /api/machines/{id}/retry", func(w http.ResponseWriter, r *http.Request) {
		if opts.Hub == nil || !opts.Hub.Retry(r.PathValue("id")) {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/stream", func(w http.ResponseWriter, r *http.Request) {
		stream(w, r, opts)
	})
	registerAdmin(mux, opts)
	if opts.Auth != nil {
		registerSession(mux, opts.Auth)
	}
	mux.Handle("/", newStatic(opts.UI))
	return checkHost(opts.Hosts, securityHeaders(guardAPI(opts.Auth, mux)))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (o Options) machines() []Machine {
	list, err := o.Machines.List()
	if err != nil {
		log.Printf("list machines: %v", err)
	}
	out := make([]Machine, 0, len(list))
	for _, m := range list {
		out = append(out, Machine{ID: m.ID, Name: m.Name, Host: m.Host})
	}
	return out
}

// stream is a Server-Sent Events feed of hub updates.
//
//	GET /api/stream?level=overview            all machines
//	GET /api/stream?level=detail&id=<id>      one machine
func stream(w http.ResponseWriter, r *http.Request, opts Options) {
	flusher, ok := w.(http.Flusher)
	if !ok || opts.Hub == nil {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	level := hub.Overview
	if r.URL.Query().Get("level") == "detail" {
		level = hub.Detail
	}
	known := map[string]bool{}
	var ids []string
	for _, m := range opts.machines() {
		known[m.ID] = true
		ids = append(ids, m.ID)
	}
	if id := r.URL.Query().Get("id"); id != "" {
		if !known[id] {
			http.Error(w, "unknown machine", http.StatusNotFound)
			return
		}
		ids = []string{id}
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no") // let nginx pass events through unbuffered
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	sub := opts.Hub.Subscribe(ids, level)
	defer sub.Close()
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case u := <-sub.C:
			b, err := json.Marshal(u)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: update\ndata: %s\n\n", b)
			flusher.Flush()
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}
