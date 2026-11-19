package agent

import (
	"context"
	"fmt"
	"strings"

	"savant-ha/internal/savantcfg"
	"savant-ha/internal/transport"
)

// Garage is a garage door or gate. Its open state is either a yes/no or a
// word like "Opening" or "Closed", depending on the controller.
type Garage struct {
	Meta
	Target
	Open, Close, Stop, Toggle string
	OpenState, ErrorState     string
}

func (g *Garage) States() []string { return nonEmpty(g.OpenState, g.ErrorState) }

func (g *Garage) State(v Values) map[string]any {
	raw := strings.ToLower(strings.TrimSpace(v[g.OpenState]))
	st := map[string]any{}
	if b, ok := transport.ParseBool(raw); ok {
		st["state"] = map[bool]string{true: "open", false: "closed"}[b]
	} else {
		for _, w := range []string{"opening", "closing", "open", "closed", "stopped"} {
			if strings.Contains(raw, w) {
				st["state"] = w
				break
			}
		}
	}
	if e := strings.TrimSpace(v[g.ErrorState]); e != "" {
		if b, ok := transport.ParseBool(e); !ok || b {
			st["error"] = e
		}
	}
	if st["state"] == nil {
		return nil
	}
	return st
}

func (g *Garage) Do(ctx context.Context, x Executor, action string, _ Params) error {
	req := map[string]string{"open": or(g.Open, g.Toggle), "close": or(g.Close, g.Toggle), "stop": g.Stop}[action]
	if req == "" {
		return errAction(&g.Meta, action)
	}
	return x.ServiceRequest(ctx, g.Request(req, nil))
}

func appGarage(inv *Inventory, gd savantcfg.AppGarage) *Garage {
	tgt := appTarget(inv, gd.Zone, "", gd.OpenState, gd.Addresses, or(gd.Open, gd.Toggle), "GarageDoorAddress", "GarageDoorAddress2")
	if tgt.Service.Type == "" {
		tgt.Service.Type = "SVC_ENV_GARAGE"
	}
	meta := appMeta(tgt, gd.Zone, or(gd.Name, "Garage Door"), gd.Addresses, fmt.Sprintf("GarageEntities %d %q", gd.ID, gd.Name))
	meta.ID, meta.Platform = "cover."+meta.ID, "cover"
	meta.Device = roomDevice(gd.Zone, "garage")
	meta.Attributes = map[string]any{"kind": "garage", "stop": gd.Stop != ""}
	return &Garage{Meta: meta, Target: tgt, Open: gd.Open, Close: gd.Close, Stop: gd.Stop, Toggle: gd.Toggle,
		OpenState: gd.OpenState, ErrorState: gd.ErrorState}
}
