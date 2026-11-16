package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.json")
	os.WriteFile(p, []byte(`{"listen":"127.0.0.1:9000","token":"abc","poll_interval":"500ms"}`), 0o600)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != "127.0.0.1:9000" || c.Token != "abc" || c.PollInterval.D() != 500*time.Millisecond || c.Concurrency != 2 {
		t.Errorf("config = %+v", c)
	}
	os.WriteFile(p, []byte(`{"lisen":"x"}`), 0o600)
	if _, err := Load(p); err == nil {
		t.Error("unknown keys should be rejected")
	}
}

func TestSlug(t *testing.T) {
	if got := Slug("Vhern's HD TV 2"); got != "vhern_s_hd_tv_2" {
		t.Errorf("Slug = %q", got)
	}
}
