// Command sshud serves a web dashboard of Linux machines read over SSH.
//
//	sshud [--listen addr] [--data dir] [--hosts names] [--demo]
//	sshud --version
//	sshud machine add --name N --host H [--port P] --user U
//	sshud machine list
//	sshud machine rm NAME
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/vinci7/sshud/internal/auth"
	"github.com/vinci7/sshud/internal/demo"
	"github.com/vinci7/sshud/internal/hub"
	"github.com/vinci7/sshud/internal/server"
	"github.com/vinci7/sshud/internal/sshsource"
	"github.com/vinci7/sshud/internal/store"
	"github.com/vinci7/sshud/web"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "machine" {
		if err := machineCmd(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}
	if err := serve(); err != nil {
		log.Fatal(err)
	}
}

// serve runs the dashboard until SIGINT or SIGTERM, then stops the HTTP
// server before closing the SSH connections and the database.
func serve() error {
	listen := flag.String("listen", "127.0.0.1:8080", "HTTP listen address")
	dataDir := flag.String("data", envOr("SSHUD_DATA", "data"), "data directory (database, keys); env SSHUD_DATA")
	demoMode := flag.Bool("demo", false, "serve built-in sample machines; no SSH needed")
	authMode := flag.String("auth", envOr("SSHUD_AUTH", "password"), "password, or none when a reverse proxy already authenticates users; env SSHUD_AUTH")
	hosts := flag.String("hosts", os.Getenv("SSHUD_HOSTS"), "comma-separated host names sshud is reached by, besides localhost and IP addresses; env SSHUD_HOSTS")
	var showVersion bool
	flag.BoolVar(&showVersion, "version", false, "print the version and exit")
	flag.BoolVar(&showVersion, "v", false, "shorthand for --version")
	flag.Parse()
	if showVersion {
		fmt.Println("sshud", version)
		return nil
	}

	opts := server.Options{Demo: *demoMode, Version: version, UI: web.Dist()}
	for _, h := range strings.Split(*hosts, ",") {
		if h = strings.TrimSpace(h); h != "" {
			opts.Hosts = append(opts.Hosts, h)
		}
	}
	if *demoMode {
		n, _ := strconv.Atoi(os.Getenv("SSHUD_DEMO_MACHINES"))
		src := demo.New(demo.Fleet(n))
		opts.Hub, opts.Machines = hub.New(src), src
	} else {
		st, err := store.Open(*dataDir)
		if err != nil {
			return fmt.Errorf("open data directory %s: %w", *dataDir, err)
		}
		defer st.Close()
		src := sshsource.New(st, *dataDir)
		defer src.Close()
		go func() {
			for range time.Tick(time.Minute) {
				src.CloseIdle()
			}
		}()
		opts.Hub = hub.New(src)
		a := admin{st, src}
		opts.Admin, opts.Machines = a, a
		switch *authMode {
		case "password":
			opts.Auth = auth.New(st)
			if pw := os.Getenv("SSHUD_PASSWORD"); pw != "" {
				if err := opts.Auth.EnsurePassword(pw); err != nil {
					return fmt.Errorf("SSHUD_PASSWORD: %w", err)
				}
			}
			if need, _ := opts.Auth.NeedsSetup(); need {
				log.Printf("no password set yet: the first visitor will be asked to create one (or set SSHUD_PASSWORD)")
			}
		case "none":
			log.Printf("auth=none: anyone who can reach %s can manage machines; only use this behind an authenticating proxy", *listen)
		default:
			return fmt.Errorf("--auth must be password or none, got %q", *authMode)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	srv := &http.Server{
		Addr:              *listen,
		Handler:           server.New(opts),
		ReadHeaderTimeout: 10 * time.Second,
		// requests see the signal too, so open event streams end at once
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	log.Printf("sshud %s listening on http://%s (demo=%v)", version, *listen, *demoMode)
	log.Printf("answering to host names: localhost, *.localhost, IP addresses%s (add more with SSHUD_HOSTS)", strings.Join(append([]string{""}, opts.Hosts...), ", "))
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Printf("shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(sctx)
}

func machineCmd(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: sshud machine add|list|rm")
	}
	fs := flag.NewFlagSet("machine "+args[0], flag.ExitOnError)
	dataDir := fs.String("data", envOr("SSHUD_DATA", "data"), "data directory")
	var m store.Machine
	if args[0] == "add" {
		fs.StringVar(&m.Name, "name", "", "display name")
		fs.StringVar(&m.Host, "host", "", "host name or IP")
		fs.IntVar(&m.Port, "port", 22, "SSH port")
		fs.StringVar(&m.User, "user", "", "SSH user")
	}
	fs.Parse(args[1:])
	st, err := store.Open(*dataDir)
	if err != nil {
		return err
	}
	defer st.Close()

	switch args[0] {
	case "add":
		added, err := st.AddMachine(m)
		if err != nil {
			return err
		}
		fmt.Printf("added %s (%s@%s:%d)\n", added.Name, added.User, added.Host, added.Port)
	case "list":
		list, err := st.Machines()
		if err != nil {
			return err
		}
		for _, m := range list {
			fmt.Printf("%-20s %s@%s:%d\n", m.Name, m.User, m.Host, m.Port)
		}
	case "rm":
		list, err := st.Machines()
		if err != nil {
			return err
		}
		for _, x := range list {
			if x.Name == fs.Arg(0) {
				return st.DeleteMachine(x.ID)
			}
		}
		return fmt.Errorf("no machine named %q", fs.Arg(0))
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
	return nil
}

// admin implements server.Admin on the store and the SSH source, whose
// Test, Trust, HostKey and Bootstrap it uses as they are.
type admin struct {
	st *store.Store
	*sshsource.Source
}

func (a admin) List() ([]store.Machine, error)             { return a.st.Machines() }
func (a admin) Add(m store.Machine) (store.Machine, error) { return a.st.AddMachine(m) }
func (a admin) Update(m store.Machine) error               { return a.st.UpdateMachine(m) }
func (a admin) Delete(id string) error                     { return a.st.DeleteMachine(id) }
func (a admin) PublicKey() (ssh.PublicKey, error) {
	s, err := a.StrictSigner()
	if err != nil {
		return nil, err
	}
	return s.PublicKey(), nil
}
