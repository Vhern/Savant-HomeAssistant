package agent

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"savant-ha/internal/config"
	"savant-ha/internal/savantcfg"
	"savant-ha/internal/transport"
)

type Host struct {
	UID              string `json:"uid"`
	Name             string `json:"name"`
	Component        string `json:"component"`
	Model            string `json:"model"`
	Software         string `json:"software"`
	TemperatureUnits string `json:"temperature_units,omitempty"`
	OS               string `json:"os"`
	Arch             string `json:"arch"`
	AgentVersion     string `json:"agent_version"`
}

type Inventory struct {
	Host       Host                           `json:"host"`
	Sclibridge string                         `json:"sclibridge"`
	ConfigDir  string                         `json:"config_dir"`
	SceneDB    string                         `json:"scene_db,omitempty"`
	AppDB      string                         `json:"app_db,omitempty"`
	Zones      []string                       `json:"zones"`
	Services   map[string][]transport.Service `json:"services"`
	Scenes     []transport.Scene              `json:"scenes"`
	AppScenes  []transport.Scene              `json:"-"`
	Rows       []savantcfg.Row                `json:"-"`
	App        *savantcfg.AppDB               `json:"-"`
	Circuits   []savantcfg.Circuit            `json:"circuits,omitempty"`
	Known      map[string]bool                `json:"-"`
	Remotes    []ProRemote                    `json:"remotes,omitempty"`
	Defs       *savantcfg.Services            `json:"-"`
	Manifest   savantcfg.Manifest             `json:"-"`
	StateNames []string                       `json:"state_names,omitempty"`
	Warnings   []string                       `json:"warnings,omitempty"`
}

func (inv *Inventory) warn(log *slog.Logger, format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	inv.Warnings = append(inv.Warnings, msg)
	log.Warn(msg)
}

func Collect(ctx context.Context, tr transport.Transport, cfg config.Config, version string, withStateNames bool, log *slog.Logger) (*Inventory, error) {
	inv := &Inventory{Services: map[string][]transport.Service{}, Defs: &savantcfg.Services{}}
	if p, ok := tr.(interface{ Path() string }); ok {
		inv.Sclibridge = p.Path()
	}
	zones, err := tr.ListZones(ctx)
	if err != nil {
		return nil, fmt.Errorf("list zones: %w", err)
	}
	if len(zones) == 0 {
		return nil, fmt.Errorf("host reports no zones (still starting?)")
	}
	inv.Zones = zones
	for _, z := range zones {
		svcs, err := tr.ListServices(ctx, z)
		if err != nil {
			inv.warn(log, "services for zone %q: %v", z, err)
			continue
		}
		inv.Services[z] = svcs
	}

	dir := cfg.ConfigDir
	if dir == "" {
		dir, err = savantcfg.LocateConfigDir()
	}
	if err != nil {
		inv.warn(log, "Blueprint config not found (%v); lights, shades and locks need it", err)
	} else {
		inv.ConfigDir = dir
		app, appErr := savantcfg.ReadAppDB(ctx, savantcfg.AppDBPath(dir))
		if appErr != nil && !errors.Is(appErr, fs.ErrNotExist) {
			inv.warn(log, "serviceImplementation.sqlite: %v", appErr)
		}
		if app != nil {
			inv.App, inv.AppDB = app, app.Path
		}
		defs, err := savantcfg.ReadServices(filepath.Join(dir, "serviceImplementation.xml"))
		switch {
		case err == nil:
			inv.Defs = defs
			if app != nil {
				mergeAppDefs(defs, app.Services)
			}
		case app != nil && app.Services != nil && len(app.Services.List) > 0:
			// Savant 12 Mac hosts keep the XML readable by _savant only.
			inv.Defs = app.Services
		default:
			inv.warn(log, "serviceImplementation.xml: %v", err)
		}
		if rows, err := savantcfg.ReadDataTables(filepath.Join(dir, "dataTableInfo.plist")); err != nil {
			inv.warn(log, "dataTableInfo.plist: %v", err)
		} else {
			inv.Rows = rows
		}
		if m, err := savantcfg.ReadManifest(filepath.Join(dir, "uimanifest.json")); err != nil {
			inv.warn(log, "uimanifest.json: %v", err)
		} else {
			inv.Manifest = m
		}
		if c, err := savantcfg.ReadEnergy(savantcfg.EnergyConfigPath(dir)); err == nil {
			inv.Circuits = c
		} else if !errors.Is(err, fs.ErrNotExist) {
			inv.warn(log, "energyConfig.json: %v", err)
		}
	}
	// State names tell which sources report what's playing.
	if names, err := tr.StateNames(ctx, ""); err != nil {
		inv.warn(log, "statenames: %v", err)
	} else {
		inv.Known = make(map[string]bool, len(names))
		for _, n := range names {
			inv.Known[n] = true
		}
		if withStateNames {
			inv.StateNames = names
		}
	}
	for _, id := range findRemotes(inv) {
		r := ProRemote{ID: id}
		keys := []string{id + ".Name", id + ".DeviceModel", id + ".DeviceMake"}
		if v, err := tr.ReadStates(ctx, keys); err == nil {
			r.Name, r.Model, r.Make = strings.TrimSpace(v[keys[0]]), strings.TrimSpace(v[keys[1]]), strings.TrimSpace(v[keys[2]])
		}
		inv.Remotes = append(inv.Remotes, r)
	}

	inv.Scenes = collectScenes(ctx, tr, inv, log)
	inv.Host = collectHost(ctx, tr, inv, version)
	return inv, nil
}

// mergeAppDefs copies what only the app database knows onto the XML's
// services, and adds the services the XML doesn't have.
func mergeAppDefs(defs, app *savantcfg.Services) {
	if app == nil {
		return
	}
	for _, a := range app.List {
		d := defs.Find(a.Zone, a.Component, a.LogicalComponent, a.Type, a.Variant)
		if d == nil {
			defs.List = append(defs.List, a)
			continue
		}
		d.Custom = a.Custom
		if d.AppName == "" {
			d.AppName = a.AppName
		}
	}
}

func collectScenes(ctx context.Context, tr transport.Transport, inv *Inventory, log *slog.Logger) []transport.Scene {
	var scenes []transport.Scene
	seen := map[string]bool{}
	if inv.ConfigDir != "" {
		inv.SceneDB = savantcfg.SceneDB(inv.ConfigDir)
		app, err := savantcfg.ReadAppScenes(ctx, inv.SceneDB)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			inv.warn(log, "app scenes: %v", err)
		}
		inv.AppScenes = appScenes(app)
		for _, sc := range inv.AppScenes {
			scenes = append(scenes, sc)
			seen[sc.ID] = true
		}
	}
	legacy, err := tr.Scenes(ctx)
	if err != nil {
		inv.warn(log, "scenes: %v", err)
	}
	for _, sc := range legacy {
		if !seen[sc.ID] {
			scenes = append(scenes, sc)
		}
	}
	return scenes
}

// The host itself shows up as the generic service with no logical component.
func hostCandidates(inv *Inventory) []string {
	seen := map[string]bool{}
	var out []string
	add := func(c string) {
		if c != "" && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	for _, z := range inv.Zones {
		for _, s := range inv.Services[z] {
			if s.Type == "SVC_GEN_GENERIC" && s.LogicalComponent == "" {
				add(s.Component)
			}
		}
	}
	for _, d := range inv.Defs.List {
		if d.LogicalComponent == "RacePointMedia_host" {
			add(d.Component)
		}
	}
	return out
}

func collectHost(ctx context.Context, tr transport.Transport, inv *Inventory, version string) Host {
	h := Host{
		UID:              inv.Manifest.HostUID,
		TemperatureUnits: inv.Manifest.TemperatureUnits,
		OS:               runtime.GOOS,
		Arch:             runtime.GOARCH,
		AgentVersion:     version,
	}
	for _, c := range hostCandidates(inv) {
		names := []string{c + ".SoftwareVersion", c + ".DeviceModel", c + ".SystemName", c + ".UniqueID"}
		v, err := tr.ReadStates(ctx, names)
		if err != nil || v[names[0]] == "" {
			continue
		}
		h.Component, h.Software, h.Model, h.Name = c, v[names[0]], v[names[1]], v[names[2]]
		if h.UID == "" {
			h.UID = v[names[3]]
		}
		break
	}
	if h.Name == "" {
		h.Name = h.Component
	}
	if h.Name == "" {
		h.Name = "Savant Host"
	}
	if h.UID == "" {
		h.UID = config.Slug(h.Name)
	}
	return h
}

type Skipped struct {
	What   string `json:"what"`
	Reason string `json:"reason"`
}

func Build(inv *Inventory, log *slog.Logger) ([]Entity, []Skipped) {
	var (
		out     []Entity
		skipped []Skipped
		ids     = map[string]int{}
	)
	add := func(e Entity) {
		m := e.Info()
		ids[m.ID]++
		if n := ids[m.ID]; n > 1 {
			m.ID = fmt.Sprintf("%s_%d", m.ID, n)
		}
		out = append(out, e)
	}

	fromApp := buildAppEntities(inv, add)
	for _, r := range inv.Rows {
		what := fmt.Sprintf("%s row %d %q", r.Table, r.Index, r.Name())
		if !r.Enabled {
			skipped = append(skipped, Skipped{what, "disabled in Blueprint"})
			continue
		}
		platform, ok := tableKind(r)
		if !ok {
			skipped = append(skipped, Skipped{what, "table not supported yet"})
			continue
		}
		if fromApp[platform] {
			continue
		}
		if platform == "climate" {
			if t := rowThermostat(inv, r); t != nil {
				add(t)
			} else {
				skipped = append(skipped, Skipped{what, "no current temperature state"})
			}
			continue
		}
		tgt := resolveTarget(inv, r)
		meta := Meta{
			ID:       platform + "." + objectID(r),
			Platform: platform,
			Name:     r.Name(),
			Zone:     r.Zone(),
			Device:   roomDevice(r.Zone(), platformDevice[platform]),
			Source:   what + " -> " + tgt.Service.String(),
		}
		switch platform {
		case "light":
			dim := lightDimmable(r, tgt)
			meta.Attributes = map[string]any{"dimmable": dim}
			add(&Light{Meta: meta, Target: tgt, Dimmable: dim,
				LevelState: pickState(r, "DimmerLevel", "CurrentDimmerLevel", "LightLevel", "IsLightOn", "LightIsOn")})
		case "cover":
			sh := &Shade{Target: tgt, Setter: "ShadeSet",
				LevelState: pickState(r, "ShadeLevel", "CurrentShadeLevel", "ShadeLevelMoving", "ShadePosition")}
			if tgt.Has("ShadeUp", false) {
				sh.Up = "ShadeUp"
			}
			if tgt.Has("ShadeDown", false) {
				sh.Down = "ShadeDown"
			}
			if tgt.Has("ShadeStop", true) {
				sh.Stop = "ShadeStop"
			}
			meta.Attributes = map[string]any{"position": sh.LevelState != "", "stop": sh.Stop != ""}
			sh.Meta = meta
			add(sh)
		case "lock":
			add(&Lock{Meta: meta, Target: tgt, StatusState: r.State("DoorLockStatus"), LockedState: r.State("IsDoorLocked")})
		}
	}

	sourceDevices := map[string][]*Device{}
	for _, z := range inv.Zones {
		room := buildMediaRoom(inv, z)
		if room == nil {
			continue
		}
		add(room)
		for _, src := range room.Sources {
			player := sourcePlayer(room, src)
			comp := src.Service.Component
			addModel(inv, comp, player.Device)
			sourceDevices[comp] = append(sourceDevices[comp], player.Device)
			add(player)
			if r := sourceRemote(player); r != nil {
				add(r)
			}
		}
	}

	zones := map[string]bool{}
	for _, z := range inv.Zones {
		zones[z] = true
	}
	for _, sc := range inv.Scenes {
		if missing := missingRooms(sc.Rooms, zones); len(missing) > 0 {
			skipped = append(skipped, Skipped{"scene " + sc.Name, "uses rooms that aren't in this config: " + strings.Join(missing, ", ")})
			continue
		}
		id := sc.ID
		if id == "" {
			id = sc.Name
		}
		add(&Scene{Meta: Meta{ID: "scene." + config.Slug(id), Platform: "scene", Name: sc.Name, Source: "Savant scene " + sc.ID}, Scene: sc})
	}

	if c := inv.Host.Component; c != "" {
		for _, s := range hostSensors(c) {
			add(s)
		}
	}
	for _, s := range energySensors(inv.Circuits) {
		add(s)
	}
	for _, e := range componentStatus(inv, sourceDevices) {
		add(e)
	}
	for _, r := range inv.Remotes {
		for _, e := range remoteEntities(inv, r) {
			add(e)
		}
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].Info().ID < out[j].Info().ID })
	return out, skipped
}

func appScenes(app []savantcfg.AppScene) []transport.Scene {
	out := make([]transport.Scene, 0, len(app))
	for _, a := range app {
		out = append(out, transport.Scene{Name: a.Name, ID: a.ID, User: a.User, Global: a.Global, Rooms: a.Rooms})
	}
	return out
}

func missingRooms(rooms []string, zones map[string]bool) []string {
	var missing []string
	for _, r := range rooms {
		if !zones[r] {
			missing = append(missing, r)
		}
	}
	return missing
}

func hostSensors(c string) []*Sensor {
	mk := func(key, name, state string, numeric bool, attrs map[string]any) *Sensor {
		attrs["entity_category"] = "diagnostic"
		return &Sensor{Meta: Meta{ID: "sensor.host_" + key, Platform: "sensor", Name: name, Attributes: attrs, Source: state}, StateName: state, Numeric: numeric}
	}
	pctAttrs := func(icon string) map[string]any {
		return map[string]any{"unit": "%", "state_class": "measurement", "icon": icon}
	}
	return []*Sensor{
		mk("cpu", "CPU usage", c+".SysStatus.CPUPercent", true, pctAttrs("mdi:cpu-64-bit")),
		mk("memory", "Memory usage", c+".SysStatus.RAMPercent", true, pctAttrs("mdi:memory")),
		mk("disk_free", "Disk free", c+".SysStatus.FreeDiskSpacePercentage", true, pctAttrs("mdi:harddisk")),
		mk("uptime", "Uptime", c+".SysStatus.Uptime.Days", true, map[string]any{"unit": "d", "device_class": "duration", "state_class": "measurement"}),
		mk("software", "Savant software", c+".SoftwareVersion", false, map[string]any{"icon": "mdi:information-outline"}),
	}
}

func buildMediaRoom(inv *Inventory, zone string) *MediaRoom {
	byName := map[string]*Source{}
	isTV := false
	for _, s := range inv.Services[zone] {
		if !strings.HasPrefix(s.Type, "SVC_AV_") {
			continue
		}
		def := inv.Defs.Find(zone, s.Component, s.LogicalComponent, s.Type, s.Variant)
		if def != nil && !def.Enabled {
			continue
		}
		name := inv.Manifest.ChildAliases[s.Dash()]
		if name == "" && def != nil {
			name = def.Alias
		}
		if name == "" {
			name = s.Component
		}
		if s.Type == "SVC_AV_TV" {
			isTV = true
		}
		src := byName[strings.ToLower(name)]
		if src == nil {
			src = &Source{Name: name, Services: map[string]bool{}}
			byName[strings.ToLower(name)] = src
		}
		src.Services[s.Dash()] = true
		if src.Service.Type == "" || betterPrimary(s, src.Service) {
			src.Service, src.Primary = s, def
		}
		if def != nil && def.AppName != "" && src.app(appLabel(def.AppName)) == nil {
			src.Apps = append(src.Apps, App{Name: appLabel(def.AppName), Service: s})
		}
	}
	for _, src := range byName {
		sortApps(src)
		src.NowPlaying = nowPlaying(inv, src.Primary)
	}
	if len(byName) == 0 {
		return nil
	}
	room := &MediaRoom{
		Meta: Meta{ID: "media_player." + config.Slug(zone), Platform: "media_player", Name: zone, Zone: zone,
			Device: roomDevice(zone, "media"), Source: "zone " + zone},
		Zone:        zone,
		ActiveState: zone + ".ActiveService",
		VolumeState: zone + ".CurrentVolume",
		MuteState:   zone + ".IsMuted",
	}
	var names []string
	class := "receiver"
	if isTV {
		class = "tv"
	}
	attrs := map[string]any{"kind": "room", "device_class": class, "volume_max": 100, "use_device_name": true}
	for _, k := range sortedKeys(byName) {
		src := byName[k]
		room.Sources = append(room.Sources, src)
		names = append(names, src.Name)
		for key, has := range playerFeatures(src.Primary) {
			attrs[key] = has || attrs[key] == true
		}
	}
	attrs["sources"] = names
	room.Attributes = attrs
	return room
}

func playerFeatures(d *savantcfg.ServiceDef) map[string]bool {
	out := map[string]bool{}
	if d == nil {
		return out
	}
	out["volume_step"] = d.HasRequest("VolumeUp")
	out["volume_mute"] = d.HasRequest("MuteOn")
	out["volume_set"] = d.HasRequest("SetVolume")
	for action, req := range transportRequests {
		out[action] = d.HasRequest(req)
	}
	out["shuffle"] = d.HasRequest("ShuffleOn")
	out["repeat"] = d.HasRequest("RepeatOn")
	return out
}

func nowPlaying(inv *Inventory, d *savantcfg.ServiceDef) map[string]string {
	if d == nil || len(inv.Known) == 0 {
		return nil
	}
	prefix := d.Component + "." + d.LogicalComponent + "."
	if !inv.Known[prefix+"CurrentSongName"] {
		return nil
	}
	out := map[string]string{}
	for k, st := range nowPlayingStates {
		if inv.Known[prefix+st] {
			out[k] = prefix + st
		}
	}
	return out
}

// Dealer-added requests that aren't apps to open.
var notShortcut = regexp.MustCompile(`(?i)restart|reboot|reset|power|pair|discover`)

func sourceShortcuts(d *savantcfg.ServiceDef) []string {
	if d == nil {
		return nil
	}
	var out []string
	for _, name := range sortedKeys(d.Custom) {
		if d.Custom[name] && len(d.Requests[name]) == 0 && !notShortcut.MatchString(name) {
			out = append(out, name)
		}
	}
	return out
}

var camelGap = regexp.MustCompile(`([a-z])([A-Z])`)

// appLabel turns names like "AmazonVideo" or "YoutubeTV" into "Amazon Video", "Youtube TV".
func appLabel(name string) string {
	return camelGap.ReplaceAllString(strings.TrimSpace(name), "$1 $2")
}

// sortApps puts the device's own main service first, then the apps by name.
// A source with only one service has no app list.
func sortApps(src *Source) {
	if len(src.Apps) < 2 {
		src.Apps = nil
		return
	}
	sort.SliceStable(src.Apps, func(i, j int) bool {
		a, b := src.Apps[i], src.Apps[j]
		if (a.Service == src.Service) != (b.Service == src.Service) {
			return a.Service == src.Service
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
}

func sourcePlayer(room *MediaRoom, src *Source) *SourcePlayer {
	attrs := map[string]any{"kind": "source", "device_class": "", "use_device_name": true}
	src.Shortcuts = sourceShortcuts(src.Primary)
	var names []string
	for _, a := range src.Apps {
		names = append(names, a.Name)
	}
	names = append(names, src.Shortcuts...)
	if len(names) > 0 {
		attrs["sources"] = names
	}
	if src.Service.Type == "SVC_AV_TV" {
		attrs["device_class"] = "tv"
	}
	for key, has := range playerFeatures(src.Primary) {
		attrs[key] = has
	}
	return &SourcePlayer{
		Meta: Meta{
			ID:         "media_player." + config.Slug(room.Zone+"_"+src.Name),
			Platform:   "media_player",
			Name:       src.Name,
			Zone:       room.Zone,
			Attributes: attrs,
			Device:     sourceDevice(room.Zone, src),
			Source:     "source " + src.Service.Dash(),
		},
		Room:   room,
		Source: src,
	}
}

func sourceDevice(zone string, src *Source) *Device {
	return &Device{ID: "source_" + config.Slug(zone+"_"+src.Name), Name: src.Name, Model: "Savant source"}
}

// remoteCommands are the requests worth listing for a remote: no arguments
// and nothing for power, pairing or Savant's own room requests.
func remoteCommands(d *savantcfg.ServiceDef) []string {
	var out []string
	for _, name := range sortedKeys(d.Requests) {
		if len(d.Requests[name]) > 0 || strings.HasPrefix(name, "__") || notShortcut.MatchString(name) ||
			strings.Contains(name, "App") || strings.Contains(name, "Text") {
			continue
		}
		out = append(out, name)
	}
	return out
}

func sourceRemote(p *SourcePlayer) *Remote {
	d := p.Source.Primary
	if d == nil || !(d.HasRequest("OSDCursorUp") || d.HasRequest("OSDSelect") || len(p.Source.Shortcuts) > 0) {
		return nil
	}
	m := p.Meta
	m.ID = "remote." + strings.TrimPrefix(p.ID, "media_player.")
	m.Platform = "remote"
	m.Attributes = map[string]any{"kind": "remote", "use_device_name": true, "commands": remoteCommands(d)}
	return &Remote{Meta: m, Player: p}
}

// Prefer a source's main service over its app inputs, then the lowest variant.
func betterPrimary(cand, cur transport.Service) bool {
	rank := func(s transport.Service) int {
		if strings.Contains(s.Type, "EXTERNALMEDIASERVER") {
			return 1
		}
		return 0
	}
	if rank(cand) != rank(cur) {
		return rank(cand) < rank(cur)
	}
	return len(cand.Variant) < len(cur.Variant) || (len(cand.Variant) == len(cur.Variant) && cand.Variant < cur.Variant)
}

func tableKind(r savantcfg.Row) (string, bool) {
	switch r.ServiceID {
	case "SVC_ENV_LIGHTING":
		return "light", true
	case "SVC_ENV_SHADE":
		return "cover", true
	case "SVC_ENV_DOORLOCK":
		return "lock", true
	case "SVC_ENV_HVAC", "SVC_ENV_SINGLE_SETPOINT_HVAC":
		return "climate", true
	}
	switch strings.ToLower(r.Table) {
	case "lighting":
		return "light", true
	case "shade", "shades":
		return "cover", true
	case "door lock":
		return "lock", true
	case "hvac":
		return "climate", true
	}
	return "", false
}

func resolveTarget(inv *Inventory, r savantcfg.Row) Target {
	prefer := append(append([]string{}, r.Zones...), r.ControllerZone)
	if d := inv.Defs.FindAnyZone(prefer, r.Controller, r.LogicalComponent, r.ServiceID); d != nil {
		return Target{
			Service: transport.Service{Zone: d.Zone, Component: d.Component, LogicalComponent: d.LogicalComponent, Variant: d.Variant, Type: d.Type},
			Def:     d,
			Fields:  r.Fields,
		}
	}
	return Target{
		Service: transport.Service{Zone: r.Zone(), Component: r.Controller, LogicalComponent: r.LogicalComponent, Variant: "1", Type: r.ServiceID},
		Fields:  r.Fields,
	}
}

func pickState(r savantcfg.Row, names ...string) string {
	if s := r.State(names...); s != "" {
		return s
	}
	if len(r.States) > 0 {
		return r.States[0].Name
	}
	return ""
}

func lightDimmable(r savantcfg.Row, t Target) bool {
	ui := strings.ToLower(r.UIType)
	switch {
	case strings.Contains(ui, "dim"), strings.Contains(ui, "slider"), strings.Contains(ui, "level"):
		return true
	case strings.Contains(ui, "switch"), strings.Contains(ui, "toggle"), strings.Contains(ui, "on/off"), strings.Contains(ui, "relay"):
		return false
	}
	return t.Has("DimmerSet", true)
}

// IDs come from the controller and addresses, so renaming a load in Blueprint
// keeps the same entity in Home Assistant.
func objectID(r savantcfg.Row) string {
	var addrs []string
	for _, k := range sortedKeys(r.Fields) {
		if v := r.Fields[k]; v != "" && addrKey.MatchString(k) {
			addrs = append(addrs, v)
		}
	}
	if len(addrs) == 0 {
		return config.Slug(r.Zone() + "_" + r.Name())
	}
	return config.Slug(r.Controller + "_" + strings.Join(addrs, "_"))
}
