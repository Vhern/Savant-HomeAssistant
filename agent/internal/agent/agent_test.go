package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"savant-ha/internal/config"
	"savant-ha/internal/savantcfg"
	"savant-ha/internal/transport"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

type fakeHost struct {
	mu     sync.Mutex
	states map[string]string
	reqs   []string
	ran    []transport.Scene
	fail   bool
	delay  time.Duration
	reqErr error
}

func (f *fakeHost) set(k, v string) {
	f.mu.Lock()
	f.states[k] = v
	f.mu.Unlock()
}

func (f *fakeHost) ListZones(context.Context) ([]string, error) { return []string{"Room 1"}, nil }
func (f *fakeHost) ListServices(_ context.Context, zone string) ([]transport.Service, error) {
	return fixtureServices(zone)
}
func (f *fakeHost) StateNames(context.Context, string) ([]string, error) { return nil, nil }
func (f *fakeHost) ReadStates(_ context.Context, names []string) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return nil, io.ErrUnexpectedEOF
	}
	out := map[string]string{}
	for _, n := range names {
		out[n] = f.states[n]
	}
	return out, nil
}
func (f *fakeHost) ServiceRequest(_ context.Context, r transport.Request) error {
	f.mu.Lock()
	delay, reqErr := f.delay, f.reqErr
	f.mu.Unlock()
	time.Sleep(delay)
	if reqErr != nil {
		return reqErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	s := r.Service.Key() + " " + r.Name
	keys := append([]string{}, r.ArgOrder...)
	for _, k := range sortedKeys(r.Args) {
		if !contains(keys, k) {
			keys = append(keys, k)
		}
	}
	for _, k := range keys {
		if v, ok := r.Args[k]; ok {
			s += " " + k + "=" + v
		}
	}
	f.reqs = append(f.reqs, s)
	return nil
}
func (f *fakeHost) Scenes(context.Context) ([]transport.Scene, error) {
	return []transport.Scene{{Name: "Movie Time", ID: "CCED55FE-A9E1", User: "dealer@example.com"}}, nil
}
func (f *fakeHost) ActivateScene(_ context.Context, s transport.Scene) error {
	f.mu.Lock()
	f.ran = append(f.ran, s)
	f.mu.Unlock()
	return nil
}

func contains(l []string, s string) bool {
	for _, v := range l {
		if v == s {
			return true
		}
	}
	return false
}

func fixtureServices(zone string) ([]transport.Service, error) {
	f, err := os.Open("../../testdata/shr2000-servicesforzone-room1.txt")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []transport.Service
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		s, err := transport.ParseServiceLine(zone, sc.Text(), nil)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

const (
	lightState = "Vhern's.RacePointMedia_host.DimmerLevel_001"
	shadeState = "Vhern's.RacePointMedia_host.ShadeLevel_01_002"
	lockState  = "Door Lock Controller.Door_lock.DoorLockStatus_1"
	active     = "Room 1.ActiveService"
)

func testInventory(t *testing.T) *Inventory {
	t.Helper()
	defs, err := savantcfg.ReadServices("../../testdata/shr2000-serviceImplementation.xml.gz")
	must(t, err)
	rows, err := savantcfg.ReadDataTables("../../testdata/shr2000-dataTableInfo.plist")
	must(t, err)
	man, err := savantcfg.ReadManifest("../../testdata/shr2000-uimanifest.json")
	must(t, err)
	svcs, err := fixtureServices("Room 1")
	must(t, err)
	rows = append(rows,
		savantcfg.Row{
			Table: "Lighting", Entity: "Pendants", Zones: []string{"Room 1"}, Controller: "Vhern's",
			LogicalComponent: "RacePointMedia_host", ServiceID: "SVC_ENV_LIGHTING", UIType: "Dimmer", Enabled: true,
			States: []savantcfg.StateRef{{Key: "State1", Name: lightState, StateName: "DimmerLevel"}},
			Fields: map[string]string{"Address1": "001", "Address2": ""},
		},
		savantcfg.Row{
			Table: "Shade", Entity: "Sheers", Zones: []string{"Room 1"}, Controller: "Vhern's",
			LogicalComponent: "RacePointMedia_host", ServiceID: "SVC_ENV_SHADE", Enabled: true,
			States: []savantcfg.StateRef{{Key: "State1", Name: shadeState, StateName: "ShadeLevel"}},
			Fields: map[string]string{"Address1": "01", "Address2": "002"},
		},
	)
	return &Inventory{
		Host:     Host{UID: man.HostUID, Name: "Vhern's", Component: "Vhern's"},
		Zones:    []string{"Room 1"},
		Services: map[string][]transport.Service{"Room 1": svcs},
		Defs:     defs,
		Rows:     rows,
		Manifest: man,
		Scenes: []transport.Scene{
			{Name: "Movie Time", ID: "CCED55FE-A9E1", User: "dealer@example.com"},
			{Name: "Chill", ID: "6B32980B", User: "admin", Rooms: []string{"Great Room"}},
		},
	}
}

func byID(ents []Entity) map[string]Entity {
	m := map[string]Entity{}
	for _, e := range ents {
		m[e.Info().ID] = e
	}
	return m
}

func TestBuild(t *testing.T) {
	ents, skipped := Build(testInventory(t), quiet)
	var ids []string
	for _, e := range ents {
		ids = append(ids, e.Info().ID)
	}
	want := "cover.vhern_s_01_002 light.vhern_s_001 lock.door_lock_controller_1 media_player.room_1 " +
		"media_player.room_1_hd_tv media_player.room_1_hd_tv_2 media_player.room_1_ps5 remote.room_1_hd_tv remote.room_1_hd_tv_2 scene.cced55fe_a9e1 " +
		"sensor.host_cpu sensor.host_disk_free sensor.host_memory sensor.host_software sensor.host_uptime"
	if got := strings.Join(ids, " "); got != want {
		t.Errorf("ids:\n got %s\nwant %s\nskipped %v", got, want, skipped)
	}
	found := false
	for _, s := range skipped {
		found = found || (s.What == "scene Chill" && strings.Contains(s.Reason, "Great Room"))
	}
	if !found {
		t.Errorf("a scene for a room that no longer exists should be skipped: %v", skipped)
	}
	room := byID(ents)["media_player.room_1"].(*MediaRoom)
	if src := room.Attributes["sources"].([]string); strings.Join(src, ",") != "HD TV,HD TV 2,PS5" {
		t.Errorf("sources = %v", src)
	}
	if room.Attributes["device_class"] != "tv" || room.Attributes["volume_step"] != true || room.Attributes["volume_set"] != false {
		t.Errorf("room attributes = %v", room.Attributes)
	}
	if s := room.source("HD TV 2"); s.Service.Type != "SVC_AV_TV" || s.Service.Variant != "1" || len(s.Services) != 13 {
		t.Errorf("HD TV 2 primary = %+v (%d services)", s.Service, len(s.Services))
	}
}

func TestMediaRoom(t *testing.T) {
	host := &fakeHost{states: map[string]string{}}
	room := byID(mustBuild(t))["media_player.room_1"].(*MediaRoom)
	ctx := context.Background()

	if st := room.State(Values{active: ""}); st["on"] != false || st["source"] != nil {
		t.Errorf("off state = %v", st)
	}
	if st := room.State(Values{active: "Room 1-HD TV-HD_monitor-5-SVC_AV_EXTERNALMEDIASERVER"}); st["source"] != "HD TV" {
		t.Errorf("app input should map to its TV: %v", st)
	}
	if st := room.State(Values{active: "Room 1-PS5-Game_console-1-SVC_AV_GAME"}); st["on"] != true || st["source"] != "PS5" {
		t.Errorf("ps5 state = %v", st)
	}
	must(t, room.Do(ctx, host, "volume_up", nil))
	must(t, room.Do(ctx, host, "mute", Params{"muted": true}))
	must(t, room.Do(ctx, host, "turn_off", nil))
	must(t, room.Do(ctx, host, "select_source", Params{"source": "HD TV 2"}))
	must(t, room.Do(ctx, host, "turn_on", nil))
	want := []string{
		"Room 1|PS5|Game_console|1|SVC_AV_GAME VolumeUp",
		"Room 1|PS5|Game_console|1|SVC_AV_GAME MuteOn",
		"Room 1|PS5|Game_console|1|SVC_AV_GAME PowerOff",
		"Room 1|HD TV 2|HD_monitor|1|SVC_AV_TV PowerOn",
		"Room 1|PS5|Game_console|1|SVC_AV_GAME PowerOn",
	}
	if got := strings.Join(host.reqs, "\n"); got != strings.Join(want, "\n") {
		t.Errorf("requests:\n%s\nwant:\n%s", got, strings.Join(want, "\n"))
	}
	must(t, room.Do(ctx, host, "volume_up", nil)) // goes to PS5 before any poll
	if last := host.reqs[len(host.reqs)-1]; last != "Room 1|PS5|Game_console|1|SVC_AV_GAME VolumeUp" {
		t.Errorf("volume after turn_on went to %q", last)
	}
	room.State(Values{active: ""})
	if err := room.Do(ctx, host, "volume_up", nil); err == nil {
		t.Error("volume on an off room should fail")
	}
}

func TestEnvCommands(t *testing.T) {
	host := &fakeHost{states: map[string]string{}}
	ents := byID(mustBuild(t))
	ctx := context.Background()
	light := ents["light.vhern_s_001"]
	if st := light.State(Values{lightState: "62.0"}); st["on"] != true || st["brightness"] != 62 {
		t.Errorf("light state = %v", st)
	}
	must(t, light.Do(ctx, host, "turn_on", Params{"brightness": 40.0}))
	must(t, light.Do(ctx, host, "turn_off", nil))
	must(t, light.Do(ctx, host, "turn_on", nil))
	must(t, ents["lock.door_lock_controller_1"].Do(ctx, host, "lock", nil))
	must(t, ents["cover.vhern_s_01_002"].Do(ctx, host, "set_position", Params{"position": 30.0}))
	must(t, ents["cover.vhern_s_01_002"].Do(ctx, host, "stop", nil))
	must(t, ents["scene.cced55fe_a9e1"].Do(ctx, host, "activate", nil))
	want := []string{
		"Room 1|Vhern's|RacePointMedia_host|1|SVC_ENV_LIGHTING DimmerSet Address1=001 DimmerLevel=40",
		"Room 1|Vhern's|RacePointMedia_host|1|SVC_ENV_LIGHTING DimmerSet Address1=001 DimmerLevel=0",
		"Room 1|Vhern's|RacePointMedia_host|1|SVC_ENV_LIGHTING DimmerSet Address1=001 DimmerLevel=62",
		"Room 1|Door Lock Controller|Door_lock|1|SVC_ENV_DOORLOCK LockDoorLock DoorLockAddress=1",
		"Room 1|Vhern's|RacePointMedia_host|1|SVC_ENV_SHADE ShadeSet Address1=01 Address2=002 ShadeLevel=30",
		"Room 1|Vhern's|RacePointMedia_host|1|SVC_ENV_SHADE ShadeStop Address1=01 Address2=002",
	}
	if got := strings.Join(host.reqs, "\n"); got != strings.Join(want, "\n") {
		t.Errorf("requests:\n%s\nwant:\n%s", got, strings.Join(want, "\n"))
	}
	if len(host.ran) != 1 {
		t.Errorf("scenes run: %v", host.ran)
	}
	if st := ents["lock.door_lock_controller_1"].State(Values{lockState: "Unlocked"}); st["locked"] != false {
		t.Errorf("lock state = %v", st)
	}
}

func mustBuild(t *testing.T) []Entity {
	ents, _ := Build(testInventory(t), quiet)
	return ents
}

func TestEngineStreamsChanges(t *testing.T) {
	host := &fakeHost{states: map[string]string{active: "", lockState: "Unlocked", "Vhern's.SysStatus.CPUPercent": "5.4"}}
	e := NewEngine(host, config.Defaults(), "test", quiet)
	ents := mustBuild(t)
	e.entities, e.byID = ents, byID(ents)
	e.inv = testInventory(t)
	e.readyOnce.Do(func() { close(e.ready) })

	sub, snap := e.Subscribe()
	if len(snap.Entities) != len(ents) || !snap.Available {
		t.Fatalf("snapshot = %+v", snap)
	}
	ctx := context.Background()
	e.poll(ctx)
	got := drain(sub)
	if string(got["sensor.host_cpu"]) != `{"value":5.4}` || string(got["lock.door_lock_controller_1"]) != `{"jammed":false,"locked":false}` ||
		string(got["media_player.room_1"]) != `{"on":false,"source":null}` || string(got["light.vhern_s_001"]) != "null" {
		t.Errorf("first poll events = %v", stringify(got))
	}

	e.poll(ctx)
	if got := drain(sub); len(got) != 0 {
		t.Errorf("unchanged poll sent %v", stringify(got))
	}
	host.set(active, "Room 1-PS5-Game_console-1-SVC_AV_GAME")
	e.poll(ctx)
	if got := drain(sub); len(got) != 2 || string(got["media_player.room_1"]) != `{"on":true,"source":"PS5"}` ||
		string(got["media_player.room_1_ps5"]) != `{"on":true}` {
		t.Errorf("change events = %v", stringify(got))
	}

	host.mu.Lock()
	host.fail = true
	host.mu.Unlock()
	for i := 0; i < 5; i++ {
		e.poll(ctx)
	}
	select {
	case ev := <-sub.C:
		if ev.Type != "availability" || *ev.Available {
			t.Errorf("expected unavailable event, got %+v", ev)
		}
	case <-time.After(time.Second):
		t.Error("no availability event after repeated failures")
	}

	must(t, e.Command(ctx, "media_player.room_1", "turn_off", nil))
	if err := e.Command(ctx, "light.nope", "turn_on", nil); err == nil {
		t.Error("unknown entity should fail")
	}
	e.Unsubscribe(sub)
}

func drain(s *Sub) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for {
		select {
		case ev := <-s.C:
			if ev.Type == "state" {
				out[ev.EntityID] = ev.State
			}
		default:
			return out
		}
	}
}

func stringify(m map[string]json.RawMessage) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		out[k] = string(v)
	}
	return out
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestCommandsAnswerBeforeSclibridgeFinishes(t *testing.T) {
	host := &fakeHost{states: map[string]string{}, delay: 2 * time.Second}
	e := NewEngine(host, config.Defaults(), "test", quiet)
	ents := mustBuild(t)
	e.entities, e.byID = ents, byID(ents)
	ctx := context.Background()

	start := time.Now()
	must(t, e.Command(ctx, "lock.door_lock_controller_1", "unlock", nil))
	must(t, e.Command(ctx, "lock.door_lock_controller_1", "lock", nil))
	if d := time.Since(start); d > time.Second {
		t.Errorf("commands took %s; they should not wait for sclibridge", d)
	}
	time.Sleep(4500 * time.Millisecond)
	host.mu.Lock()
	got := strings.Join(host.reqs, "\n")
	host.mu.Unlock()
	want := "Room 1|Door Lock Controller|Door_lock|1|SVC_ENV_DOORLOCK UnlockDoorLock DoorLockAddress=1\n" +
		"Room 1|Door Lock Controller|Door_lock|1|SVC_ENV_DOORLOCK LockDoorLock DoorLockAddress=1"
	if got != want {
		t.Errorf("commands for one device must run in order:\n%s", got)
	}

	host.mu.Lock()
	host.delay, host.reqErr = 0, io.ErrClosedPipe
	host.mu.Unlock()
	if err := e.Command(ctx, "lock.door_lock_controller_1", "lock", nil); err == nil {
		t.Error("a command that fails quickly should report the error")
	}
}

func TestSourcePlayers(t *testing.T) {
	host := &fakeHost{states: map[string]string{}}
	ents := byID(mustBuild(t))
	room := ents["media_player.room_1"].(*MediaRoom)
	ps5 := ents["media_player.room_1_ps5"].(*SourcePlayer)
	tv := ents["media_player.room_1_hd_tv"].(*SourcePlayer)
	ctx := context.Background()

	if ps5.Attributes["device_class"] != "" || tv.Attributes["device_class"] != "tv" || tv.Attributes["play"] != true {
		t.Errorf("attributes: ps5=%v tv=%v", ps5.Attributes, tv.Attributes)
	}
	vals := Values{active: "Room 1-HD TV-HD_monitor-7-SVC_AV_EXTERNALMEDIASERVER"}
	room.State(vals)
	if ps5.State(vals)["on"] != false || tv.State(vals)["on"] != true {
		t.Errorf("only the TV should be on: ps5=%v tv=%v", ps5.State(vals), tv.State(vals))
	}
	if err := ps5.Do(ctx, host, "volume_up", nil); err == nil {
		t.Error("volume on a source that isn't playing should fail")
	}
	must(t, ps5.Do(ctx, host, "turn_on", nil))
	must(t, ps5.Do(ctx, host, "volume_up", nil))
	must(t, ps5.Do(ctx, host, "turn_off", nil))
	want := []string{
		"Room 1|PS5|Game_console|1|SVC_AV_GAME PowerOn",
		"Room 1|PS5|Game_console|1|SVC_AV_GAME VolumeUp",
		"Room 1|PS5|Game_console|1|SVC_AV_GAME PowerOff",
	}
	if got := strings.Join(host.reqs, "\n"); got != strings.Join(want, "\n") {
		t.Errorf("requests:\n%s", got)
	}
}

func TestTVApps(t *testing.T) {
	host := &fakeHost{states: map[string]string{}}
	ents := byID(mustBuild(t))
	room := ents["media_player.room_1"].(*MediaRoom)
	tv := ents["media_player.room_1_hd_tv"].(*SourcePlayer)
	ps5 := ents["media_player.room_1_ps5"].(*SourcePlayer)

	want := "LG Channels,Amazon Video,Apple TV,Disney+,External Media (USB),Google Play,Hulu,Netflix,Sling TV,Vudu,Youtube,Youtube Kids,Youtube TV"
	if got := strings.Join(tv.Attributes["sources"].([]string), ","); got != want {
		t.Errorf("TV apps:\n got %s\nwant %s", got, want)
	}
	if _, ok := ps5.Attributes["sources"]; ok {
		t.Error("the PS5 has no apps and should have no source list")
	}

	netflix := Values{active: "Room 1-HD TV-HD_monitor-7-SVC_AV_EXTERNALMEDIASERVER"}
	if st := tv.State(netflix); st["on"] != true || st["source"] != "Netflix" {
		t.Errorf("tv state = %v", st)
	}
	if st := room.State(netflix); st["source"] != "HD TV" || st["app"] != "Netflix" {
		t.Errorf("room state = %v", st)
	}
	if st := room.State(Values{active: "Room 1-PS5-Game_console-1-SVC_AV_GAME"}); st["app"] != nil {
		t.Errorf("PS5 has no app: %v", st)
	}

	ctx := context.Background()
	must(t, tv.Do(ctx, host, "select_source", Params{"source": "Hulu"}))
	must(t, tv.Do(ctx, host, "turn_on", nil))
	if err := tv.Do(ctx, host, "select_source", Params{"source": "Spotify"}); err == nil {
		t.Error("an app the TV doesn't have should fail")
	}
	got := strings.Join(host.reqs, "\n")
	if got != "Room 1|HD TV|HD_monitor|6|SVC_AV_EXTERNALMEDIASERVER PowerOn\nRoom 1|HD TV|HD_monitor|1|SVC_AV_TV PowerOn" {
		t.Errorf("requests:\n%s", got)
	}
}
