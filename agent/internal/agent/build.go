package agent

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
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
	Zones      []string                       `json:"zones"`
	Services   map[string][]transport.Service `json:"services"`
	Scenes     []transport.Scene              `json:"scenes"`
	AppScenes  []transport.Scene              `json:"-"`
	Rows       []savantcfg.Row                `json:"-"`
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
		if defs, err := savantcfg.ReadServices(filepath.Join(dir, "serviceImplementation.xml")); err != nil {
			inv.warn(log, "serviceImplementation.xml: %v", err)
		} else {
			inv.Defs = defs
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
	}

	inv.Scenes = collectScenes(ctx, tr, inv, log)
	inv.Host = collectHost(ctx, tr, inv, version)
	if withStateNames {
		if inv.StateNames, err = tr.StateNames(ctx, ""); err != nil {
			inv.warn(log, "statenames: %v", err)
		}
	}
	return inv, nil
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
		tgt := resolveTarget(inv, r)
		meta := Meta{
			ID:       platform + "." + objectID(r),
			Platform: platform,
			Name:     r.Name(),
			Zone:     r.Zone(),
			Source:   what + " -> " + tgt.Service.String(),
		}
		switch platform {
		case "light":
			dim := lightDimmable(r, tgt)
			meta.Attributes = map[string]any{"dimmable": dim}
			add(&Light{Meta: meta, Target: tgt, Dimmable: dim,
				LevelState: pickState(r, "DimmerLevel", "CurrentDimmerLevel", "LightLevel", "IsLightOn", "LightIsOn")})
		case "cover":
			lvl := pickState(r, "ShadeLevel", "CurrentShadeLevel", "ShadeLevelMoving", "ShadePosition")
			meta.Attributes = map[string]any{"position": lvl != "", "stop": tgt.Has("ShadeStop", true)}
			add(&Shade{Meta: meta, Target: tgt, LevelState: lvl})
		case "lock":
			add(&Lock{Meta: meta, Target: tgt, StatusState: r.State("DoorLockStatus"), LockedState: r.State("IsDoorLocked")})
		}
	}

	for _, z := range inv.Zones {
		room := buildMediaRoom(inv, z)
		if room == nil {
			continue
		}
		add(room)
		for _, src := range room.Sources {
			add(sourcePlayer(room, src))
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
	}
	if len(byName) == 0 {
		return nil
	}
	room := &MediaRoom{
		Meta:        Meta{ID: "media_player." + config.Slug(zone), Platform: "media_player", Name: zone, Zone: zone, Source: "zone " + zone},
		Zone:        zone,
		ActiveState: zone + ".ActiveService",
		VolumeState: zone + ".CurrentVolume",
		MuteState:   zone + ".IsMuted",
	}
	var names []string
	var step, mute, set bool
	for _, k := range sortedKeys(byName) {
		src := byName[k]
		room.Sources = append(room.Sources, src)
		names = append(names, src.Name)
		if d := src.Primary; d != nil {
			step = step || d.HasRequest("VolumeUp")
			mute = mute || d.HasRequest("MuteOn")
			set = set || d.HasRequest("SetVolume")
		}
	}
	class := "receiver"
	if isTV {
		class = "tv"
	}
	room.Attributes = map[string]any{
		"kind": "room", "sources": names, "device_class": class,
		"volume_step": step, "volume_mute": mute, "volume_set": set, "volume_max": 100,
	}
	return room
}

func sourcePlayer(room *MediaRoom, src *Source) *SourcePlayer {
	attrs := map[string]any{"kind": "source", "device_class": ""}
	if src.Service.Type == "SVC_AV_TV" {
		attrs["device_class"] = "tv"
	}
	if d := src.Primary; d != nil {
		attrs["volume_step"] = d.HasRequest("VolumeUp")
		attrs["volume_mute"] = d.HasRequest("MuteOn")
		attrs["volume_set"] = d.HasRequest("SetVolume")
		for action, req := range transportRequests {
			attrs[action] = d.HasRequest(req)
		}
	}
	return &SourcePlayer{
		Meta: Meta{
			ID:         "media_player." + config.Slug(room.Zone+"_"+src.Name),
			Platform:   "media_player",
			Name:       src.Name,
			Zone:       room.Zone,
			Attributes: attrs,
			Source:     "source " + src.Service.Dash(),
		},
		Room:   room,
		Source: src,
	}
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
	}
	switch strings.ToLower(r.Table) {
	case "lighting":
		return "light", true
	case "shade", "shades":
		return "cover", true
	case "door lock":
		return "lock", true
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
