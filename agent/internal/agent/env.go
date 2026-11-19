package agent

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"savant-ha/internal/config"
	"savant-ha/internal/savantcfg"
)

var securityDevice = &Device{ID: "security", Name: "Security", Model: "Savant security"}

// AlarmPanel is one partition of a security system. Only Savant's own
// arming words are translated; anything else leaves the state unknown.
type AlarmPanel struct {
	Meta
	Target
	StatusState string
	Partition   string
}

func (a *AlarmPanel) States() []string { return nonEmpty(a.StatusState) }

func (a *AlarmPanel) State(v Values) map[string]any {
	raw := strings.TrimSpace(v[a.StatusState])
	if raw == "" {
		return nil
	}
	st := map[string]any{"status": raw}
	if s, ok := armingStates[raw]; ok {
		st["state"] = s
	}
	return st
}

// armingStates are the values of Savant's CurrentPartitionArmingStatus.
var armingStates = map[string]string{
	"Disarmed": "disarmed", "ArmedAway": "armed_away", "ArmedStay": "armed_home", "ArmedInstant": "armed_home",
	"ArmedNightStay": "armed_night", "ArmedVacation": "armed_vacation", "AlarmTriggered": "triggered",
}

var alarmRequests = map[string]string{
	"arm_away": "ArmAlarmAway", "arm_home": "ArmAlarmStay", "arm_night": "ArmAlarmNight",
	"arm_vacation": "ArmAlarmVacation", "disarm": "DisarmAlarm",
}

func (a *AlarmPanel) Do(ctx context.Context, x Executor, action string, p Params) error {
	req := alarmRequests[action]
	if req == "" || !a.Has(req, false) {
		return errAction(&a.Meta, action)
	}
	extra := map[string]string{"PartitionNumber": a.Partition}
	if code := p.String("code"); code != "" {
		extra["UserCode"] = code
	}
	return x.ServiceRequest(ctx, a.Request(req, extra))
}

// appAlarm makes a partition into an alarm panel and a zone into a sensor
// showing Savant's own status text.
func appAlarm(inv *Inventory, a savantcfg.AppAlarm) Entity {
	key := config.Slug(or(a.Component, "security") + "_" + a.Partition + "_" + a.AlarmZone)
	zone := a.Zone
	if strings.HasPrefix(zone, "SAVANT_GLOBAL") {
		zone = ""
	}
	what := fmt.Sprintf("SecuritySystemEntities %d %q", a.ID, a.Name)
	switch a.Type {
	case "Zone":
		return &Sensor{Meta: Meta{ID: "sensor.security_zone_" + key, Platform: "sensor", Name: or(a.Name, "Zone "+a.AlarmZone),
			Zone: zone, Device: securityDevice, Attributes: map[string]any{"icon": "mdi:shield-home-outline"}, Source: what},
			StateName: a.StatusState}
	case "Partition":
	default:
		return nil
	}
	tgt := appTarget(inv, zone, "", a.StatusState, nil, "DisarmAlarm")
	if tgt.Def == nil {
		return nil
	}
	var modes []string
	for _, act := range []string{"arm_away", "arm_home", "arm_night", "arm_vacation"} {
		if tgt.Def.HasRequest(alarmRequests[act]) {
			modes = append(modes, act)
		}
	}
	return &AlarmPanel{Meta: Meta{ID: "alarm_control_panel.security_" + key, Platform: "alarm_control_panel",
		Name: or(a.Name, "Partition "+a.Partition), Zone: zone, Device: securityDevice,
		Attributes: map[string]any{"modes": modes}, Source: what}, Target: tgt, StatusState: a.StatusState, Partition: a.Partition}
}

// PoolAux switches a pool or spa auxiliary. Savant doesn't report their
// state, so it stays unknown.
type PoolAux struct {
	Meta
	Target
	Aux string
}

func (p *PoolAux) States() []string            { return nil }
func (p *PoolAux) State(Values) map[string]any { return map[string]any{} }

func (p *PoolAux) Do(ctx context.Context, x Executor, action string, _ Params) error {
	switch action {
	case "turn_on":
		return x.ServiceRequest(ctx, p.Request("SetAuxiliary"+p.Aux+"On", nil))
	case "turn_off":
		return x.ServiceRequest(ctx, p.Request("SetAuxiliary"+p.Aux+"Off", nil))
	}
	return errAction(&p.Meta, action)
}

func appPool(inv *Inventory, pl savantcfg.AppPool) *PoolAux {
	if n, err := strconv.Atoi(pl.Aux); err != nil || n <= 0 {
		return nil
	}
	tgt := appTarget(inv, pl.Zone, "SVC_ENV_POOLANDSPA", "", nil, "SetAuxiliary"+pl.Aux+"On")
	if tgt.Def == nil {
		return nil
	}
	zone := pl.Zone
	if strings.HasPrefix(zone, "SAVANT_GLOBAL") {
		zone = ""
	}
	return &PoolAux{Meta: Meta{ID: "switch.pool_aux_" + config.Slug(tgt.Service.Component+"_"+pl.Aux), Platform: "switch",
		Name: or(pl.Name, "Auxiliary "+pl.Aux), Zone: zone, Device: &Device{ID: "pool", Name: "Pool & Spa", Model: "Savant pool and spa"},
		Attributes: map[string]any{"assumed": true, "icon": "mdi:pool"}, Source: fmt.Sprintf("PoolAndSpaEntities %d %q", pl.ID, pl.Name)},
		Target: tgt, Aux: pl.Aux}
}
