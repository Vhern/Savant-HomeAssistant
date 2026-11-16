package agent

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"savant-ha/internal/savantcfg"
	"savant-ha/internal/transport"
)

type Entity interface {
	Info() *Meta
	States() []string
	State(v Values) map[string]any
	Do(ctx context.Context, x Executor, action string, p Params) error
}

type Meta struct {
	ID         string         `json:"id"`
	Platform   string         `json:"platform"`
	Name       string         `json:"name"`
	Zone       string         `json:"zone,omitempty"`
	Attributes map[string]any `json:"attributes,omitempty"`
	Source     string         `json:"source,omitempty"`
}

func (m *Meta) Info() *Meta { return m }

type Values map[string]string

type Executor interface {
	ServiceRequest(ctx context.Context, req transport.Request) error
	ActivateScene(ctx context.Context, s transport.Scene) error
}

type Params map[string]any

func (p Params) Float(k string) (float64, bool) {
	switch v := p[k].(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case string:
		f, err := strconv.ParseFloat(v, 64)
		return f, err == nil
	}
	return 0, false
}

func (p Params) String(k string) string {
	s, _ := p[k].(string)
	return s
}

func (p Params) Bool(k string) (bool, bool) {
	b, ok := p[k].(bool)
	return b, ok
}

func errAction(m *Meta, action string) error {
	return fmt.Errorf("%s: unsupported action %q", m.ID, action)
}

// Request args come from the data table row, the same way Blueprint fills them
// (a light row's Address1 becomes DimmerSet's Address1).
type Target struct {
	Service transport.Service
	Def     *savantcfg.ServiceDef
	Fields  map[string]string
}

var addrKey = regexp.MustCompile(`(?i)address\d*$`)

func (t Target) Request(name string, extra map[string]string) transport.Request {
	args := map[string]string{}
	var order []string
	if t.Def != nil {
		if declared, ok := t.Def.Requests[name]; ok {
			for _, a := range declared {
				if v := t.Fields[a]; v != "" {
					args[a] = v
				}
			}
			order = declared
		}
	}
	if len(args) == 0 {
		for k, v := range t.Fields {
			if v != "" && addrKey.MatchString(k) {
				args[k] = v
			}
		}
	}
	for k, v := range extra {
		args[k] = v
	}
	return transport.Request{Service: t.Service, Name: name, Args: args, ArgOrder: order}
}

func (t Target) Has(name string, unknown bool) bool {
	if t.Def == nil {
		return unknown
	}
	return t.Def.HasRequest(name)
}

func pct(f float64) int { return int(math.Round(math.Max(0, math.Min(100, f)))) }

type Light struct {
	Meta
	Target
	LevelState string
	Dimmable   bool

	mu        sync.Mutex
	lastLevel float64
}

func (l *Light) States() []string { return nonEmpty(l.LevelState) }

func (l *Light) State(v Values) map[string]any {
	raw, ok := v[l.LevelState]
	if !ok || l.LevelState == "" {
		return nil
	}
	level, ok := transport.ParseLevel(raw)
	if !ok {
		on, okb := transport.ParseBool(raw)
		if !okb {
			return nil
		}
		if level = 0; on {
			level = 100
		}
	}
	if level > 0 {
		l.mu.Lock()
		l.lastLevel = level
		l.mu.Unlock()
	}
	st := map[string]any{"on": level > 0}
	if l.Dimmable {
		st["brightness"] = pct(level)
	}
	return st
}

func (l *Light) Do(ctx context.Context, x Executor, action string, p Params) error {
	switch action {
	case "turn_off":
		if !l.Dimmable && l.Has("SwitchOff", true) {
			return x.ServiceRequest(ctx, l.Request("SwitchOff", nil))
		}
		return x.ServiceRequest(ctx, l.dimmerSet(0, p))
	case "turn_on":
		if !l.Dimmable && l.Has("SwitchOn", true) {
			return x.ServiceRequest(ctx, l.Request("SwitchOn", nil))
		}
		level, ok := p.Float("brightness")
		if !ok {
			l.mu.Lock()
			level = l.lastLevel
			l.mu.Unlock()
			if level <= 0 {
				level = 100
			}
		}
		return x.ServiceRequest(ctx, l.dimmerSet(level, p))
	}
	return errAction(&l.Meta, action)
}

func (l *Light) dimmerSet(level float64, p Params) transport.Request {
	extra := map[string]string{"DimmerLevel": strconv.Itoa(pct(level))}
	if t, ok := p.Float("transition"); ok && t > 0 {
		extra["FadeTime"] = strconv.FormatFloat(t, 'f', -1, 64)
	}
	return l.Request("DimmerSet", extra)
}

type Shade struct {
	Meta
	Target
	LevelState string
}

func (s *Shade) States() []string { return nonEmpty(s.LevelState) }

func (s *Shade) State(v Values) map[string]any {
	if s.LevelState == "" {
		return map[string]any{}
	}
	lvl, ok := transport.ParseLevel(v[s.LevelState])
	if !ok {
		return nil
	}
	return map[string]any{"position": pct(lvl)}
}

func (s *Shade) Do(ctx context.Context, x Executor, action string, p Params) error {
	switch action {
	case "open":
		if s.Has("ShadeUp", false) {
			return x.ServiceRequest(ctx, s.Request("ShadeUp", nil))
		}
		return x.ServiceRequest(ctx, s.set(100))
	case "close":
		if s.Has("ShadeDown", false) {
			return x.ServiceRequest(ctx, s.Request("ShadeDown", nil))
		}
		return x.ServiceRequest(ctx, s.set(0))
	case "stop":
		return x.ServiceRequest(ctx, s.Request("ShadeStop", nil))
	case "set_position":
		pos, ok := p.Float("position")
		if !ok {
			return fmt.Errorf("%s: set_position needs position", s.ID)
		}
		return x.ServiceRequest(ctx, s.set(pos))
	}
	return errAction(&s.Meta, action)
}

func (s *Shade) set(pos float64) transport.Request {
	return s.Request("ShadeSet", map[string]string{"ShadeLevel": strconv.Itoa(pct(pos))})
}

type Lock struct {
	Meta
	Target
	StatusState string
	LockedState string
}

func (l *Lock) States() []string { return nonEmpty(l.StatusState, l.LockedState) }

func (l *Lock) State(v Values) map[string]any {
	st := strings.ToLower(strings.TrimSpace(v[l.StatusState]))
	switch {
	case strings.Contains(st, "unlock"):
		return map[string]any{"locked": false, "jammed": false}
	case strings.Contains(st, "jam"):
		return map[string]any{"locked": false, "jammed": true}
	case strings.Contains(st, "lock"):
		return map[string]any{"locked": true, "jammed": false}
	}
	if b, ok := transport.ParseBool(v[l.LockedState]); ok {
		return map[string]any{"locked": b, "jammed": false}
	}
	return nil
}

func (l *Lock) Do(ctx context.Context, x Executor, action string, _ Params) error {
	switch action {
	case "lock":
		return x.ServiceRequest(ctx, l.Request("LockDoorLock", nil))
	case "unlock":
		return x.ServiceRequest(ctx, l.Request("UnlockDoorLock", nil))
	}
	return errAction(&l.Meta, action)
}

type Scene struct {
	Meta
	Scene transport.Scene
}

func (s *Scene) States() []string            { return nil }
func (s *Scene) State(Values) map[string]any { return map[string]any{} }

func (s *Scene) Do(ctx context.Context, x Executor, action string, _ Params) error {
	if action != "activate" {
		return errAction(&s.Meta, action)
	}
	return x.ActivateScene(ctx, s.Scene)
}

// A TV's built-in app inputs all share its name, so one source can map to
// several services. Primary is the one that gets powered on.
type Source struct {
	Name     string
	Primary  *savantcfg.ServiceDef
	Service  transport.Service
	Services map[string]bool
}

type MediaRoom struct {
	Meta
	Zone        string
	Sources     []*Source
	ActiveState string
	VolumeState string
	MuteState   string

	mu         sync.Mutex
	lastSource string
	active     string
}

func (m *MediaRoom) States() []string { return nonEmpty(m.ActiveState, m.VolumeState, m.MuteState) }

func (m *MediaRoom) activeSource(v Values) (*Source, bool) {
	raw, ok := v[m.ActiveState]
	if !ok {
		return nil, false
	}
	active := strings.TrimSpace(raw)
	m.mu.Lock()
	m.active = active
	m.mu.Unlock()
	if active == "" {
		return nil, true
	}
	for _, s := range m.Sources {
		if s.Services[active] {
			return s, true
		}
	}
	return &Source{Name: active}, true
}

func (m *MediaRoom) State(v Values) map[string]any {
	src, ok := m.activeSource(v)
	if !ok {
		return nil
	}
	st := map[string]any{"on": src != nil, "source": nil}
	if src != nil {
		st["source"] = src.Name
		m.mu.Lock()
		m.lastSource = src.Name
		m.mu.Unlock()
	}
	if vol, ok := transport.ParseLevel(v[m.VolumeState]); ok {
		st["volume"] = vol
	}
	if mute, ok := transport.ParseBool(v[m.MuteState]); ok {
		st["muted"] = mute
	}
	return st
}

func (m *MediaRoom) source(name string) *Source {
	for _, s := range m.Sources {
		if strings.EqualFold(s.Name, name) {
			return s
		}
	}
	return nil
}

func (m *MediaRoom) Do(ctx context.Context, x Executor, action string, p Params) error {
	switch action {
	case "turn_on", "select_source":
		name := p.String("source")
		if name == "" {
			m.mu.Lock()
			name = m.lastSource
			m.mu.Unlock()
		}
		src := m.source(name)
		if src == nil && action == "turn_on" && len(m.Sources) > 0 {
			src = m.Sources[0]
		}
		if src == nil {
			return fmt.Errorf("%s: unknown source %q", m.ID, name)
		}
		return x.ServiceRequest(ctx, transport.Request{Service: src.Service, Name: "PowerOn"})
	}

	src, svc := m.current()
	if svc == nil {
		return fmt.Errorf("%s: room is off", m.ID)
	}
	req := func(name string, args map[string]string) error {
		return x.ServiceRequest(ctx, transport.Request{Service: *svc, Name: name, Args: args})
	}
	switch action {
	case "turn_off":
		return req("PowerOff", nil)
	case "volume_up":
		return req("VolumeUp", nil)
	case "volume_down":
		return req("VolumeDown", nil)
	case "mute":
		if muted, _ := p.Bool("muted"); muted {
			return req("MuteOn", nil)
		}
		return req("MuteOff", nil)
	case "set_volume":
		vol, ok := p.Float("volume")
		if !ok {
			return fmt.Errorf("%s: set_volume needs volume", m.ID)
		}
		if src != nil && src.Primary != nil && !src.Primary.HasRequest("SetVolume") {
			return fmt.Errorf("%s: %s has no SetVolume request", m.ID, src.Name)
		}
		return req("SetVolume", map[string]string{"VolumeValue": strconv.Itoa(int(math.Round(vol)))})
	}
	return errAction(&m.Meta, action)
}

func (m *MediaRoom) current() (*Source, *transport.Service) {
	m.mu.Lock()
	active := m.active
	m.mu.Unlock()
	if active == "" {
		return nil, nil
	}
	for _, s := range m.Sources {
		if s.Services[active] {
			if svc, err := transport.ParseServiceLine(m.Zone, active, nil); err == nil {
				return s, &svc
			}
			return s, &s.Service
		}
	}
	if svc, err := transport.ParseServiceLine(m.Zone, active, nil); err == nil {
		return nil, &svc
	}
	return nil, nil
}

type Sensor struct {
	Meta
	StateName string
	Numeric   bool
}

func (s *Sensor) States() []string { return []string{s.StateName} }

func (s *Sensor) State(v Values) map[string]any {
	raw := strings.TrimSpace(v[s.StateName])
	if raw == "" {
		return nil
	}
	if s.Numeric {
		f, ok := transport.ParseLevel(raw)
		if !ok {
			return nil
		}
		return map[string]any{"value": f}
	}
	return map[string]any{"value": raw}
}

func (s *Sensor) Do(_ context.Context, _ Executor, action string, _ Params) error {
	return errAction(&s.Meta, action)
}

func nonEmpty(s ...string) []string {
	var out []string
	for _, v := range s {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
