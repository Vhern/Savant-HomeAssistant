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
	Device     *Device        `json:"device,omitempty"`
	Source     string         `json:"source,omitempty"`
	// Secret holds things like camera logins; diagnostics and discover hide it.
	Secret map[string]string `json:"secret,omitempty"`
}

// Device is how entities are grouped in Home Assistant: one device per room
// and kind (Lighting, Shades, Keypads, Media) and one per A/V source.
type Device struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Model        string `json:"model"`
	Manufacturer string `json:"manufacturer,omitempty"`
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
	// DMX loads report their color as "R,G,B,W,..|kelvin,..|mode" in ColorState.
	ColorState           string
	RGB                  bool
	MinKelvin, MaxKelvin int

	mu        sync.Mutex
	lastLevel float64
}

func (l *Light) States() []string { return nonEmpty(l.LevelState, l.ColorState) }

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
	if c, ok := parseColor(v[l.ColorState]); ok && l.ColorState != "" {
		tunable := l.MaxKelvin > 0 && c.Kelvin > 0
		if l.RGB {
			st["rgbw"] = c.RGBW[:]
			st["color_mode"] = "rgbw"
		}
		if tunable {
			st["kelvin"] = c.Kelvin
			if !l.RGB || strings.EqualFold(c.Mode, "Tunable White") {
				st["color_mode"] = "color_temp"
			}
		}
	}
	return st
}

type lightColor struct {
	RGBW   [4]int
	Kelvin int
	Mode   string
}

// parseColor reads CurrentColor_*, like "255,000,000,000,100,100|3000,000,100|Infinite Color".
func parseColor(raw string) (lightColor, bool) {
	var c lightColor
	parts := strings.Split(strings.TrimSpace(raw), "|")
	rgb := strings.Split(parts[0], ",")
	if len(rgb) < 4 {
		return c, false
	}
	for i := range 4 {
		n, err := strconv.Atoi(strings.TrimSpace(rgb[i]))
		if err != nil {
			return c, false
		}
		c.RGBW[i] = max(0, min(255, n))
	}
	if len(parts) > 1 {
		k, _ := strconv.Atoi(strings.TrimSpace(strings.Split(parts[1], ",")[0]))
		c.Kelvin = k
	}
	if len(parts) > 2 {
		c.Mode = strings.TrimSpace(parts[2])
	}
	return c, true
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
	if rgbw, ok := p["rgbw"].([]any); ok && len(rgbw) == 4 && l.RGB {
		for i, k := range []string{"bleColorRed", "bleColorGreen", "bleColorBlue", "bleColorWhite"} {
			f, _ := Params{"v": rgbw[i]}.Float("v")
			extra[k] = strconv.Itoa(int(math.Round(math.Max(0, math.Min(255, f)))))
		}
	}
	if k, ok := p.Float("kelvin"); ok && l.MaxKelvin > 0 {
		extra["kelvin"] = strconv.Itoa(int(math.Round(math.Max(float64(l.MinKelvin), math.Min(float64(l.MaxKelvin), k)))))
	}
	return l.Request("DimmerSet", extra)
}

// LightScene is a Blueprint lighting scene, which can be turned on and off.
type LightScene struct {
	Meta
	Target
	StateName string
	On, Off   string
}

func (s *LightScene) States() []string { return nonEmpty(s.StateName) }

func (s *LightScene) State(v Values) map[string]any {
	on, ok := transport.ParseBool(v[s.StateName])
	if !ok {
		return nil
	}
	return map[string]any{"on": on}
}

func (s *LightScene) Do(ctx context.Context, x Executor, action string, _ Params) error {
	switch {
	case action == "turn_on":
		return x.ServiceRequest(ctx, s.Request(s.On, nil))
	case action == "turn_off" && s.Off != "":
		return x.ServiceRequest(ctx, s.Request(s.Off, nil))
	}
	return errAction(&s.Meta, action)
}

type KeypadButton struct {
	Meta
	Target
	LEDState       string
	Press, Release string
}

func (b *KeypadButton) States() []string { return nonEmpty(b.LEDState) }

func (b *KeypadButton) State(v Values) map[string]any {
	if led, ok := transport.ParseBool(v[b.LEDState]); ok {
		return map[string]any{"led": led}
	}
	return map[string]any{}
}

func (b *KeypadButton) Do(ctx context.Context, x Executor, action string, _ Params) error {
	if action != "press" {
		return errAction(&b.Meta, action)
	}
	if err := x.ServiceRequest(ctx, b.Request(b.Press, nil)); err != nil {
		return err
	}
	if b.Release != "" && b.Release != b.Press {
		return x.ServiceRequest(ctx, b.Request(b.Release, nil))
	}
	return nil
}

// Shade requests come from Blueprint: a shade group uses GroupShadeUp,
// GroupShadeStop and GroupShadeSet instead of ShadeUp, ShadeStop and ShadeSet.
type Shade struct {
	Meta
	Target
	LevelState             string
	Up, Down, Stop, Setter string
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
		if s.Up != "" {
			return x.ServiceRequest(ctx, s.Request(s.Up, nil))
		}
		return x.ServiceRequest(ctx, s.set(100))
	case "close":
		if s.Down != "" {
			return x.ServiceRequest(ctx, s.Request(s.Down, nil))
		}
		return x.ServiceRequest(ctx, s.set(0))
	case "stop":
		if s.Stop == "" {
			return errAction(&s.Meta, action)
		}
		return x.ServiceRequest(ctx, s.Request(s.Stop, nil))
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
	return s.Request(s.Setter, map[string]string{"ShadeLevel": strconv.Itoa(pct(pos))})
}

type Lock struct {
	Meta
	Target
	StatusState string
	LockedState string
	LockReq     string
	UnlockReq   string
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
		return x.ServiceRequest(ctx, l.Request(or(l.LockReq, "LockDoorLock"), nil))
	case "unlock":
		return x.ServiceRequest(ctx, l.Request(or(l.UnlockReq, "UnlockDoorLock"), nil))
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
// several services. Primary is the one that gets powered on; Apps lists the
// services that open an app on the device (Netflix, Hulu...), when it has them.
type Source struct {
	Name     string
	Primary  *savantcfg.ServiceDef
	Service  transport.Service
	Services map[string]bool
	Apps     []App
	// Shortcuts are requests a dealer added to the source, like the Netflix
	// and YouTube buttons on an Apple TV in the Savant app.
	Shortcuts []string
	// NowPlaying maps title, artist... to the source's own states, for music
	// servers that report them (Savant Music, Sonos).
	NowPlaying map[string]string
}

var nowPlayingStates = map[string]string{
	"title": "CurrentSongName", "artist": "CurrentArtistName", "album": "CurrentAlbumName",
	"artwork": "CurrentArtworkURL", "paused": "CurrentPauseStatus", "shuffle": "CurrentShuffleStatus",
	"repeat": "CurrentRepeatStatus",
}

func (s *Source) nowPlayingStates() []string {
	var out []string
	for _, k := range sortedKeys(s.NowPlaying) {
		out = append(out, s.NowPlaying[k])
	}
	return out
}

func (s *Source) addNowPlaying(st map[string]any, v Values) {
	if len(s.NowPlaying) == 0 {
		return
	}
	for _, k := range []string{"title", "artist", "album"} {
		if t := strings.TrimSpace(v[s.NowPlaying[k]]); t != "" {
			st[k] = t
		}
	}
	if st["title"] != nil || st["artist"] != nil {
		if art := strings.TrimSpace(v[s.NowPlaying["artwork"]]); art != "" {
			st["artwork"] = art
		}
	}
	for _, k := range []string{"paused", "shuffle"} {
		if b, ok := transport.ParseBool(v[s.NowPlaying[k]]); ok {
			st[k] = b
		}
	}
	if r := strings.TrimSpace(v[s.NowPlaying["repeat"]]); r != "" {
		st["repeat"] = map[string]string{"0": "off", "1": "all", "2": "one"}[r]
	}
}

func (s *Source) shortcut(name string) string {
	for _, sc := range s.Shortcuts {
		if strings.EqualFold(sc, name) {
			return sc
		}
	}
	return ""
}

type App struct {
	Name    string
	Service transport.Service
}

func (s *Source) app(name string) *App {
	for i := range s.Apps {
		if strings.EqualFold(s.Apps[i].Name, name) {
			return &s.Apps[i]
		}
	}
	return nil
}

func (s *Source) appFor(dash string) string {
	for _, a := range s.Apps {
		if a.Service.Dash() == dash {
			return a.Name
		}
	}
	return ""
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

func (m *MediaRoom) States() []string {
	out := nonEmpty(m.ActiveState, m.VolumeState, m.MuteState)
	for _, s := range m.Sources {
		out = append(out, s.nowPlayingStates()...)
	}
	return out
}

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
		if app := src.appFor(m.activeDash()); app != "" {
			st["app"] = app
		}
		src.addNowPlaying(st, v)
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
		return m.powerOn(ctx, x, src.Service)
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
		if err := req("PowerOff", nil); err != nil {
			return err
		}
		m.setActive("")
		return nil
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
	case "play", "pause", "stop", "next", "previous":
		return req(transportRequests[action], nil)
	case "shuffle":
		if on, _ := p.Bool("shuffle"); on {
			return req("ShuffleOn", nil)
		}
		return req("ShuffleOff", nil)
	case "repeat":
		switch p.String("repeat") {
		case "all":
			return req("RepeatOn", nil)
		case "one":
			if src != nil && src.Primary != nil && src.Primary.HasRequest("RepeatOne") {
				return req("RepeatOne", nil)
			}
			return req("RepeatOn", nil)
		}
		return req("RepeatOff", nil)
	}
	return errAction(&m.Meta, action)
}

var transportRequests = map[string]string{"play": "Play", "pause": "Pause", "stop": "Stop", "next": "SkipUp", "previous": "SkipDown"}

// SourcePlayer is one source in a room (a PS5, a TV), shown on its own like
// the tiles in the Savant app. It is on while the room is playing it.
type SourcePlayer struct {
	Meta
	Room   *MediaRoom
	Source *Source
}

func (s *SourcePlayer) LockKey() string { return s.Room.ID }

func (s *SourcePlayer) States() []string {
	return append(nonEmpty(s.Room.ActiveState), s.Source.nowPlayingStates()...)
}

func (s *SourcePlayer) State(v Values) map[string]any {
	raw, ok := v[s.Room.ActiveState]
	if !ok {
		return nil
	}
	active := strings.TrimSpace(raw)
	st := map[string]any{"on": s.Source.Services[active]}
	if len(s.Source.Apps) > 0 {
		var app any
		if name := s.Source.appFor(active); name != "" {
			app = name
		}
		st["source"] = app
	}
	if st["on"] == true {
		s.Source.addNowPlaying(st, v)
	}
	return st
}

func (s *SourcePlayer) Do(ctx context.Context, x Executor, action string, p Params) error {
	switch action {
	case "turn_on":
		return s.Room.powerOn(ctx, x, s.Source.Service)
	case "select_source":
		name := p.String("source")
		if app := s.Source.app(name); app != nil {
			return s.Room.powerOn(ctx, x, app.Service)
		}
		if sc := s.Source.shortcut(name); sc != "" {
			if !s.playing() {
				if err := s.Room.powerOn(ctx, x, s.Source.Service); err != nil {
					return err
				}
			}
			return x.ServiceRequest(ctx, transport.Request{Service: s.Source.Service, Name: sc})
		}
		return fmt.Errorf("%s has no app %q", s.Source.Name, name)
	}
	if src, _ := s.Room.current(); src != s.Source {
		return fmt.Errorf("%s isn't playing in %s", s.Source.Name, s.Room.Zone)
	}
	return s.Room.Do(ctx, x, action, p)
}

func (s *SourcePlayer) playing() bool {
	src, _ := s.Room.current()
	return src == s.Source
}

// Remote sends a source's own requests (OSDCursorUp, Menu...) or aliases like "up".
type Remote struct {
	Meta
	Player *SourcePlayer
}

var remoteAliases = map[string]string{
	"up": "OSDCursorUp", "down": "OSDCursorDown", "left": "OSDCursorLeft", "right": "OSDCursorRight",
	"select": "OSDSelect", "ok": "OSDSelect", "enter": "OSDSelect", "menu": "Menu", "home": "Home",
	"back": "Back", "exit": "Exit", "play": "Play", "pause": "Pause", "stop": "Stop",
	"next": "SkipUp", "previous": "SkipDown", "volume_up": "VolumeUp", "volume_down": "VolumeDown",
	"mute": "MuteOn", "unmute": "MuteOff", "info": "Info", "guide": "Guide",
	"channel_up": "ChannelUp", "channel_down": "ChannelDown",
}

func (r *Remote) LockKey() string { return r.Player.LockKey() }

func (r *Remote) States() []string { return r.Player.States() }

func (r *Remote) State(v Values) map[string]any {
	st := r.Player.State(v)
	if st == nil {
		return nil
	}
	return map[string]any{"on": st["on"]}
}

func (r *Remote) Do(ctx context.Context, x Executor, action string, p Params) error {
	if action != "send_command" {
		return r.Player.Do(ctx, x, action, p)
	}
	var cmds []string
	switch c := p["command"].(type) {
	case string:
		cmds = []string{c}
	case []any:
		for _, v := range c {
			if s, ok := v.(string); ok {
				cmds = append(cmds, s)
			}
		}
	}
	if len(cmds) == 0 {
		return fmt.Errorf("%s: send_command needs a command", r.ID)
	}
	src := r.Player.Source
	reqs := make([]string, 0, len(cmds))
	for _, c := range cmds {
		name := r.resolve(c)
		if name == "" {
			return fmt.Errorf("%s has no command %q", src.Name, c)
		}
		reqs = append(reqs, name)
	}
	for _, name := range reqs {
		if err := x.ServiceRequest(ctx, transport.Request{Service: src.Service, Name: name}); err != nil {
			return err
		}
	}
	return nil
}

func (r *Remote) resolve(cmd string) string {
	d := r.Player.Source.Primary
	if d == nil {
		if a := remoteAliases[strings.ToLower(cmd)]; a != "" {
			return a
		}
		return cmd
	}
	for name := range d.Requests {
		if strings.EqualFold(name, cmd) {
			return name
		}
	}
	if a := remoteAliases[strings.ToLower(cmd)]; a != "" && d.HasRequest(a) {
		return a
	}
	return ""
}

func (m *MediaRoom) powerOn(ctx context.Context, x Executor, svc transport.Service) error {
	if err := x.ServiceRequest(ctx, transport.Request{Service: svc, Name: "PowerOn"}); err != nil {
		return err
	}
	m.setActive(svc.Dash())
	return nil
}

func (m *MediaRoom) activeDash() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active
}

// setActive records what a command just did, so the next command goes to the
// right service before the poll catches up.
func (m *MediaRoom) setActive(dash string) {
	m.mu.Lock()
	m.active = dash
	m.mu.Unlock()
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

// Camera has nothing to poll; Home Assistant reads the camera itself.
type Camera struct{ Meta }

func (c *Camera) States() []string            { return nil }
func (c *Camera) State(Values) map[string]any { return map[string]any{} }
func (c *Camera) Do(_ context.Context, _ Executor, action string, _ Params) error {
	return errAction(&c.Meta, action)
}

// BoolSensor is an on/off state like a device's power or connection.
type BoolSensor struct {
	Meta
	StateName string
}

func (s *BoolSensor) States() []string { return []string{s.StateName} }

func (s *BoolSensor) State(v Values) map[string]any {
	if b, ok := transport.ParseBool(v[s.StateName]); ok {
		return map[string]any{"on": b}
	}
	return nil
}

func (s *BoolSensor) Do(_ context.Context, _ Executor, action string, _ Params) error {
	return errAction(&s.Meta, action)
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

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
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
