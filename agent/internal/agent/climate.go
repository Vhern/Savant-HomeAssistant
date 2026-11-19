package agent

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"savant-ha/internal/savantcfg"
	"savant-ha/internal/transport"
)

// Thermostat states are "<component>.<logical>.<name>_<address>", like
// "HVAC Controller.HVAC_controller.ThermostatCurrentTemperature_1".
type Thermostat struct {
	Meta
	Target
	Prefix, Suffix string
	Single         bool
	states         []string
}

var thermostatStates = []string{
	"ThermostatCurrentTemperature", "ThermostatCurrentSetPoint", "ThermostatCurrentHeatPoint",
	"ThermostatCurrentCoolPoint", "ThermostatMode", "ThermostatHVACState", "ThermostatFanMode",
	"ThermostatCurrentHumidity", "ThermostatCurrentHumiditySetPoint",
	"IsCurrentHVACModeOff", "IsCurrentHVACModeHeat", "IsCurrentHVACModeCool", "IsCurrentHVACModeAuto",
	"ThermostatCurrentFanModeOn", "ThermostatCurrentFanModeAuto", "ThermostatCurrentFanModeCycle",
}

func (t *Thermostat) st(name string) string { return t.Prefix + name + t.Suffix }

func (t *Thermostat) States() []string { return t.states }

func (t *Thermostat) State(v Values) map[string]any {
	st := map[string]any{}
	num := func(key, state string) {
		if f, ok := transport.ParseLevel(v[t.st(state)]); ok {
			st[key] = f
		}
	}
	num("temperature", "ThermostatCurrentTemperature")
	num("humidity", "ThermostatCurrentHumidity")
	num("target_humidity", "ThermostatCurrentHumiditySetPoint")
	if t.Single {
		num("target", "ThermostatCurrentSetPoint")
	} else {
		num("heat", "ThermostatCurrentHeatPoint")
		num("cool", "ThermostatCurrentCoolPoint")
	}
	if m := strings.ToLower(strings.TrimSpace(v[t.st("ThermostatMode")])); m != "" {
		st["mode"] = m
	} else {
		for _, m := range []string{"Off", "Heat", "Cool", "Auto"} {
			if on, _ := transport.ParseBool(v[t.st("IsCurrentHVACMode"+m)]); on {
				st["mode"] = strings.ToLower(m)
			}
		}
	}
	if a := strings.ToLower(strings.TrimSpace(v[t.st("ThermostatHVACState")])); a != "" {
		st["action"] = a
	}
	if f := strings.ToLower(strings.TrimSpace(v[t.st("ThermostatFanMode")])); f != "" {
		if strings.Contains(f, "auto") {
			f = "auto"
		}
		st["fan"] = f
	} else {
		for _, m := range []string{"On", "Auto", "Cycle"} {
			if on, _ := transport.ParseBool(v[t.st("ThermostatCurrentFanMode"+m)]); on {
				st["fan"] = strings.ToLower(m)
			}
		}
	}
	if st["temperature"] == nil && st["mode"] == nil {
		return nil
	}
	return st
}

func temp(f float64) string {
	return strconv.FormatFloat(math.Round(f*2)/2, 'f', -1, 64)
}

func (t *Thermostat) Do(ctx context.Context, x Executor, action string, p Params) error {
	send := func(name string, extra map[string]string) error {
		if !t.Has(name, true) {
			return fmt.Errorf("%s has no %s request", t.Name, name)
		}
		return x.ServiceRequest(ctx, t.Request(name, extra))
	}
	switch action {
	case "set_mode":
		return send("SetHVACMode"+upperFirst(p.String("mode")), nil)
	case "set_fan":
		return send("SetFanMode"+upperFirst(p.String("fan")), nil)
	case "set_humidity":
		h, ok := p.Float("humidity")
		if !ok {
			return fmt.Errorf("%s: set_humidity needs humidity", t.ID)
		}
		return send("SetHumiditySetPoint", map[string]string{"HumidityPoint": strconv.Itoa(int(math.Round(h)))})
	case "set_temperature":
		if f, ok := p.Float("temperature"); ok && t.Single {
			return send("SetSingleSetPointTemperature", map[string]string{"SetPointTemperature": temp(f)})
		}
		if f, ok := p.Float("heat"); ok {
			if err := send("SetHeatPointTemperature", map[string]string{"HeatPointTemperature": temp(f)}); err != nil {
				return err
			}
		}
		if f, ok := p.Float("cool"); ok {
			return send("SetCoolPointTemperature", map[string]string{"CoolPointTemperature": temp(f)})
		}
		return nil
	}
	return errAction(&t.Meta, action)
}

func tempUnit(inv *Inventory, celsius bool) string {
	u := strings.ToUpper(inv.Host.TemperatureUnits)
	if celsius || u == "C" || strings.HasPrefix(u, "CELS") {
		return "C"
	}
	return "F"
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

type thermostatRow struct {
	zone, name, state string
	addrs             []string
	heat, cool, auto  bool
	humidity          bool
	minT, maxT        float64
	celsius           bool
	what              string
}

func buildThermostat(inv *Inventory, r thermostatRow) *Thermostat {
	tgt := appTarget(inv, r.zone, "", r.state, r.addrs, "SetHVACModeOff", "ThermostatAddress", "ThermostatAddress2")
	if tgt.Service.Type == "" {
		tgt.Service.Type = "SVC_ENV_HVAC"
	}
	single := tgt.Service.Type == "SVC_ENV_SINGLE_SETPOINT_HVAC" || (tgt.Def != nil && tgt.Def.HasRequest("SetSingleSetPointTemperature"))
	i := strings.LastIndex(r.state, "ThermostatCurrentTemperature")
	if i < 0 {
		i = strings.LastIndex(r.state, ".") + 1
	}
	prefix, suffix := r.state[:i], ""
	if j := strings.LastIndexByte(r.state, '_'); j > i {
		suffix = r.state[j:]
	}
	meta := appMeta(tgt, r.zone, or(r.name, "Thermostat"), r.addrs, r.what)
	meta.ID, meta.Platform = "climate."+meta.ID, "climate"
	meta.Device = roomDevice(r.zone, "climate")
	var modes []string
	for _, m := range []struct {
		on   bool
		name string
	}{{r.heat, "heat"}, {r.cool, "cool"}, {r.auto, "auto"}} {
		if m.on {
			modes = append(modes, m.name)
		}
	}
	attrs := map[string]any{"modes": modes, "single_setpoint": single, "unit": tempUnit(inv, r.celsius),
		"humidity": r.humidity && (tgt.Def == nil || tgt.Def.HasRequest("SetHumiditySetPoint"))}
	var fans []string
	for _, f := range []string{"auto", "on", "cycle"} {
		if tgt.Has("SetFanMode"+upperFirst(f), f != "cycle") {
			fans = append(fans, f)
		}
	}
	if len(fans) > 1 {
		attrs["fan_modes"] = fans
	}
	if r.maxT > r.minT && r.minT > 0 {
		attrs["min_temp"], attrs["max_temp"] = r.minT, r.maxT
	}
	meta.Attributes = attrs
	t := &Thermostat{Meta: meta, Target: tgt, Prefix: prefix, Suffix: suffix, Single: single}
	for _, s := range thermostatStates {
		if name := t.st(s); len(inv.Known) == 0 || inv.Known[name] {
			t.states = append(t.states, name)
		}
	}
	return t
}

func appThermostat(inv *Inventory, h savantcfg.AppHVAC) *Thermostat {
	return buildThermostat(inv, thermostatRow{zone: h.Zone, name: h.Name, state: h.State, addrs: h.Addresses,
		heat: h.Heat, cool: h.Cool, auto: h.Auto, humidity: h.Humidity, minT: h.MinTemp, maxT: h.MaxTemp,
		celsius: h.Celsius, what: fmt.Sprintf("HVACEntities %d %q", h.ID, h.Name)})
}

// rowThermostat reads an HVAC data table row, where the flags are columns
// like "Heat" and "Humidity" and State1 is the current temperature.
func rowThermostat(inv *Inventory, r savantcfg.Row) *Thermostat {
	state := r.State("ThermostatCurrentTemperature")
	if state == "" {
		return nil
	}
	var addrs []string
	for _, k := range []string{"ThermostatAddress", "ThermostatAddress2"} {
		if v := r.Fields[k]; v != "" {
			addrs = append(addrs, v)
		}
	}
	return buildThermostat(inv, thermostatRow{zone: r.Zone(), name: r.Name(), state: state, addrs: addrs,
		heat: r.Flag("Heat"), cool: r.Flag("Cool"), auto: r.Flag("Auto"), humidity: r.Flag("Humidity"),
		what: fmt.Sprintf("%s row %d %q", r.Table, r.Index, r.Name())})
}
