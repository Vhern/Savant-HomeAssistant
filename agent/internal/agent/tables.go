package agent

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"savant-ha/internal/config"
	"savant-ha/internal/savantcfg"
	"savant-ha/internal/transport"
)

// Command is a button for one Savant request, like starting an EV charge.
type Command struct {
	Meta
	Target
	Req string
}

func (c *Command) States() []string            { return nil }
func (c *Command) State(Values) map[string]any { return map[string]any{} }

func (c *Command) Do(ctx context.Context, x Executor, action string, _ Params) error {
	if action != "press" {
		return errAction(&c.Meta, action)
	}
	return x.ServiceRequest(ctx, c.Request(c.Req, nil))
}

// WaterHeater shows a water heater's temperatures and sets its target.
type WaterHeater struct {
	Meta
	Target
	TempState, SetPointState, ModeState string
	SetTemp                             string
}

func (w *WaterHeater) States() []string { return nonEmpty(w.TempState, w.SetPointState, w.ModeState) }

func (w *WaterHeater) State(v Values) map[string]any {
	st := map[string]any{}
	if f, ok := transport.ParseLevel(v[w.TempState]); ok {
		st["temperature"] = f
	}
	if f, ok := transport.ParseLevel(v[w.SetPointState]); ok {
		st["target"] = f
	}
	if m := strings.TrimSpace(v[w.ModeState]); m != "" {
		st["mode"] = m
	}
	if len(st) == 0 {
		return nil
	}
	return st
}

func (w *WaterHeater) Do(ctx context.Context, x Executor, action string, p Params) error {
	f, ok := p.Float("temperature")
	arg := valueArg(w.Def, w.SetTemp)
	if action != "set_temperature" || !ok || arg == "" {
		return errAction(&w.Meta, action)
	}
	return x.ServiceRequest(ctx, w.Request(w.SetTemp, map[string]string{arg: strconv.Itoa(int(math.Round(f)))}))
}

// valueArg is the one argument a request takes besides its addresses, as the
// service defines it. Without a definition there's no way to know its name.
func valueArg(d *savantcfg.ServiceDef, req string) string {
	if d == nil || req == "" {
		return ""
	}
	var found []string
	for _, a := range d.Requests[req] {
		if !addrKey.MatchString(a) {
			found = append(found, a)
		}
	}
	if len(found) != 1 {
		return ""
	}
	return found[0]
}

func tableTarget(inv *Inventory, t savantcfg.AppTable, state, req string) Target {
	return appTarget(inv, t.Zone, "", state, nonEmpty(t.Fields["address"]), req)
}

func tableDevice(kind string, t savantcfg.AppTable, model string) *Device {
	return &Device{ID: kind + "_" + config.Slug(t.Zone+"_"+t.Name), Name: or(t.Name, model), Model: model}
}

// evCharger makes start and stop buttons plus a sensor for each state the
// charger's row names, showing Savant's values as they are.
func evCharger(inv *Inventory, t savantcfg.AppTable) []Entity {
	f := t.Fields
	dev := tableDevice("ev_charger", t, "Savant EV charger")
	key := "ev_charger_" + config.Slug(t.Zone+"_"+t.Name) + "_"
	var out []Entity
	for _, c := range []struct{ field, key, name string }{{"startCommand", "start", "Start charging"}, {"stopCommand", "stop", "Stop charging"}} {
		if req := f[c.field]; req != "" {
			tgt := tableTarget(inv, t, f["chargerState"], req)
			out = append(out, &Command{Meta: Meta{ID: "button." + key + c.key, Platform: "button", Name: c.name, Zone: t.Zone,
				Device: dev, Attributes: map[string]any{"icon": "mdi:ev-station"}, Source: fmt.Sprintf("EVChargerEntities %d %s", t.ID, req)},
				Target: tgt, Req: req})
		}
	}
	for _, s := range []struct {
		field, key, name string
		attrs            map[string]any
	}{
		{"chargerState", "status", "Status", map[string]any{"icon": "mdi:ev-station"}},
		{"chargeModeState", "mode", "Charge mode", map[string]any{"icon": "mdi:ev-plug-type2"}},
		{"powerState", "power", "Power", map[string]any{"icon": "mdi:flash"}},
		{"chargeRateState", "rate", "Charge rate", map[string]any{"icon": "mdi:speedometer"}},
		{"maxChargeRateState", "max_rate", "Max charge rate", map[string]any{"entity_category": "diagnostic"}},
		{"currentSessionEnergyState", "session_energy", "Session energy", map[string]any{"icon": "mdi:lightning-bolt"}},
		{"lastSessionEnergyState", "last_session_energy", "Last session energy", map[string]any{"entity_category": "diagnostic"}},
		{"errorState", "error", "Error", map[string]any{"entity_category": "diagnostic", "icon": "mdi:alert-circle-outline"}},
	} {
		if state := f[s.field]; state != "" {
			out = append(out, &Sensor{Meta: Meta{ID: "sensor." + key + s.key, Platform: "sensor", Name: s.name, Zone: t.Zone,
				Device: dev, Attributes: s.attrs, Source: state}, StateName: state})
		}
	}
	return out
}

func waterHeater(inv *Inventory, t savantcfg.AppTable) []Entity {
	f := t.Fields
	dev := tableDevice("water_heater", t, "Savant water heater")
	key := "water_heater_" + config.Slug(t.Zone+"_"+t.Name)
	tgt := tableTarget(inv, t, or(f["currentTempState"], f["currentSetPointState"]), f["setTempCommand"])
	out := []Entity{&WaterHeater{
		Meta: Meta{ID: "water_heater." + key, Platform: "water_heater", Name: t.Name, Zone: t.Zone, Device: dev,
			Attributes: map[string]any{"use_device_name": true, "settable": valueArg(tgt.Def, f["setTempCommand"]) != "", "unit": tempUnit(inv, false)},
			Source:     fmt.Sprintf("HotWaterHeaterEntities %d %q", t.ID, t.Name)},
		Target: tgt, TempState: f["currentTempState"], SetPointState: f["currentSetPointState"],
		ModeState: f["currentModeState"], SetTemp: f["setTempCommand"],
	}}
	for _, s := range []struct{ field, key, name, icon string }{
		{"hotWaterRemainState", "remaining", "Hot water remaining", "mdi:water-percent"},
		{"timeToSetPointState", "time_to_target", "Time to target", "mdi:timer-outline"},
	} {
		if state := f[s.field]; state != "" {
			out = append(out, &Sensor{Meta: Meta{ID: "sensor." + key + "_" + s.key, Platform: "sensor", Name: s.name, Zone: t.Zone,
				Device: dev, Attributes: map[string]any{"icon": s.icon}, Source: state}, StateName: state})
		}
	}
	return out
}
