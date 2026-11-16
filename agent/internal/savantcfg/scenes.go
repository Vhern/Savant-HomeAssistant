package savantcfg

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Scenes made in the Savant app live in the dashboard service's database, not
// where `sclibridge getSceneNames` looks.
func SceneDB(configDir string) string {
	return filepath.Join(filepath.Dir(configDir), "statusfiles", "dis", "dashboard.sqlite")
}

type AppScene struct {
	ID     string
	Name   string
	User   string
	Global bool
	Rooms  []string
}

// Values are hex-encoded so names with "|" or newlines can't break parsing.
const sceneQuery = `select hex(s.identifier), hex(ifnull(nullif(s.alias, ''), s.name)), ifnull(s.isGlobal, 0),
  hex(ifnull(m.user, '')), hex(ifnull(d.definition, ''))
from Scenes s
left join SceneUserMap m on m.sceneIdentifier = s.identifier
left join SceneDefinitions d on d.sceneIdentifier = s.identifier
order by s.name;`

func ReadAppScenes(ctx context.Context, dbPath string) ([]AppScene, error) {
	if _, err := os.Stat(dbPath); err != nil {
		return nil, err
	}
	bin, err := exec.LookPath("sqlite3")
	if err != nil {
		return nil, fmt.Errorf("sqlite3 not found: %w", err)
	}
	// -init /dev/null skips ~/.sqliterc, which on the SHR-2000 switches to column output with headers.
	out, err := exec.CommandContext(ctx, bin, "-init", "/dev/null", "-readonly", "-batch", "-list", "-noheader",
		"-separator", "|", dbPath, sceneQuery).Output()
	if err != nil {
		return nil, fmt.Errorf("read scenes: %w", err)
	}
	return parseAppScenes(string(out))
}

func parseAppScenes(out string) ([]AppScene, error) {
	byID := map[string]*AppScene{}
	var order []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Split(strings.TrimSpace(line), "|")
		if len(f) != 5 {
			continue
		}
		id, name, user, def := unhex(f[0]), unhex(f[1]), unhex(f[3]), unhex(f[4])
		if id == "" || name == "" {
			continue
		}
		sc, ok := byID[id]
		if !ok {
			sc = &AppScene{ID: id, Name: name, Global: f[2] == "1", Rooms: sceneRooms(def)}
			byID[id] = sc
			order = append(order, id)
		}
		if sc.User == "" || user == "admin" {
			sc.User = user
		}
	}
	if len(order) == 0 && strings.TrimSpace(out) != "" {
		return nil, fmt.Errorf("unexpected sqlite3 output: %.80q", out)
	}
	res := make([]AppScene, 0, len(order))
	for _, id := range order {
		res = append(res, *byID[id])
	}
	return res, nil
}

func unhex(s string) string {
	b, err := hex.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return ""
	}
	return string(b)
}

// sceneRooms lists the rooms a scene definition touches: "rooms" lists and
// maps, "lightingOff" and the keys of "volume".
func sceneRooms(def string) []string {
	var v any
	if json.Unmarshal([]byte(def), &v) != nil {
		return nil
	}
	set := map[string]bool{}
	var walk func(any)
	walk = func(n any) {
		switch t := n.(type) {
		case map[string]any:
			for k, child := range t {
				switch k {
				case "rooms", "lightingOff", "volume":
					addRooms(set, child)
				}
				walk(child)
			}
		case []any:
			for _, child := range t {
				walk(child)
			}
		}
	}
	walk(v)
	rooms := make([]string, 0, len(set))
	for r := range set {
		rooms = append(rooms, r)
	}
	sort.Strings(rooms)
	return rooms
}

func addRooms(set map[string]bool, n any) {
	switch t := n.(type) {
	case []any:
		for _, r := range t {
			if s, ok := r.(string); ok && s != "" {
				set[s] = true
			}
		}
	case map[string]any:
		for k := range t {
			set[k] = true
		}
	}
}
