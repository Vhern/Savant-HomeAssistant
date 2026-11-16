package agent

import (
	"savant-ha/internal/config"
	"savant-ha/internal/savantcfg"
)

var energyDevice = &Device{ID: "energy", Name: "Energy", Model: "Savant energy"}

// energySensors mirrors the Savant app's energy page: whole-home power and
// today's usage, power per group (Lighting...) and per circuit.
func energySensors(circuits []savantcfg.Circuit) []*Sensor {
	if len(circuits) == 0 {
		return nil
	}
	mk := func(key, name, state string, attrs map[string]any) *Sensor {
		numeric := attrs["unit"] != nil
		return &Sensor{Meta: Meta{ID: "sensor.energy_" + key, Platform: "sensor", Name: name, Attributes: attrs,
			Device: energyDevice, Source: state}, StateName: state, Numeric: numeric}
	}
	power := func() map[string]any {
		return map[string]any{"unit": "W", "device_class": "power", "state_class": "measurement"}
	}
	energy := func() map[string]any {
		return map[string]any{"unit": "Wh", "device_class": "energy", "state_class": "total_increasing"}
	}
	out := []*Sensor{
		mk("power", "Power", "Energy.Total.Consumption.Power", power()),
		mk("today", "Today", "Energy.TotalToday.Consumption.Power", energy()),
		mk("average_today", "Average power today", "Energy.AverageToday.Consumption.Power", power()),
		mk("average_yesterday", "Average power yesterday", "Energy.AverageYesterday.Consumption.Power", power()),
		mk("mode", "Mode", "Energy.ModeName", map[string]any{"icon": "mdi:leaf"}),
	}
	groups := map[string]bool{}
	production := false
	for _, c := range circuits {
		production = production || c.Classification == "Production"
		if c.Group != "" && !groups[c.Group] {
			groups[c.Group] = true
			out = append(out, mk("group_"+config.Slug(c.Group)+"_power", c.Group+" power", "Energy.Group."+c.Group+".Power", power()))
		}
	}
	if production {
		out = append(out, mk("production", "Production power", "Energy.Total.Production.Power", power()))
	}
	for _, c := range circuits {
		key := "circuit_" + config.Slug(c.Name)
		base := "Energy.Circuit." + c.Name + "."
		e := energy()
		e["disabled"] = true
		st := map[string]any{"entity_category": "diagnostic", "icon": "mdi:information-outline"}
		out = append(out,
			mk(key+"_power", c.Name+" power", base+"Power", power()),
			mk(key+"_energy", c.Name+" energy", base+"Energy", e),
			mk(key+"_status", c.Name+" status", base+"StatusName", st),
		)
	}
	return out
}
