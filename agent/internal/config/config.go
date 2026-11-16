// Package config loads the agent config that the integration writes at install time.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

type Config struct {
	Listen             string            `json:"listen"`
	Token              string            `json:"token"`
	Sclibridge         string            `json:"sclibridge"`
	ConfigDir          string            `json:"config_dir"`
	PollInterval       Duration          `json:"poll_interval"`
	RediscoverInterval Duration          `json:"rediscover_interval"`
	Timeout            Duration          `json:"timeout"`
	Concurrency        int               `json:"concurrency"`
	Env                map[string]string `json:"env"`
	LogLevel           string            `json:"log_level"`
}

type Duration time.Duration

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(time.Duration(d).String()) }
func (d Duration) D() time.Duration             { return time.Duration(d) }

func Defaults() Config {
	return Config{
		Listen:             "0.0.0.0:8765",
		PollInterval:       Duration(time.Second),
		RediscoverInterval: Duration(10 * time.Minute),
		Timeout:            Duration(10 * time.Second),
		Concurrency:        2,
		LogLevel:           "info",
	}
}

func Load(path string) (Config, error) {
	c := Defaults()
	if path == "" {
		return c, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return c, fmt.Errorf("%s: %w", path, err)
	}
	if c.PollInterval.D() < 200*time.Millisecond {
		c.PollInterval = Duration(200 * time.Millisecond)
	}
	return c, nil
}

var nonID = regexp.MustCompile(`[^a-z0-9_]+`)

func Slug(s string) string {
	return strings.Trim(nonID.ReplaceAllString(strings.ToLower(s), "_"), "_")
}
