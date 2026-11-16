package savantcfg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Circuit struct {
	Name           string
	Room           string
	Group          string
	Classification string // Consumption or Production
}

func EnergyConfigPath(configDir string) string {
	return filepath.Join(configDir, "energyConfig.json")
}

// ReadEnergy reads only the circuit list from energyConfig.json; the rest of
// the file (users, connections) is never decoded.
func ReadEnergy(path string) ([]Circuit, error) {
	var raw struct {
		EnergyConfig struct {
			Circuits []struct {
				Name           string `json:"name"`
				RoomID         string `json:"roomID"`
				GroupName      string `json:"groupName"`
				Classification string `json:"classification"`
			} `json:"circuits"`
		} `json:"energyConfig"`
		Rooms []struct {
			RoomID string `json:"roomID"`
			Name   string `json:"name"`
		} `json:"rooms"`
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	rooms := map[string]string{}
	for _, r := range raw.Rooms {
		rooms[r.RoomID] = r.Name
	}
	var out []Circuit
	for _, c := range raw.EnergyConfig.Circuits {
		if c.Name == "" {
			continue
		}
		out = append(out, Circuit{Name: c.Name, Room: rooms[c.RoomID], Group: c.GroupName, Classification: c.Classification})
	}
	return out, nil
}
