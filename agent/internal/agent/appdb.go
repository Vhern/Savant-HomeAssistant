package agent

import (
	"fmt"
	"regexp"
	"strings"

	"savant-ha/internal/config"
	"savant-ha/internal/savantcfg"
	"savant-ha/internal/transport"
)

var roomDevices = map[string][2]string{
	"lighting": {"Lighting", "Savant lighting"},
	"shades":   {"Shades", "Savant shades"},
	"keypads":  {"Keypads", "Savant keypads"},
	"locks":    {"Locks", "Savant door locks"},
	"media":    {"Media", "Savant room audio/video"},
}

var platformDevice = map[string]string{"light": "lighting", "switch": "lighting", "cover": "shades", "button": "keypads", "lock": "locks"}

func roomDevice(zone, kind string) *Device {
	d, ok := roomDevices[kind]
	if zone == "" || !ok {
		return nil
	}
	return &Device{ID: kind + "_" + config.Slug(zone), Name: d[0], Model: d[1]}
}

// buildAppEntities uses serviceImplementation.sqlite, which says what each row
// really is (an RGB DMX load, a keypad button, a shade group). It returns the
// platforms it covered so the data table rows for them are skipped.
func buildAppEntities(inv *Inventory, add func(Entity)) map[string]bool {
	covered := map[string]bool{}
	app := inv.App
	if app == nil {
		return covered
	}
	if len(app.Lights) > 0 {
		covered["light"] = true
		keypads := map[string]bool{}
		for _, l := range app.Lights {
			addAppLight(inv, l, keypads, add)
		}
	}
	if len(app.Shades) > 0 {
		covered["cover"] = true
		for _, s := range app.Shades {
			add(appShade(inv, s))
		}
	}
	// The data tables already handle locks well, and their IDs are what existing
	// installs use, so the database only fills in when they have none.
	if len(app.Locks) > 0 && !hasRows(inv, "lock") {
		covered["lock"] = true
		for _, l := range app.Locks {
			add(appLock(inv, l))
		}
	}
	for _, c := range app.Cameras {
		if cam := appCamera(c); cam != nil {
			add(cam)
		}
	}
	return covered
}

func appCamera(c savantcfg.AppCamera) *Camera {
	if c.Snapshot == "" && c.Stream == "" {
		return nil
	}
	key := config.Slug(c.Component + "_" + c.Logical)
	zone := c.Zone
	if strings.HasPrefix(zone, "SAVANT_GLOBAL") {
		zone = ""
	}
	secret := map[string]string{}
	for k, v := range map[string]string{"snapshot_url": c.Snapshot, "stream_url": c.Stream, "username": c.Username, "password": c.Password} {
		if v != "" {
			secret[k] = v
		}
	}
	return &Camera{Meta{
		ID: "camera." + key, Platform: "camera", Name: c.Name, Zone: zone,
		Device:     &Device{ID: "camera_" + key, Name: c.Name, Model: "Savant camera"},
		Attributes: map[string]any{"kind": "camera", "use_device_name": true, "snapshot": c.Snapshot != "", "stream": c.Stream != ""},
		Source:     fmt.Sprintf("CameraEntities %d %q", c.ID, c.Name),
		Secret:     secret,
	}}
}

func hasRows(inv *Inventory, platform string) bool {
	for _, r := range inv.Rows {
		if k, ok := tableKind(r); ok && k == platform && r.Enabled {
			return true
		}
	}
	return false
}

var placeholderName = regexp.MustCompile(`(?i)^(\d+|button \d+)?$`)

func addAppLight(inv *Inventory, l savantcfg.AppLight, keypads map[string]bool, add func(Entity)) {
	tgt := appTarget(inv, l.Zone, "SVC_ENV_LIGHTING", l.State, l.Addresses, or(l.Press, l.Release))
	meta := appMeta(tgt, l.Zone, l.Name, l.Addresses, fmt.Sprintf("LightEntities %d %q", l.ID, l.Name))
	isScene := l.Type == "Scene" || strings.HasPrefix(l.Press, "Button")
	switch {
	case isScene && (l.Release == "ActivateScene" || l.Press == "ActivateScene"):
		meta.ID, meta.Platform = "switch."+meta.ID, "switch"
		meta.Device = roomDevice(l.Zone, "lighting")
		meta.Attributes = map[string]any{"kind": "lighting_scene", "icon": "mdi:lightbulb-group"}
		add(&LightScene{Meta: meta, Target: tgt, StateName: l.State, On: "ActivateScene",
			Off: or(l.ToggleRelease, l.TogglePress)})
	case isScene:
		meta.ID, meta.Platform = "button."+meta.ID, "button"
		meta.Device = roomDevice(l.Zone, "keypads")
		if meta.Name == "" {
			meta.Name = "Button"
			if len(l.Addresses) > 1 {
				meta.Name += " " + l.Addresses[1]
			}
		}
		key := l.Zone + "\x00" + strings.ToLower(meta.Name)
		attrs := map[string]any{"kind": "keypad_button", "icon": "mdi:gesture-tap-button"}
		// Unnamed buttons and the same button on a second keypad stay hidden until enabled.
		if placeholderName.MatchString(strings.TrimSpace(l.Name)) || keypads[key] {
			attrs["disabled"] = true
		}
		keypads[key] = true
		meta.Attributes = attrs
		release := l.Release
		if release == l.Press {
			release = ""
		}
		add(&KeypadButton{Meta: meta, Target: tgt, LEDState: l.State, Press: or(l.Press, l.Release), Release: release})
	default:
		meta.ID, meta.Platform = "light."+meta.ID, "light"
		meta.Device = roomDevice(l.Zone, "lighting")
		if meta.Name == "" {
			meta.Name = "Load " + strings.Join(l.Addresses, " ")
		}
		light := &Light{Meta: meta, Target: tgt, LevelState: l.State,
			Dimmable: l.Dimmer != "" || l.Press == "DimmerSet" || (l.Press == "" && tgt.Has("DimmerSet", true))}
		if strings.Contains(l.State, "CurrentColor_") {
			light.ColorState = l.State
			light.LevelState = strings.Replace(l.State, "CurrentColor_", "CurrentDimmerLevel_", 1)
			light.RGB = strings.Contains(strings.ToUpper(l.SubType), "RGB")
			light.MinKelvin, light.MaxKelvin = l.MinKelvin, l.MaxKelvin
		}
		attrs := map[string]any{"dimmable": light.Dimmable, "type": strings.TrimSpace(l.Type + " " + l.SubType)}
		var modes []string
		if light.RGB {
			modes = append(modes, "rgbw")
		}
		if light.ColorState != "" && light.MaxKelvin > 0 {
			modes = append(modes, "color_temp")
			attrs["min_kelvin"], attrs["max_kelvin"] = light.MinKelvin, light.MaxKelvin
		}
		if len(modes) > 0 {
			attrs["color_modes"] = modes
		}
		light.Attributes = attrs
		add(light)
	}
}

func appShade(inv *Inventory, s savantcfg.AppShade) *Shade {
	setter := ""
	for _, c := range []string{s.Dimmer, s.Release, s.Press} {
		if strings.HasSuffix(c, "ShadeSet") {
			setter = c
			break
		}
	}
	tgt := appTarget(inv, s.Zone, "SVC_ENV_SHADE", s.State, s.Addresses, or(setter, s.Press))
	meta := appMeta(tgt, s.Zone, s.Name, s.Addresses, fmt.Sprintf("ShadeEntities %d %q", s.ID, s.Name))
	meta.ID, meta.Platform = "cover."+meta.ID, "cover"
	meta.Device = roomDevice(s.Zone, "shades")
	prefix := ""
	if strings.HasPrefix(setter, "Group") {
		prefix = "Group"
	}
	pick := func(name string) string {
		for _, n := range []string{prefix + name, name} {
			if tgt.Has(n, n == prefix+name) {
				return n
			}
		}
		return ""
	}
	if setter == "" {
		setter = pick("ShadeSet")
	}
	sh := &Shade{Meta: meta, Target: tgt, LevelState: s.State,
		Up: pick("ShadeUp"), Down: pick("ShadeDown"), Stop: pick("ShadeStop"), Setter: setter}
	if !strings.Contains(s.State, "Level") {
		sh.LevelState = ""
	}
	sh.Attributes = map[string]any{"position": sh.LevelState != "" && setter != "", "stop": sh.Stop != ""}
	return sh
}

func appLock(inv *Inventory, l savantcfg.AppLock) *Lock {
	tgt := appTarget(inv, l.Zone, "", l.LockState, l.Addresses, l.Lock)
	if tgt.Service.Type == "" {
		tgt.Service.Type = "SVC_ENV_DOORLOCK"
	}
	name := l.Name
	if name == "" {
		name = "Door Lock"
	}
	meta := appMeta(tgt, l.Zone, name, l.Addresses, fmt.Sprintf("DoorLockEntities %d %q", l.ID, l.Name))
	meta.ID, meta.Platform = "lock."+meta.ID, "lock"
	meta.Device = roomDevice(l.Zone, "locks")
	return &Lock{Meta: meta, Target: tgt, StatusState: l.LockState, LockReq: l.Lock, UnlockReq: l.Unlock}
}

// appMeta uses the same IDs as the data table rows (controller plus
// addresses), so entities keep their history when the source changes.
func appMeta(tgt Target, zone, name string, addrs []string, what string) Meta {
	id := config.Slug(tgt.Service.Component + "_" + strings.Join(addrs, "_"))
	if len(addrs) == 0 {
		id = config.Slug(zone + "_" + name)
	}
	return Meta{ID: id, Name: name, Zone: zone, Source: what + " -> " + tgt.Service.String()}
}

// appTarget finds the service a row's requests go to, using the component and
// logical component in its state name ("Main Host.Lighting.CurrentColor_4_001").
func appTarget(inv *Inventory, zone, typ, state string, addrs []string, req string) Target {
	comp, logical := "", ""
	if p := strings.SplitN(state, ".", 3); len(p) == 3 {
		comp, logical = p[0], p[1]
	}
	fields := map[string]string{}
	for i, a := range addrs {
		fields[fmt.Sprintf("Address%d", i+1)] = a
	}
	var best *savantcfg.ServiceDef
	score := -1
	for _, d := range inv.Defs.List {
		if typ != "" && d.Type != typ {
			continue
		}
		if comp != "" && (d.Component != comp || d.LogicalComponent != logical) {
			continue
		}
		if comp == "" && typ == "" {
			continue
		}
		s := 0
		if d.Zone == zone {
			s += 2
		}
		if req != "" && d.HasRequest(req) {
			s++
		}
		if s > score || (s == score && d.Variant < best.Variant) {
			best, score = d, s
		}
	}
	if best != nil {
		return Target{Service: serviceOf(best), Def: best, Fields: fields}
	}
	return Target{Service: transport.Service{Zone: zone, Component: comp, LogicalComponent: logical, Variant: "1", Type: typ}, Fields: fields}
}

func serviceOf(d *savantcfg.ServiceDef) transport.Service {
	return transport.Service{Zone: d.Zone, Component: d.Component, LogicalComponent: d.LogicalComponent, Variant: d.Variant, Type: d.Type}
}
