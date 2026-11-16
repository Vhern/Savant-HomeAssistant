// Package agent builds entities from the Savant config and keeps their state current.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"savant-ha/internal/config"
	"savant-ha/internal/savantcfg"
	"savant-ha/internal/transport"
)

type Event struct {
	Type      string          `json:"type"`
	EntityID  string          `json:"entity_id,omitempty"`
	State     json.RawMessage `json:"state,omitempty"`
	Available *bool           `json:"available,omitempty"`
}

type Sub struct {
	C    chan Event
	once sync.Once
}

func (s *Sub) close() { s.once.Do(func() { close(s.C) }) }

type Snapshot struct {
	Host      Host                       `json:"host"`
	Available bool                       `json:"available"`
	Entities  []*Meta                    `json:"entities"`
	States    map[string]json.RawMessage `json:"states"`
}

type Engine struct {
	tr      transport.Transport
	cfg     config.Config
	log     *slog.Logger
	version string

	ready     chan struct{}
	readyOnce sync.Once
	refresh   chan struct{}

	mu        sync.Mutex
	inv       *Inventory
	skipped   []Skipped
	entities  []Entity
	byID      map[string]Entity
	values    Values
	states    map[string]json.RawMessage
	subs      map[*Sub]struct{}
	cmdLocks  map[string]*sync.Mutex
	failures  int
	available bool

	sceneStamp string
}

func NewEngine(tr transport.Transport, cfg config.Config, version string, log *slog.Logger) *Engine {
	return &Engine{
		tr: tr, cfg: cfg, log: log, version: version,
		ready:     make(chan struct{}),
		refresh:   make(chan struct{}, 1),
		byID:      map[string]Entity{},
		values:    Values{},
		states:    map[string]json.RawMessage{},
		subs:      map[*Sub]struct{}{},
		cmdLocks:  map[string]*sync.Mutex{},
		available: true,
	}
}

func (e *Engine) Version() string { return e.version }

func (e *Engine) Ready() <-chan struct{} { return e.ready }

func (e *Engine) IsReady() bool {
	select {
	case <-e.ready:
		return true
	default:
		return false
	}
}

func (e *Engine) Refresh() {
	select {
	case e.refresh <- struct{}{}:
	default:
	}
}

func (e *Engine) Run(ctx context.Context) error {
	for {
		if err := e.discover(ctx); err == nil {
			break
		} else if ctx.Err() != nil {
			return nil
		} else {
			e.log.Warn("discovery failed; retrying in 10s", "err", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(10 * time.Second):
		}
	}
	poll := time.NewTicker(e.cfg.PollInterval.D())
	defer poll.Stop()
	var redisc <-chan time.Time
	if d := e.cfg.RediscoverInterval.D(); d > 0 {
		t := time.NewTicker(d)
		defer t.Stop()
		redisc = t.C
	}
	sceneCheck := time.NewTicker(5 * time.Second)
	defer sceneCheck.Stop()
	e.poll(ctx)
	for {
		select {
		case <-ctx.Done():
			e.closeSubs()
			return nil
		case <-sceneCheck.C:
			e.checkScenes(ctx)
		case <-poll.C:
			e.poll(ctx)
		case <-e.refresh:
			e.poll(ctx)
		case <-redisc:
			if err := e.discover(ctx); err != nil {
				e.log.Warn("rediscovery failed", "err", err)
			}
		}
	}
}

func (e *Engine) discover(ctx context.Context) error {
	inv, err := Collect(ctx, e.tr, e.cfg, e.version, false, e.log)
	if err != nil {
		return err
	}
	ents, skipped := Build(inv, e.log)
	e.mu.Lock()
	changed := !sameMetas(e.entities, ents)
	e.inv, e.skipped = inv, skipped
	if changed {
		e.entities = ents
		e.byID = map[string]Entity{}
		for _, en := range ents {
			e.byID[en.Info().ID] = en
		}
		e.states = map[string]json.RawMessage{}
	}
	e.mu.Unlock()
	e.log.Info("discovery", "zones", len(inv.Zones), "entities", len(ents), "skipped", len(skipped), "changed", changed)
	if e.IsReady() && changed {
		e.broadcast(Event{Type: "entities_changed"})
	}
	e.readyOnce.Do(func() { close(e.ready) })
	return nil
}

// checkScenes rescans when the app's scene list changes, so a scene made in the
// Savant app shows up in seconds instead of at the next scheduled rescan.
func (e *Engine) checkScenes(ctx context.Context) {
	e.mu.Lock()
	var db, known string
	if e.inv != nil {
		db, known = e.inv.SceneDB, sceneKey(e.inv.AppScenes)
	}
	e.mu.Unlock()
	if db == "" {
		return
	}
	stamp := fileStamp(db) + fileStamp(db+"-wal")
	if stamp == e.sceneStamp {
		return
	}
	e.sceneStamp = stamp
	app, err := savantcfg.ReadAppScenes(ctx, db)
	if err != nil {
		e.log.Warn("reading scenes failed", "err", err)
		return
	}
	if sceneKey(appScenes(app)) == known {
		return
	}
	e.log.Info("scenes changed in the Savant app; rescanning")
	if err := e.discover(ctx); err != nil {
		e.log.Warn("rescan failed", "err", err)
	}
}

func sceneKey(scenes []transport.Scene) string {
	var parts []string
	for _, sc := range scenes {
		parts = append(parts, sc.ID+"="+sc.Name+"@"+strings.Join(sc.Rooms, ","))
	}
	return strings.Join(parts, ";")
}

func fileStamp(path string) string {
	fi, err := os.Stat(path)
	if err != nil {
		return "-"
	}
	return fi.ModTime().String() + "/" + strconv.FormatInt(fi.Size(), 10)
}

func sameMetas(a, b []Entity) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, _ := json.Marshal(a[i].Info())
		y, _ := json.Marshal(b[i].Info())
		if !bytes.Equal(x, y) {
			return false
		}
	}
	return true
}

func (e *Engine) poll(ctx context.Context) {
	e.mu.Lock()
	var names []string
	for _, en := range e.entities {
		names = append(names, en.States()...)
	}
	e.mu.Unlock()
	if len(names) == 0 {
		return
	}
	vals, err := e.tr.ReadStates(ctx, names)

	e.mu.Lock()
	var events []Event
	if err != nil {
		e.failures++
		if e.failures == 5 && e.available {
			e.available = false
			f := false
			events = append(events, Event{Type: "availability", Available: &f})
			e.log.Error("state reads keep failing; host unavailable", "err", err)
		} else {
			e.log.Warn("poll failed", "err", err)
		}
	} else {
		if !e.available {
			e.available = true
			t := true
			events = append(events, Event{Type: "availability", Available: &t})
			e.log.Info("state reads recovered")
		}
		e.failures = 0
		for k, v := range vals {
			e.values[k] = v
		}
		for _, en := range e.entities {
			b, _ := json.Marshal(en.State(e.values))
			id := en.Info().ID
			if old, ok := e.states[id]; ok && bytes.Equal(old, b) {
				continue
			}
			e.states[id] = b
			events = append(events, Event{Type: "state", EntityID: id, State: b})
		}
	}
	e.mu.Unlock()
	for _, ev := range events {
		e.broadcast(ev)
	}
}

func (e *Engine) Subscribe() (*Sub, Snapshot) {
	s := &Sub{C: make(chan Event, 512)}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.subs[s] = struct{}{}
	return s, e.snapshotLocked()
}

func (e *Engine) Unsubscribe(s *Sub) {
	e.mu.Lock()
	delete(e.subs, s)
	e.mu.Unlock()
	s.close()
}

func (e *Engine) snapshotLocked() Snapshot {
	snap := Snapshot{Available: e.available, States: map[string]json.RawMessage{}}
	if e.inv != nil {
		snap.Host = e.inv.Host
	}
	for _, en := range e.entities {
		snap.Entities = append(snap.Entities, en.Info())
		if st, ok := e.states[en.Info().ID]; ok {
			snap.States[en.Info().ID] = st
		}
	}
	return snap
}

func (e *Engine) broadcast(ev Event) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for s := range e.subs {
		select {
		case s.C <- ev:
		default:
			e.log.Warn("subscriber too slow; dropping it")
			delete(e.subs, s)
			s.close()
		}
	}
}

func (e *Engine) closeSubs() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for s := range e.subs {
		delete(e.subs, s)
		s.close()
	}
}

var ErrUnknownEntity = errors.New("unknown entity")

var (
	commandAck     = 300 * time.Millisecond
	commandTimeout = 30 * time.Second
)

func (e *Engine) Command(ctx context.Context, id, action string, p Params) error {
	e.mu.Lock()
	en, ok := e.byID[id]
	e.mu.Unlock()
	if !ok {
		return fmt.Errorf("%w %q", ErrUnknownEntity, id)
	}
	e.log.Info("command", "entity", id, "action", action, "params", p)
	key := id
	if k, ok := en.(interface{ LockKey() string }); ok {
		key = k.LockKey()
	}
	lock := e.commandLock(key)
	done := make(chan error, 1)
	go func() {
		lock.Lock()
		defer lock.Unlock()
		cctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		start := time.Now()
		err := en.Do(cctx, e.tr, action, p)
		e.Refresh()
		if err != nil {
			e.log.Warn("command failed", "entity", id, "action", action, "err", err)
		} else {
			e.log.Debug("command done", "entity", id, "action", action, "took", time.Since(start))
		}
		done <- err
	}()
	// sclibridge takes a couple of seconds to return from a service request even
	// though Savant acts on it right away, so don't make Home Assistant wait for it.
	select {
	case err := <-done:
		return err
	case <-time.After(commandAck):
		return nil
	case <-ctx.Done():
		return nil
	}
}

func (e *Engine) commandLock(id string) *sync.Mutex {
	e.mu.Lock()
	defer e.mu.Unlock()
	l, ok := e.cmdLocks[id]
	if !ok {
		l = &sync.Mutex{}
		e.cmdLocks[id] = l
	}
	return l
}

func (e *Engine) ServiceRequest(ctx context.Context, req transport.Request) error {
	e.log.Info("service request", "service", req.Service.String(), "request", req.Name, "args", req.Args)
	err := e.tr.ServiceRequest(ctx, req)
	e.Refresh()
	return err
}

func (e *Engine) Rediscover(ctx context.Context) error { return e.discover(ctx) }

func (e *Engine) Diagnostics() map[string]any {
	e.mu.Lock()
	defer e.mu.Unlock()
	var sources []string
	for _, en := range e.entities {
		sources = append(sources, en.Info().ID+": "+en.Info().Source)
	}
	return map[string]any{
		"inventory": e.inv,
		"entities":  sources,
		"skipped":   e.skipped,
		"available": e.available,
		"failures":  e.failures,
	}
}
