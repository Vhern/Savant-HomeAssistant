// Package savantcfg reads the host's Blueprint config files. It never writes them.
package savantcfg

import (
	"compress/gzip"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"howett.net/plist"
)

var configDirs = map[string][]string{
	"linux": {
		"/data/RPM/GNUstep/Library/ApplicationSupport/RacePointMedia/userConfig.rpmConfig",
		"~/GNUstep/Library/ApplicationSupport/RacePointMedia/userConfig.rpmConfig",
	},
	"darwin": {
		"~/Library/Application Support/RacePointMedia/userConfig.rpmConfig",
		"/Users/RPM/Library/Application Support/RacePointMedia/userConfig.rpmConfig",
		"/Users/Shared/Savant/Library/Application Support/RacePointMedia/userConfig.rpmConfig",
	},
}

func LocateConfigDir() (string, error) {
	cands := configDirs[runtime.GOOS]
	if d := os.Getenv("RPM_APPSUPPORT_DIR"); d != "" {
		cands = append([]string{filepath.Join(d, "userConfig.rpmConfig")}, cands...)
	}
	for _, d := range cands {
		if strings.HasPrefix(d, "~/") {
			h, err := os.UserHomeDir()
			if err != nil {
				continue
			}
			d = filepath.Join(h, d[2:])
		}
		if fi, err := os.Stat(filepath.Join(d, "serviceImplementation.xml")); err == nil && !fi.IsDir() {
			return d, nil
		}
	}
	return "", errors.New("userConfig.rpmConfig not found; set config_dir in the agent config")
}

type StateRef struct {
	Key       string
	Name      string
	StateName string
}

type Row struct {
	Table            string
	Index            int
	Entity           string
	Label            string
	Zones            []string
	ControllerZone   string
	Controller       string
	LogicalComponent string
	ServiceID        string
	UIType           string
	Enabled          bool
	States           []StateRef
	Fields           map[string]string
	Raw              map[string]any
}

func (r Row) Name() string {
	if r.Label != "" {
		return r.Label
	}
	if r.Entity != "" {
		return r.Entity
	}
	return fmt.Sprintf("%s %d", r.Table, r.Index+1)
}

func (r Row) Zone() string {
	if len(r.Zones) > 0 {
		return r.Zones[0]
	}
	return r.ControllerZone
}

// Flag reads a yes/no column like "Heat" or "Humidity".
func (r Row) Flag(k string) bool { return truthy(r.Raw[k], false) }

func (r Row) State(names ...string) string {
	for _, s := range r.States {
		for _, n := range names {
			if strings.EqualFold(s.StateName, n) {
				return s.Name
			}
		}
	}
	return ""
}

func ReadDataTables(path string) ([]Row, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var root map[string]any
	if err := plist.NewDecoder(f).Decode(&root); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return parseTables(root), nil
}

func parseTables(root map[string]any) []Row {
	var rows []Row
	tables := sortedKeys(root)
	for _, table := range tables {
		td, ok := root[table].(map[string]any)
		if !ok {
			continue
		}
		arr, _ := td[table].([]any)
		if arr == nil {
			for _, k := range sortedKeys(td) {
				if strings.Contains(k, "Zone Map") {
					continue
				}
				if a, ok := td[k].([]any); ok {
					arr = a
					break
				}
			}
		}
		for i, item := range arr {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			rows = append(rows, parseRow(table, i, m))
		}
	}
	return rows
}

func parseRow(table string, idx int, m map[string]any) Row {
	r := Row{
		Table:            table,
		Index:            idx,
		Entity:           str(m["Entity"]),
		Label:            str(m["Label"]),
		ControllerZone:   str(m["Controller Zone"]),
		Controller:       str(m["Controller"]),
		LogicalComponent: str(m["Logical Component"]),
		ServiceID:        str(m["ServiceID"]),
		UIType:           str(m["UI Type"]),
		Enabled:          truthy(m["Enabled"], true),
		Fields:           map[string]string{},
		Raw:              m,
	}
	if z, ok := m["Savant Zone"].(map[string]any); ok {
		for _, k := range sortedKeys(z) {
			if truthy(z[k], false) {
				r.Zones = append(r.Zones, k)
			}
		}
	}
	for _, k := range sortedKeys(m) {
		switch v := m[k].(type) {
		case string:
			r.Fields[k] = v
		case int64, uint64, float64:
			r.Fields[k] = fmt.Sprint(v)
		case map[string]any:
			if strings.HasPrefix(k, "State") {
				if name := str(v["RPMStateName"]); name != "" {
					r.States = append(r.States, StateRef{Key: k, Name: name, StateName: str(v["stateName"])})
				}
			}
		}
	}
	return r
}

func str(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case nil:
		return ""
	default:
		return fmt.Sprint(t)
	}
}

func truthy(v any, def bool) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		switch strings.ToLower(t) {
		case "yes", "true", "1":
			return true
		case "no", "false", "0":
			return false
		}
	case int64:
		return t != 0
	case uint64:
		return t != 0
	}
	return def
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

type ServiceDef struct {
	Zone             string
	ZoneType         string
	Type             string
	Component        string
	LogicalComponent string
	Variant          string
	Alias            string
	AppName          string          // the app on the TV, from <internal name_on_component>
	Custom           map[string]bool // requests a dealer added, like app shortcuts on an Apple TV
	Enabled          bool
	Requests         map[string][]string
}

func (s *ServiceDef) HasRequest(name string) bool {
	_, ok := s.Requests[name]
	return ok
}

type Services struct {
	List []*ServiceDef
}

func (s *Services) Find(zone, component, logical, typ, variant string) *ServiceDef {
	var best *ServiceDef
	for _, d := range s.List {
		if d.Zone == zone && d.Component == component && d.LogicalComponent == logical && d.Type == typ &&
			(variant == "" || d.Variant == variant) {
			if best == nil || d.Variant < best.Variant {
				best = d
			}
		}
	}
	return best
}

func (s *Services) FindAnyZone(prefer []string, component, logical, typ string) *ServiceDef {
	for _, z := range prefer {
		if d := s.Find(z, component, logical, typ, ""); d != nil {
			return d
		}
	}
	for _, d := range s.List {
		if d.Component == component && d.LogicalComponent == logical && d.Type == typ {
			return d
		}
	}
	return nil
}

func ReadServices(path string) (*Services, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var r io.Reader = f
	if strings.HasSuffix(path, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		r = gz
	}
	return parseServices(r)
}

func parseServices(r io.Reader) (*Services, error) {
	dec := xml.NewDecoder(r)
	dec.Strict = false
	out := &Services{}
	var (
		zone, zoneType string
		svc            *ServiceDef
		req            string
		depth          int
	)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return out, fmt.Errorf("serviceImplementation.xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			a := attrs(t)
			switch {
			case t.Name.Local == "zone" && svc == nil:
				zone, zoneType = a["name"], a["type"]
			case t.Name.Local == "service" && svc == nil:
				svc = &ServiceDef{
					Zone: zone, ZoneType: zoneType,
					Type: a["service_type"], Component: a["source_component_name"],
					LogicalComponent: a["source_logical_component"], Variant: a["variant_id"],
					Alias: a["service_alias"], Enabled: a["enabled"] != "false",
					Requests: map[string][]string{},
				}
			case svc != nil && depth == 0 && t.Name.Local == "internal" && a["name_on_component"] != "":
				if svc.AppName == "" {
					svc.AppName = a["name_on_component"]
				}
			case svc != nil && t.Name.Local == "request" && req == "" && depth == 0:
				if a["inter-service"] != "true" && a["name"] != "" {
					req = a["name"]
					if _, ok := svc.Requests[req]; !ok {
						svc.Requests[req] = nil
					}
				}
				depth = 1
			case svc != nil && depth > 0:
				depth++
				if depth == 2 && req != "" && t.Name.Local == "arg" && a["name"] != "" {
					svc.Requests[req] = appendUnique(svc.Requests[req], a["name"])
				}
			}
		case xml.EndElement:
			switch {
			case depth > 0:
				depth--
				if depth == 0 {
					req = ""
				}
			case t.Name.Local == "service" && svc != nil:
				out.List = append(out.List, svc)
				svc = nil
			case t.Name.Local == "zone" && svc == nil:
				zone, zoneType = "", ""
			}
		}
	}
	return out, nil
}

func attrs(t xml.StartElement) map[string]string {
	m := make(map[string]string, len(t.Attr))
	for _, a := range t.Attr {
		m[a.Name.Local] = a.Value
	}
	return m
}

func appendUnique(list []string, s string) []string {
	for _, v := range list {
		if v == s {
			return list
		}
	}
	return append(list, s)
}

type Manifest struct {
	HostUID          string
	TemperatureUnits string
	ChildAliases     map[string]string
}

func ReadManifest(path string) (Manifest, error) {
	var raw struct {
		HostUID  string `json:"HostUID"`
		Settings struct {
			TemperatureUnits    string            `json:"temperatureUnits"`
			ServiceChildAliases map[string]string `json:"serviceChildAliases"`
		} `json:"Settings"`
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return Manifest{}, fmt.Errorf("%s: %w", path, err)
	}
	return Manifest{raw.HostUID, raw.Settings.TemperatureUnits, raw.Settings.ServiceChildAliases}, nil
}
