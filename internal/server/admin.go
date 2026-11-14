package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/vinci7/sshud/internal/probe"
	"github.com/vinci7/sshud/internal/probeerr"
	"github.com/vinci7/sshud/internal/sshconfig"
	"github.com/vinci7/sshud/internal/sshsource"
	"github.com/vinci7/sshud/internal/store"
)

// Admin manages the machine list read through Options.Machines. It is nil
// in demo mode, which makes the management endpoints read-only.
type Admin interface {
	Add(store.Machine) (store.Machine, error)
	Update(store.Machine) error
	Delete(id string) error
	Test(ctx context.Context, m store.Machine) sshsource.TestResult
	// PublicKey is sshud's own key, the one in every authorized_keys line.
	PublicKey() (ssh.PublicKey, error)
	// Trust returns the recorded and presented host key fingerprints and
	// records the presented key if it is the confirmed one.
	Trust(ctx context.Context, id, confirmed string) (recorded, presented string, err error)
	// Bootstrap installs sshud's line with one-time credentials; see sshsource.
	Bootstrap(ctx context.Context, id string, c sshsource.Credentials) error
}

// probeErrorJSON reports a classified SSH failure, with its kind for the UI.
func probeErrorJSON(w http.ResponseWriter, err error) {
	kind, pe := probeerr.ErrInternal, (*probeerr.Error)(nil)
	if errors.As(err, &pe) {
		kind, err = pe.Kind, pe.Err
	}
	writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error(), "kind": string(kind)})
}

// machineDTO is the editable form of a machine. Line and Command are output
// only: the authorized_keys line for the machine and a command that installs
// it from the user's own computer.
type machineDTO struct {
	ID      string `json:"id,omitempty"`
	Name    string `json:"name"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	User    string `json:"user"`
	From    string `json:"from"`
	Line    string `json:"line,omitempty"`
	Command string `json:"command,omitempty"`
}

// withLines converts machines for the UI, adding their lines.
func withLines(a Admin, list ...store.Machine) ([]machineDTO, error) {
	key, err := a.PublicKey()
	if err != nil {
		return nil, err
	}
	out := make([]machineDTO, 0, len(list))
	for _, m := range list {
		line := probe.AuthorizedKeysLine(key, m.From)
		out = append(out, machineDTO{
			ID: m.ID, Name: m.Name, Host: m.Host, Port: m.Port, User: m.User, From: m.From,
			Line: line, Command: probe.InstallCommand(line, m.User, m.Host, m.Port),
		})
	}
	return out, nil
}

func (d machineDTO) machine() store.Machine {
	m := store.Machine{
		ID: d.ID, Name: strings.TrimSpace(d.Name), Host: strings.TrimSpace(d.Host), Port: d.Port,
		User: strings.TrimSpace(d.User), From: strings.TrimSpace(d.From),
	}
	if m.Port == 0 {
		m.Port = 22
	}
	return m
}

func errorJSON(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func readDTO(w http.ResponseWriter, r *http.Request) (machineDTO, bool) {
	var d machineDTO
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid JSON")
		return d, false
	}
	return d, true
}

func registerAdmin(mux *http.ServeMux, opts Options) {
	guard := func(h func(http.ResponseWriter, *http.Request, Admin)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if opts.Admin == nil {
				errorJSON(w, http.StatusForbidden, "machines cannot be changed in demo mode")
				return
			}
			h(w, r, opts.Admin)
		}
	}
	// reply sends saved machines with their lines; one is sent as an object
	reply := func(w http.ResponseWriter, a Admin, status int, list []store.Machine, one bool) {
		out, err := withLines(a, list...)
		switch {
		case err != nil:
			errorJSON(w, http.StatusInternalServerError, err.Error())
		case one:
			writeJSON(w, status, out[0])
		default:
			writeJSON(w, status, out)
		}
	}
	mux.HandleFunc("GET /api/admin/machines", guard(func(w http.ResponseWriter, r *http.Request, a Admin) {
		list, err := opts.Machines.List()
		if err != nil {
			errorJSON(w, http.StatusInternalServerError, err.Error())
			return
		}
		reply(w, a, http.StatusOK, list, false)
	}))
	mux.HandleFunc("POST /api/admin/machines", guard(func(w http.ResponseWriter, r *http.Request, a Admin) {
		d, ok := readDTO(w, r)
		if !ok {
			return
		}
		m, err := a.Add(d.machine())
		if err != nil {
			errorJSON(w, http.StatusBadRequest, err.Error())
			return
		}
		reply(w, a, http.StatusCreated, []store.Machine{m}, true)
	}))
	mux.HandleFunc("PUT /api/admin/machines/{id}", guard(func(w http.ResponseWriter, r *http.Request, a Admin) {
		d, ok := readDTO(w, r)
		if !ok {
			return
		}
		d.ID = r.PathValue("id")
		m := d.machine()
		if err := a.Update(m); errors.Is(err, store.ErrNotFound) {
			errorJSON(w, http.StatusNotFound, err.Error())
			return
		} else if err != nil {
			errorJSON(w, http.StatusBadRequest, err.Error())
			return
		}
		reply(w, a, http.StatusOK, []store.Machine{m}, true)
	}))
	mux.HandleFunc("DELETE /api/admin/machines/{id}", guard(func(w http.ResponseWriter, r *http.Request, a Admin) {
		if err := a.Delete(r.PathValue("id")); errors.Is(err, store.ErrNotFound) {
			errorJSON(w, http.StatusNotFound, err.Error())
			return
		} else if err != nil {
			errorJSON(w, http.StatusInternalServerError, err.Error())
			return
		}
		if opts.Hub != nil {
			opts.Hub.Forget(r.PathValue("id"))
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	// trust: without a fingerprint, only compare host keys; with the presented
	// one confirmed, record it and probe again
	mux.HandleFunc("POST /api/admin/machines/{id}/trust", guard(func(w http.ResponseWriter, r *http.Request, a Admin) {
		var body struct{ Fingerprint string }
		r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
		json.NewDecoder(r.Body).Decode(&body)
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		id := r.PathValue("id")
		recorded, presented, err := a.Trust(ctx, id, body.Fingerprint)
		if err != nil {
			errorJSON(w, http.StatusBadGateway, err.Error())
			return
		}
		trusted := presented == body.Fingerprint
		if trusted && opts.Hub != nil {
			opts.Hub.Retry(id)
		}
		writeJSON(w, http.StatusOK, map[string]any{"recorded": recorded, "presented": presented, "trusted": trusted})
	}))
	// bootstrap: log in once with the user's password or key and install
	// sshud's line. The credentials are used for this request only.
	mux.HandleFunc("POST /api/admin/machines/{id}/bootstrap", guard(func(w http.ResponseWriter, r *http.Request, a Admin) {
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
		defer clear(raw) // the decoded strings cannot be wiped; the raw body can
		var c struct{ Password, PrivateKey, Passphrase string }
		if err != nil || json.Unmarshal(raw, &c) != nil || (c.Password == "") == (c.PrivateKey == "") {
			errorJSON(w, http.StatusBadRequest, "send either a password or a private key")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
		defer cancel()
		id := r.PathValue("id")
		if err := a.Bootstrap(ctx, id, sshsource.Credentials{Password: c.Password, PrivateKey: c.PrivateKey, Passphrase: c.Passphrase}); err != nil {
			probeErrorJSON(w, err)
			return
		}
		if opts.Hub != nil {
			opts.Hub.Retry(id)
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("POST /api/admin/import/preview", guard(func(w http.ResponseWriter, r *http.Request, a Admin) {
		var body struct {
			Text string `json:"text"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errorJSON(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		existing, err := opts.Machines.List()
		if err != nil {
			errorJSON(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, preview(sshconfig.Parse(body.Text), existing))
	}))
	mux.HandleFunc("POST /api/admin/test", guard(func(w http.ResponseWriter, r *http.Request, a Admin) {
		d, ok := readDTO(w, r)
		if !ok {
			return
		}
		m := d.machine()
		if err := store.Validate(m); err != nil {
			errorJSON(w, http.StatusBadRequest, err.Error())
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		writeJSON(w, http.StatusOK, a.Test(ctx, m))
	}))
}

type importRow struct {
	sshconfig.Entry
	// Note explains why a row is not selected by default: "exists",
	// "proxyjump" or "nouser". Empty means ready to import.
	Note string `json:"note,omitempty"`
}

func preview(r sshconfig.Result, existing []store.Machine) map[string]any {
	names := map[string]bool{}
	for _, m := range existing {
		names[m.Name] = true
	}
	rows := make([]importRow, 0, len(r.Entries))
	for _, e := range r.Entries {
		row := importRow{Entry: e}
		switch {
		case names[e.Name]:
			row.Note = "exists"
		case e.ProxyJmp != "":
			row.Note = "proxyjump" // jump hosts are not supported yet
		case e.User == "":
			row.Note = "nouser"
		}
		rows = append(rows, row)
	}
	return map[string]any{"rows": rows, "skipped": r.Skipped}
}
