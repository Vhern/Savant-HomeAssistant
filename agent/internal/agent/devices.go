package agent

import (
	"regexp"
	"sort"
	"strings"

	"savant-ha/internal/config"
)

// componentStatus gives each device Savant controls (TVs, receivers, the
// Apple TV) a connection sensor, and power and input when the device reports
// them. A component that's a source in one room joins that source's device.
func componentStatus(inv *Inventory, sourceDevices map[string][]*Device) []Entity {
	if len(inv.Known) == 0 {
		return nil
	}
	byComp := map[string][]string{}
	for name := range inv.Known {
		comp, rest, ok := strings.Cut(name, ".")
		if !ok {
			continue
		}
		if comp == "global" {
			if c, ok := strings.CutSuffix(rest, ".ControlIsConnected"); ok {
				byComp[c] = append(byComp[c], name)
			}
			continue
		}
		byComp[comp] = append(byComp[comp], name)
	}
	var out []Entity
	for _, comp := range sortedKeys(byComp) {
		connected := "global." + comp + ".ControlIsConnected"
		if comp == inv.Host.Component || !inv.Known[connected] {
			continue
		}
		dev := componentDevice(inv, comp, sourceDevices[comp])
		key := config.Slug(comp)
		zone := componentZone(inv, comp)
		mk := func(id, name, state string, attrs map[string]any) Meta {
			return Meta{ID: id + key + "_" + config.Slug(name), Name: name, Zone: zone, Device: dev, Attributes: attrs, Source: state}
		}
		out = append(out, &BoolSensor{Meta: withPlatform(mk("binary_sensor.", "Connected", connected,
			map[string]any{"device_class": "connectivity", "entity_category": "diagnostic"}), "binary_sensor"), StateName: connected})
		names := byComp[comp]
		sort.Strings(names)
		for _, n := range names {
			logical, state := splitState(comp, n)
			switch state {
			case "IsPowered":
				out = append(out, &BoolSensor{Meta: withPlatform(mk("binary_sensor.", zoneLabel(logical)+"Power", n,
					map[string]any{"device_class": "power"}), "binary_sensor"), StateName: n})
			case "CurrentInput":
				out = append(out, &Sensor{Meta: withPlatform(mk("sensor.", zoneLabel(logical)+"Input", n,
					map[string]any{"icon": "mdi:import"}), "sensor"), StateName: n})
			}
		}
	}
	return out
}

func withPlatform(m Meta, platform string) Meta {
	m.Platform = platform
	return m
}

// splitState turns "Family Room AVR.Receiver_2.IsPowered" into "Receiver_2", "IsPowered".
func splitState(comp, name string) (string, string) {
	rest := strings.TrimPrefix(name, comp+".")
	i := strings.LastIndexByte(rest, '.')
	if i < 0 {
		return "", rest
	}
	return rest[:i], rest[i+1:]
}

var zoneSuffix = regexp.MustCompile(`_(\d+)$`)

// zoneLabel names a receiver's extra zones: "Receiver_2" becomes "Zone 2 ".
func zoneLabel(logical string) string {
	if m := zoneSuffix.FindStringSubmatch(logical); m != nil {
		return "Zone " + m[1] + " "
	}
	return ""
}

func componentZone(inv *Inventory, comp string) string {
	if inv.App == nil {
		return ""
	}
	z := inv.App.Components[comp].Zone
	if strings.HasPrefix(z, "[") || strings.HasPrefix(z, "SAVANT_GLOBAL") {
		return ""
	}
	return z
}

func componentDevice(inv *Inventory, comp string, sources []*Device) *Device {
	if len(sources) == 1 {
		return sources[0]
	}
	d := &Device{ID: "component_" + config.Slug(comp), Name: comp, Model: "Savant controlled device"}
	addModel(inv, comp, d)
	return d
}

func addModel(inv *Inventory, comp string, d *Device) {
	if inv.App == nil {
		return
	}
	if c, ok := inv.App.Components[comp]; ok && c.Model != "" {
		d.Model, d.Manufacturer = c.Model, c.Manufacturer
	}
}

var remoteID = regexp.MustCompile(`^[0-9A-F]{16}$`)

// ProRemote is a handheld Savant remote, known by the 16-character ID its
// states use. Its values are shown exactly as Savant reports them.
type ProRemote struct {
	ID, Name, Model, Make string
}

func findRemotes(inv *Inventory) []string {
	var ids []string
	for name := range inv.Known {
		if id, ok := strings.CutSuffix(name, ".BatteryLevel"); ok && remoteID.MatchString(id) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func remoteEntities(inv *Inventory, r ProRemote) []Entity {
	dev := &Device{ID: "remote_" + strings.ToLower(r.ID), Name: or(r.Name, r.ID), Model: r.Model, Manufacturer: r.Make}
	var out []Entity
	for _, s := range []struct{ state, key, name, icon string }{
		{"BatteryLevel", "battery", "Battery", "mdi:battery"},
		{"WifiStrength", "wifi", "Wi-Fi signal", "mdi:wifi"},
		{"CurrentZone", "room", "Room", "mdi:map-marker"},
		{"IsAwake", "awake", "Awake", "mdi:sleep-off"},
		{"Status", "status", "Status", "mdi:information-outline"},
		{"FirmwareVersion", "firmware", "Firmware", "mdi:chip"},
	} {
		state := r.ID + "." + s.state
		if !inv.Known[state] {
			continue
		}
		attrs := map[string]any{"icon": s.icon}
		if s.key == "status" || s.key == "firmware" || s.key == "wifi" {
			attrs["entity_category"] = "diagnostic"
		}
		out = append(out, &Sensor{Meta: Meta{ID: "sensor.remote_" + strings.ToLower(r.ID) + "_" + s.key, Platform: "sensor",
			Name: s.name, Device: dev, Attributes: attrs, Source: state}, StateName: state})
	}
	return out
}
