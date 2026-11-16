// savant-ha runs on a Savant host and serves its devices to the Home Assistant integration.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"savant-ha/internal/agent"
	"savant-ha/internal/api"
	"savant-ha/internal/config"
	"savant-ha/internal/transport"
)

var version = "dev"

const usage = `savant-ha: Savant host agent for Home Assistant.

Usage:
  savant-ha [flags] serve                         run the API for Home Assistant
  savant-ha [flags] discover                      print what the agent finds (read-only)
  savant-ha [flags] read STATE...                 read states
  savant-ha [flags] request SERVICE REQUEST [ARG VALUE]...
                                                  SERVICE: "Zone-Component-Logical-Variant-SVC_TYPE"
  savant-ha version

Flags:
`

func main() {
	fs := flag.NewFlagSet("savant-ha", flag.ExitOnError)
	cfgPath := fs.String("config", "", "config file (JSON)")
	pidFile := fs.String("pidfile", "", "serve: write the process id here")
	logLevel := fs.String("log-level", "", "debug, info, warn, error")
	withStates := fs.Bool("statenames", false, "discover: include every state name (slow)")
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage); fs.PrintDefaults() }
	_ = fs.Parse(os.Args[1:])

	cmd, args := "serve", fs.Args()
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	if cmd == "version" {
		fmt.Printf("savant-ha %s %s/%s\n", version, runtime.GOOS, runtime.GOARCH)
		return
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fatal(err)
	}
	if *logLevel != "" {
		cfg.LogLevel = *logLevel
	}
	log := newLogger(cfg.LogLevel)
	slog.SetDefault(log)

	tr, err := transport.NewSclibridge(transport.SclibridgeOptions{
		Path:        cfg.Sclibridge,
		Timeout:     cfg.Timeout.D(),
		Concurrency: cfg.Concurrency,
		Env:         cfg.Env,
		Logger:      log,
	})
	if err != nil {
		fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch cmd {
	case "serve":
		err = serve(ctx, cfg, tr, *pidFile, log)
	case "discover":
		err = discover(ctx, cfg, tr, *withStates, log)
	case "read":
		err = read(ctx, tr, args)
	case "request":
		err = request(ctx, tr, args)
	default:
		fs.Usage()
		os.Exit(2)
	}
	if err != nil {
		fatal(err)
	}
}

func serve(ctx context.Context, cfg config.Config, tr *transport.Sclibridge, pidFile string, log *slog.Logger) error {
	if cfg.Token == "" {
		return errors.New("serve: config needs a token")
	}
	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.Listen, err)
	}
	if pidFile != "" {
		if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
			return err
		}
		defer os.Remove(pidFile)
	}
	log.Info("savant-ha agent starting", "version", version, "listen", cfg.Listen, "sclibridge", tr.Path())

	eng := agent.NewEngine(tr, cfg, version, log)
	srv := &http.Server{Handler: api.New(eng, cfg.Token, log).Handler(), ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	go eng.Run(ctx)

	select {
	case <-ctx.Done():
	case err := <-errc:
		return err
	}
	log.Info("shutting down")
	shut, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return srv.Shutdown(shut)
}

func discover(ctx context.Context, cfg config.Config, tr transport.Transport, withStates bool, log *slog.Logger) error {
	inv, err := agent.Collect(ctx, tr, cfg, version, withStates, log)
	if err != nil {
		return err
	}
	ents, skipped := agent.Build(inv, log)
	vals, err := tr.ReadStates(ctx, statesOf(ents))
	if err != nil {
		return err
	}
	type item struct {
		*agent.Meta
		State map[string]any `json:"state"`
	}
	var items []item
	for _, e := range ents {
		m := *e.Info()
		if len(m.Secret) > 0 {
			m.Secret = map[string]string{"hidden": strings.Join(sortedKeys(m.Secret), ",")}
		}
		items = append(items, item{&m, e.State(vals)})
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(map[string]any{"inventory": inv, "entities": items, "skipped": skipped})
}

func statesOf(ents []agent.Entity) []string {
	var out []string
	for _, e := range ents {
		out = append(out, e.States()...)
	}
	return out
}

func read(ctx context.Context, tr transport.Transport, names []string) error {
	if len(names) == 0 {
		return errors.New("read: give one or more state names")
	}
	vals, err := tr.ReadStates(ctx, names)
	if err != nil {
		return err
	}
	for _, n := range names {
		fmt.Printf("%s = %q\n", n, vals[n])
	}
	return nil
}

func request(ctx context.Context, tr transport.Transport, args []string) error {
	if len(args) < 2 {
		return errors.New(`request: need "Zone-Component-Logical-Variant-SVC_TYPE" REQUEST [ARG VALUE]...`)
	}
	zones, err := tr.ListZones(ctx)
	if err != nil {
		return err
	}
	var svc transport.Service
	found := false
	for _, z := range zones {
		if s, err := transport.ParseServiceLine(z, args[0], nil); err == nil {
			svc, found = s, true
			break
		}
	}
	if !found {
		return fmt.Errorf("request: %q does not start with a known zone (%s)", args[0], strings.Join(zones, ", "))
	}
	rest := args[2:]
	if len(rest)%2 != 0 {
		return errors.New("request: arguments must be name/value pairs")
	}
	req := transport.Request{Service: svc, Name: args[1], Args: map[string]string{}}
	for i := 0; i < len(rest); i += 2 {
		req.Args[rest[i]] = rest[i+1]
		req.ArgOrder = append(req.ArgOrder, rest[i])
	}
	if err := tr.ServiceRequest(ctx, req); err != nil {
		return err
	}
	fmt.Printf("sent %s to %s\n", req.Name, svc)
	return nil
}

func newLogger(level string) *slog.Logger {
	var l slog.Level
	if err := l.UnmarshalText([]byte(level)); err != nil {
		l = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: l}))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "savant-ha:", err)
	os.Exit(1)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
