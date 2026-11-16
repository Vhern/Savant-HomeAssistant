package savantcfg

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// AppDB is serviceImplementation.sqlite: the model the Savant app itself uses.
// Unlike serviceImplementation.xml it stays readable on Savant 12 Mac hosts.
type AppDB struct {
	Path     string
	Lights   []AppLight
	Shades   []AppShade
	Locks    []AppLock
	Cameras  []AppCamera
	Services *Services
}

// AppCamera URLs can carry the camera's login, as Blueprint stores them.
type AppCamera struct {
	ID                          int
	Zone, Name                  string
	Component, Logical          string
	Snapshot, Stream, Substream string
	Username, Password          string
}

type AppLight struct {
	ID                   int
	Zone, Name           string
	Type, SubType, Tech  string
	Addresses            []string
	Press, Release       string
	TogglePress          string
	ToggleRelease        string
	Dimmer               string
	State                string
	MinKelvin, MaxKelvin int
}

type AppShade struct {
	ID             int
	Zone, Name     string
	Type           string
	Addresses      []string
	Press, Release string
	Dimmer         string
	State          string
}

type AppLock struct {
	ID                    int
	Zone, Name            string
	Addresses             []string
	Lock, Unlock          string
	LockState, ErrorState string
}

func AppDBPath(configDir string) string {
	return filepath.Join(configDir, "serviceImplementation.sqlite")
}

// ReadAppDB loads entities and service definitions. Tables a Savant version
// doesn't have are skipped, so older hosts load whatever they do have.
func ReadAppDB(ctx context.Context, path string) (*AppDB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	db := &AppDB{Path: path}
	zones := map[string]string{}
	rows, err := sqlRows(ctx, path, "Zones", []string{"id", "name"}, "")
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		zones[r[0]] = r[1]
	}

	kelvin := map[string][2]int{}
	if rows, err := sqlRows(ctx, path, "LightProperties", []string{"lightingEntityID", "minKelvinTemp", "maxKelvinTemp"}, ""); err == nil {
		for _, r := range rows {
			lo, _ := strconv.Atoi(r[1])
			hi, _ := strconv.Atoi(r[2])
			kelvin[r[0]] = [2]int{lo, hi}
		}
	}

	if rows, err := sqlRows(ctx, path, "LightEntities", []string{"id", "zoneID", "name", "addresses", "entityType",
		"entitySubType", "technology", "pressCommand", "releaseCommand", "togglePressCommand",
		"toggleReleaseCommand", "dimmerCommand", "stateName"}, "order by orderValue, id"); err == nil {
		for _, r := range rows {
			id, _ := strconv.Atoi(r[0])
			k := kelvin[r[0]]
			db.Lights = append(db.Lights, AppLight{
				ID: id, Zone: zones[r[1]], Name: r[2], Addresses: addresses(r[3]),
				Type: r[4], SubType: r[5], Tech: r[6], Press: r[7], Release: r[8],
				TogglePress: r[9], ToggleRelease: r[10], Dimmer: r[11], State: r[12],
				MinKelvin: k[0], MaxKelvin: k[1],
			})
		}
	}

	if rows, err := sqlRows(ctx, path, "ShadeEntities", []string{"id", "zoneID", "name", "addresses", "entityType",
		"pressCommand", "releaseCommand", "dimmerCommand", "stateName"}, "order by orderValue, id"); err == nil {
		for _, r := range rows {
			id, _ := strconv.Atoi(r[0])
			db.Shades = append(db.Shades, AppShade{
				ID: id, Zone: zones[r[1]], Name: r[2], Addresses: addresses(r[3]), Type: r[4],
				Press: r[5], Release: r[6], Dimmer: r[7], State: r[8],
			})
		}
	}

	if rows, err := sqlRows(ctx, path, "DoorLockEntities", []string{"id", "zoneID", "name", "addresses",
		"lockCommand", "unlockCommand", "lockState", "errorState"}, "order by id"); err == nil {
		for _, r := range rows {
			id, _ := strconv.Atoi(r[0])
			db.Locks = append(db.Locks, AppLock{
				ID: id, Zone: zones[r[1]], Name: r[2], Addresses: addresses(r[3]),
				Lock: r[4], Unlock: r[5], LockState: r[6], ErrorState: r[7],
			})
		}
	}

	if rows, err := sqlRows(ctx, path, "CameraEntities c left join Zones z on z.id = c.zoneID", []string{"c.id", "z.name",
		"c.name", "c.componentName", "c.logicalComponent", "c.fullscreenURL", "c.previewURL", "c.highSourceURL",
		"c.lowSourceURL", "c.username", "c.password"}, "order by c.id"); err == nil {
		for _, r := range rows {
			id, _ := strconv.Atoi(r[0])
			db.Cameras = append(db.Cameras, AppCamera{
				ID: id, Zone: r[1], Name: r[2], Component: r[3], Logical: r[4],
				Snapshot: or(r[5], r[6]), Stream: or(r[7], r[8]), Substream: r[8], Username: r[9], Password: r[10],
			})
		}
	}

	db.Services, err = readDBServices(ctx, path)
	return db, err
}

func readDBServices(ctx context.Context, path string) (*Services, error) {
	out := &Services{}
	rows, err := sqlRows(ctx, path, "ServiceImplementationZonedService",
		[]string{"id", "zone", "component", "logicalComponent", "serviceType", "serviceVariantID", "alias"}, "order by id")
	if err != nil {
		return out, err
	}
	byID := map[string]*ServiceDef{}
	for _, r := range rows {
		d := &ServiceDef{Zone: r[1], Component: r[2], LogicalComponent: r[3], Type: r[4], Variant: r[5],
			Alias: r[6], Enabled: true, Requests: map[string][]string{}, Custom: map[string]bool{}}
		byID[r[0]] = d
		out.List = append(out.List, d)
	}
	reqs, err := sqlRows(ctx, path, "ServiceImplementationRequestMap m join ServiceImplementationRequests r on r.id = m.ServiceImplementationRequests_id",
		[]string{"m.ServiceImplementationZonedService_id", "r.request", "r.arguments", "r.isCustom"}, "where r.isInterService = 0")
	if err != nil {
		return out, err
	}
	for _, r := range reqs {
		d := byID[r[0]]
		if d == nil || r[1] == "" {
			continue
		}
		var args []string
		for _, a := range strings.Split(r[2], ",") {
			if a = strings.TrimSpace(a); a != "" {
				args = append(args, a)
			}
		}
		d.Requests[r[1]] = args
		if r[3] == "1" {
			d.Custom[r[1]] = true
		}
	}
	// A TV's app inputs name the app on the TV, like <internal name_on_component> in the XML.
	if apps, err := sqlRows(ctx, path, "ServiceImplementationServiceResources",
		[]string{"zone", "component", "logicalComponent", "serviceType", "serviceVariantID", "nameOnComponent"},
		"where avioType = 'Internal' and resourceType like 'AV_%_SOURCE' and nameOnComponent != ''"); err == nil {
		for _, r := range apps {
			if d := out.Find(r[0], r[1], r[2], r[3], r[4]); d != nil && d.AppName == "" {
				d.AppName = r[5]
			}
		}
	}
	return out, nil
}

// sqlRows selects columns from a table expression. Every value comes back
// hex-encoded so names containing "|" or newlines can't break the parsing.
func sqlRows(ctx context.Context, path, from string, cols []string, tail string) ([][]string, error) {
	bin, err := exec.LookPath("sqlite3")
	if err != nil {
		return nil, fmt.Errorf("sqlite3 not found: %w", err)
	}
	sel := make([]string, len(cols))
	for i, c := range cols {
		sel[i] = fmt.Sprintf("hex(ifnull(cast(%s as text), ''))", c)
	}
	query := fmt.Sprintf("select %s from %s %s;", strings.Join(sel, ", "), from, tail)
	// -init /dev/null skips ~/.sqliterc, which on the SHR-2000 switches to column output with headers.
	cmd := exec.CommandContext(ctx, bin, "-init", "/dev/null", "-readonly", "-batch", "-list", "-noheader",
		"-separator", "|", path, query)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s: %w: %s", from, err, strings.TrimSpace(stderr.String()))
	}
	var rows [][]string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line == "" {
			continue
		}
		f := strings.Split(line, "|")
		if len(f) != len(cols) {
			return nil, fmt.Errorf("%s: unexpected sqlite3 output %.80q", from, line)
		}
		for i := range f {
			f[i] = unhex(f[i])
		}
		rows = append(rows, f)
	}
	return rows, nil
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// addresses turns "002,1025,/button/37,(null),(null),(null)" into its parts.
func addresses(s string) []string {
	var out []string
	for _, a := range strings.Split(s, ",") {
		a = strings.TrimSpace(a)
		if a == "" || a == "(null)" {
			continue
		}
		out = append(out, a)
	}
	return out
}
