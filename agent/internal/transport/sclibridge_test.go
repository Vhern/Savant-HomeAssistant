package transport

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseServiceLineFixture(t *testing.T) {
	f, err := os.Open("../../testdata/shr2000-servicesforzone-room1.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		svc, err := ParseServiceLine("Room 1", sc.Text(), nil)
		if err != nil {
			t.Fatalf("%q: %v", sc.Text(), err)
		}
		if !strings.HasPrefix(svc.Type, "SVC_") || !isDigits(svc.Variant) {
			t.Errorf("%q parsed as %+v", sc.Text(), svc)
		}
		n++
	}
	if n != 83 {
		t.Errorf("parsed %d lines, want 83", n)
	}
}

func TestParseServiceLineCases(t *testing.T) {
	cases := []struct {
		line  string
		known func(Service) bool
		want  Service
	}{
		{"Room 1-HD TV 2-HD_monitor-10-SVC_AV_EXTERNALMEDIASERVER", nil,
			Service{"Room 1", "HD TV 2", "HD_monitor", "10", "SVC_AV_EXTERNALMEDIASERVER"}},
		{"Room 1-Vhern's--1-SVC_GEN_GENERIC", nil,
			Service{"Room 1", "Vhern's", "", "1", "SVC_GEN_GENERIC"}},
		{"Room 1-PS5-Game_console-SVC_AV_GAME-1", nil,
			Service{"Room 1", "PS5", "Game_console", "1", "SVC_AV_GAME"}},
		{"Room 1-Lutron - Main-Lighting_controller-1-SVC_ENV_LIGHTING", nil,
			Service{"Room 1", "Lutron - Main", "Lighting_controller", "1", "SVC_ENV_LIGHTING"}},
		{"Room 1-Pool-Pump-ctl-1-SVC_GEN_GENERIC",
			func(s Service) bool { return s.Component == "Pool" },
			Service{"Room 1", "Pool", "Pump-ctl", "1", "SVC_GEN_GENERIC"}},
	}
	for _, c := range cases {
		got, err := ParseServiceLine("Room 1", c.line, c.known)
		if err != nil {
			t.Errorf("%q: %v", c.line, err)
			continue
		}
		if got != c.want {
			t.Errorf("%q:\n got %+v\nwant %+v", c.line, got, c.want)
		}
	}
	if _, err := ParseServiceLine("Kitchen", "Room 1-X-Y-1-SVC_Z", nil); err == nil {
		t.Error("expected error for wrong zone prefix")
	}
}

func TestParseBool(t *testing.T) {
	for in, want := range map[string]bool{"0.0": false, "1.0": true, "1": true, "ON": true, "off": false} {
		if got, ok := ParseBool(in); !ok || got != want {
			t.Errorf("ParseBool(%q) = %v,%v", in, got, ok)
		}
	}
	if _, ok := ParseBool(""); ok {
		t.Error("empty should be unknown")
	}
}

func TestParseScenesCSV(t *testing.T) {
	data := []byte("Name,ID,User\n\"Movie Time\",CCED55FE-A9E1-4BFC-8814-3EE814134E51,someone@example.com\n\nGood Night,ABC,\n")
	got, err := ParseScenesCSV(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "Movie Time" || got[0].User != "someone@example.com" || got[1].ID != "ABC" {
		t.Errorf("got %+v", got)
	}
}

func fakeSclibridge(t *testing.T) (path, logPath string) {
	dir := t.TempDir()
	logPath = filepath.Join(dir, "calls.log")
	script := `#!/bin/sh
echo "$*" >> "` + logPath + `"
cmd="$1"; shift
case "$cmd" in
readstate)
  for n in "$@"; do
    case "$n" in
    multi.state) printf 'line1\nline2\n' ;;
    empty.state) printf '\n' ;;
    *) printf 'v:%s\n' "$n" ;;
    esac
  done ;;
userzones) printf 'Living Room\nKitchen\n\n' ;;
servicerequest) exit 0 ;;
*) echo "unknown" >&2; exit 3 ;;
esac
`
	path = filepath.Join(dir, "sclibridge")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path, logPath
}

func calls(t *testing.T, logPath string) []string {
	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	return nonEmptyLines(string(b))
}

func TestReadStatesBatchingAndMultiline(t *testing.T) {
	bin, logPath := fakeSclibridge(t)
	s, err := NewSclibridge(SclibridgeOptions{Path: bin, BatchSize: 4})
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"a", "b", "empty.state", "c", "d", "multi.state", "e", "a"}
	got, err := s.ReadStates(context.Background(), names)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"a": "v:a", "b": "v:b", "empty.state": "", "c": "v:c", "d": "v:d", "multi.state": "line1\nline2", "e": "v:e"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	before := len(calls(t, logPath))
	if _, err := s.ReadStates(context.Background(), names); err != nil {
		t.Fatal(err)
	}
	if n := len(calls(t, logPath)) - before; n != 3 {
		t.Errorf("second read used %d invocations, want 3: %v", n, calls(t, logPath)[before:])
	}
}

func TestZonesAndServiceRequest(t *testing.T) {
	bin, logPath := fakeSclibridge(t)
	s, err := NewSclibridge(SclibridgeOptions{Path: bin})
	if err != nil {
		t.Fatal(err)
	}
	zones, err := s.ListZones(context.Background())
	if err != nil || len(zones) != 2 || zones[1] != "Kitchen" {
		t.Fatalf("zones=%v err=%v", zones, err)
	}
	err = s.ServiceRequest(context.Background(), Request{
		Service:  Service{"Kitchen", "Lighting", "Lighting_controller", "1", "SVC_ENV_LIGHTING"},
		Name:     "DimmerSet",
		Args:     map[string]string{"DimmerLevel": "50", "Address1": "12"},
		ArgOrder: []string{"Address1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	c := calls(t, logPath)
	if last := c[len(c)-1]; last != "servicerequest Kitchen Lighting Lighting_controller 1 SVC_ENV_LIGHTING DimmerSet Address1 12 DimmerLevel 50" {
		t.Errorf("servicerequest args: %q", last)
	}
	if _, err := s.StateNames(context.Background(), ""); err == nil {
		t.Error("expected error from unknown command")
	}
}

func TestParseLevel(t *testing.T) {
	for in, want := range map[string]float64{"50": 50, "50.0": 50, "94%": 94, "67F": 67, "50,100,X,-1": 50, "0": 0} {
		if got, ok := ParseLevel(in); !ok || got != want {
			t.Errorf("ParseLevel(%q) = %v,%v", in, got, ok)
		}
	}
	for _, in := range []string{"", "X", "-1", "-", "abc"} {
		if _, ok := ParseLevel(in); ok {
			t.Errorf("ParseLevel(%q) should be unknown", in)
		}
	}
}
