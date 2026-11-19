package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"savant-ha/internal/savantcfg"
	"savant-ha/internal/transport"
)

func appInventory(t *testing.T) *Inventory {
	t.Helper()
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not installed")
	}
	sql, err := os.ReadFile("../../testdata/appdb.sql")
	must(t, err)
	path := filepath.Join(t.TempDir(), "serviceImplementation.sqlite")
	cmd := exec.Command("sqlite3", path)
	cmd.Stdin = strings.NewReader(string(sql))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create db: %v %s", err, out)
	}
	app, err := savantcfg.ReadAppDB(context.Background(), path)
	must(t, err)
	atv := transport.Service{Zone: "Den", Component: "Den Apple TV", LogicalComponent: "Media_server", Variant: "1", Type: "SVC_AV_APPLEREMOTEMEDIASERVER"}
	// Like a Savant 12 Mac: no readable XML or data tables, so everything comes from the database.
	return &Inventory{
		Host:     Host{UID: "test", Name: "Test Host", Component: "Test Host"},
		Zones:    []string{"Den"},
		Services: map[string][]transport.Service{"Den": {atv}},
		Defs:     app.Services,
		App:      app,
	}
}

func TestAppDBEntities(t *testing.T) {
	ents, _ := Build(appInventory(t), quiet)
	var ids []string
	for _, e := range ents {
		if m := e.Info(); m.Zone != "" || m.Platform == "camera" {
			ids = append(ids, m.ID)
		}
	}
	want := "button.ev_charger_den_driveway_charger_start button.ev_charger_den_driveway_charger_stop button.test_host_002_1025__button_37 button.test_host_002_1026__button_36 button.test_host_004_6 camera.porch_camera_security_camera1 " +
		"climate.den_tstat_1 cover.garage_controller_2 cover.test_host_002_1__zone_13 light.test_host_001_4 light.test_host_002_17__zone_57 light.test_host_002_2__zone_43 " +
		"media_player.den media_player.den_apple_tv remote.den_apple_tv sensor.ev_charger_den_driveway_charger_power " +
		"sensor.ev_charger_den_driveway_charger_status sensor.water_heater_den_water_heater_remaining switch.pool_aux_pool_controller_2 switch.test_host_cove_scene " +
		"water_heater.water_heater_den_water_heater"
	if got := strings.Join(ids, " "); got != want {
		t.Fatalf("ids:\n got %s\nwant %s", got, want)
	}
	e := byID(ents)
	cove := e["light.test_host_001_4"].(*Light)
	if cove.Device == nil || cove.Device.ID != "lighting_den" || cove.Device.Name != "Lighting" {
		t.Errorf("cove device = %+v", cove.Device)
	}
	if !reflect.DeepEqual(cove.Attributes["color_modes"], []string{"rgbw", "color_temp"}) || cove.Attributes["max_kelvin"] != 6000 {
		t.Errorf("cove attributes = %v", cove.Attributes)
	}
	if lamp := e["light.test_host_002_17__zone_57"].(*Light); !reflect.DeepEqual(lamp.Attributes["color_modes"], []string{"rgbw"}) {
		t.Errorf("lamp attributes = %v", lamp.Attributes)
	}
	if p := e["light.test_host_002_2__zone_43"].(*Light); p.ColorState != "" || p.Attributes["color_modes"] != nil || !p.Dimmable {
		t.Errorf("pendants = %+v", p)
	}
	if sh := e["cover.test_host_002_1__zone_13"]; sh.Info().Device.Name != "Shades" || sh.Info().Attributes["stop"] != true {
		t.Errorf("shade = %+v", sh.Info())
	}
	for id, disabled := range map[string]bool{
		"button.test_host_002_1025__button_37": false, // Away
		"button.test_host_002_1026__button_36": true,  // "1"
		"button.test_host_004_6":               true,  // Away again on a second keypad
	} {
		if got := e[id].Info().Attributes["disabled"] == true; got != disabled {
			t.Errorf("%s disabled = %v", id, got)
		}
	}
	atv := e["media_player.den_apple_tv"].Info()
	if !reflect.DeepEqual(atv.Attributes["sources"], []string{"Netflix", "Youtube"}) || atv.Attributes["play"] != true ||
		atv.Attributes["volume_set"] != true || atv.Attributes["next"] != false {
		t.Errorf("apple tv attributes = %v", atv.Attributes)
	}
	if atv.Device.ID != "source_den_apple_tv" || e["remote.den_apple_tv"].Info().Device.ID != atv.Device.ID {
		t.Errorf("apple tv and its remote should share a device")
	}
	cmds := e["remote.den_apple_tv"].Info().Attributes["commands"].([]string)
	if strings.Join(cmds, ",") != "Home,Menu,MuteOff,MuteOn,Netflix,OSDCursorDown,OSDCursorUp,OSDSelect,Pause,Play,VolumeDown,VolumeUp,Youtube" {
		t.Errorf("remote commands = %v", cmds)
	}
}

func TestAppDBStates(t *testing.T) {
	e := byID(must2(Build(appInventory(t), quiet)))
	v := Values{
		"Test Host.Lighting.CurrentColor_4_001":        "000,000,000,000,000,100|3000,000,100|Tunable White",
		"Test Host.Lighting.CurrentDimmerLevel_4_001":  "40",
		"Test Host.Lighting.CurrentColor_17_002":       "000,000,255,000,100,100|0000,100,100|Infinite Color",
		"Test Host.Lighting.CurrentDimmerLevel_17_002": "100",
		"Test Host.Lighting.IsSceneActive_Cove Scene":  "1",
		"Test Host.Lighting.CurrentLEDState_1025_002":  "0",
		"Test Host.Lighting.GroupShadeLevel_1_002":     "35",
	}
	check := func(id string, want map[string]any) {
		t.Helper()
		if got := e[id].State(v); !reflect.DeepEqual(got, want) {
			t.Errorf("%s state = %v, want %v", id, got, want)
		}
	}
	check("light.test_host_001_4", map[string]any{"on": true, "brightness": 40, "rgbw": []int{0, 0, 0, 0}, "kelvin": 3000, "color_mode": "color_temp"})
	check("light.test_host_002_17__zone_57", map[string]any{"on": true, "brightness": 100, "rgbw": []int{0, 0, 255, 0}, "color_mode": "rgbw"})
	check("switch.test_host_cove_scene", map[string]any{"on": true})
	check("button.test_host_002_1025__button_37", map[string]any{"led": false})
	check("button.test_host_004_6", map[string]any{})
	check("cover.test_host_002_1__zone_13", map[string]any{"position": 35})
}

func TestAppDBCommands(t *testing.T) {
	e := byID(must2(Build(appInventory(t), quiet)))
	h := &fakeHost{states: map[string]string{}}
	ctx := context.Background()
	run := func(id, action string, p Params, want ...string) {
		t.Helper()
		h.reqs = nil
		if err := e[id].Do(ctx, h, action, p); err != nil {
			t.Fatalf("%s %s: %v", id, action, err)
		}
		if !reflect.DeepEqual(h.reqs, want) {
			t.Errorf("%s %s sent\n %q\nwant\n %q", id, action, h.reqs, want)
		}
	}
	light := "Den|Test Host|Lighting|1|SVC_ENV_LIGHTING "
	shade := "Den|Test Host|Lighting|1|SVC_ENV_SHADE "
	atv := "Den|Den Apple TV|Media_server|1|SVC_AV_APPLEREMOTEMEDIASERVER "

	run("light.test_host_001_4", "turn_on", Params{"brightness": 60.0, "rgbw": []any{255.0, 10.0, 0.0, 0.0}},
		light+"DimmerSet Address1=001 Address2=4 DimmerLevel=60 bleColorRed=255 bleColorGreen=10 bleColorBlue=0 bleColorWhite=0")
	run("light.test_host_001_4", "turn_on", Params{"kelvin": 9000.0},
		light+"DimmerSet Address1=001 Address2=4 DimmerLevel=100 kelvin=6000")
	run("light.test_host_002_2__zone_43", "turn_off", nil,
		light+"DimmerSet Address1=002 Address2=2 Address3=/zone/43 DimmerLevel=0")
	run("switch.test_host_cove_scene", "turn_off", nil, light+"DeactivateScene Address1=Cove Scene")
	run("button.test_host_002_1025__button_37", "press", nil,
		light+"ButtonPressAndRelease Address1=002 Address2=1025 Address3=/button/37")
	run("button.test_host_004_6", "press", nil,
		light+"ButtonPress Address1=004 Address2=6", light+"ButtonRelease Address1=004 Address2=6")
	run("cover.test_host_002_1__zone_13", "stop", nil, shade+"GroupShadeStop Address1=002 Address2=1 Address3=/zone/13")
	run("cover.test_host_002_1__zone_13", "open", nil, shade+"GroupShadeUp Address1=002 Address2=1 Address3=/zone/13")
	run("cover.test_host_002_1__zone_13", "set_position", Params{"position": 40.0},
		shade+"GroupShadeSet Address1=002 Address2=1 Address3=/zone/13 ShadeLevel=40")
	run("media_player.den_apple_tv", "select_source", Params{"source": "netflix"}, atv+"PowerOn", atv+"Netflix")
	run("media_player.den_apple_tv", "select_source", Params{"source": "Youtube"}, atv+"Youtube")
	run("remote.den_apple_tv", "send_command", Params{"command": []any{"up", "select", "Menu"}},
		atv+"OSDCursorUp", atv+"OSDSelect", atv+"Menu")

	if err := e["remote.den_apple_tv"].Do(ctx, h, "send_command", Params{"command": []any{"up", "nope"}}); err == nil {
		t.Error("an unknown command should fail before anything is sent")
	}
	if err := e["media_player.den_apple_tv"].Do(ctx, h, "select_source", Params{"source": "Apple TV Restart"}); err == nil {
		t.Error("restart isn't an app")
	}
}

func must2[T any](v T, _ any) T { return v }

func TestCamerasAndEnergy(t *testing.T) {
	inv := appInventory(t)
	circuits, err := savantcfg.ReadEnergy("../../testdata/energyConfig.json")
	must(t, err)
	inv.Circuits = circuits
	e := byID(must2(Build(inv, quiet)))

	cam := e["camera.porch_camera_security_camera1"].Info()
	if cam.Zone != "" || cam.Device.Name != "Porch Camera" || cam.Secret["stream_url"] != "rtsp://viewer:secret@10.9.9.2/cam/realmonitor?channel=1&subtype=0" ||
		cam.Secret["snapshot_url"] == "" || cam.Secret["password"] != "secret" {
		t.Errorf("camera = %+v", cam)
	}
	if strings.Contains(cam.Source, "secret") || strings.Contains(fmtAttrs(cam.Attributes), "secret") {
		t.Error("camera logins must only be in Secret")
	}

	for id, state := range map[string]string{
		"sensor.energy_power":                     "Energy.Total.Consumption.Power",
		"sensor.energy_today":                     "Energy.TotalToday.Consumption.Power",
		"sensor.energy_group_lighting_power":      "Energy.Group.Lighting.Power",
		"sensor.energy_production":                "Energy.Total.Production.Power",
		"sensor.energy_circuit_pendants_1_power":  "Energy.Circuit.Pendants 1.Power",
		"sensor.energy_circuit_roof_array_energy": "Energy.Circuit.Roof Array.Energy",
	} {
		s, ok := e[id].(*Sensor)
		if !ok || s.StateName != state || s.Device != energyDevice {
			t.Errorf("%s = %+v", id, e[id])
		}
	}
	got := e["sensor.energy_today"].State(Values{"Energy.TotalToday.Consumption.Power": "76.88"})
	if got["value"] != 76.88 || e["sensor.energy_today"].Info().Attributes["state_class"] != "total_increasing" {
		t.Errorf("energy today = %v %v", got, e["sensor.energy_today"].Info().Attributes)
	}
}

func TestMusicNowPlaying(t *testing.T) {
	inv := appInventory(t)
	music := transport.Service{Zone: "Den", Component: "Savant Music", LogicalComponent: "Audio Zone 1", Variant: "1", Type: "SVC_AV_SAVANTMUSIC"}
	inv.Services["Den"] = append(inv.Services["Den"], music)
	inv.Defs.List = append(inv.Defs.List, &savantcfg.ServiceDef{Zone: "Den", Component: "Savant Music", LogicalComponent: "Audio Zone 1",
		Type: "SVC_AV_SAVANTMUSIC", Variant: "1", Alias: "Savant Music", Enabled: true,
		Requests: map[string][]string{"PowerOn": nil, "PowerOff": nil, "Play": nil, "Pause": nil, "SkipUp": nil, "SkipDown": nil,
			"ShuffleOn": nil, "ShuffleOff": nil, "RepeatOn": nil, "RepeatOff": nil, "VolumeUp": nil, "SetVolume": {"VolumeValue"}}})
	p := "Savant Music.Audio Zone 1."
	inv.Known = map[string]bool{}
	for _, st := range []string{"CurrentSongName", "CurrentArtistName", "CurrentAlbumName", "CurrentArtworkURL", "CurrentPauseStatus", "CurrentShuffleStatus", "CurrentRepeatStatus"} {
		inv.Known[p+st] = true
	}
	e := byID(must2(Build(inv, quiet)))
	player := e["media_player.den_savant_music"]
	for _, k := range []string{"play", "pause", "next", "previous", "shuffle", "repeat", "volume_set"} {
		if player.Info().Attributes[k] != true {
			t.Errorf("savant music should support %s: %v", k, player.Info().Attributes)
		}
	}
	v := Values{"Den.ActiveService": music.Dash(), p + "CurrentSongName": "So What", p + "CurrentArtistName": "Miles Davis",
		p + "CurrentAlbumName": "", p + "CurrentArtworkURL": "http://10.9.9.3/albumart?guid=1", p + "CurrentPauseStatus": "0",
		p + "CurrentShuffleStatus": "1", p + "CurrentRepeatStatus": "0"}
	want := map[string]any{"on": true, "title": "So What", "artist": "Miles Davis", "artwork": "http://10.9.9.3/albumart?guid=1",
		"paused": false, "shuffle": true, "repeat": "off"}
	if got := player.State(v); !reflect.DeepEqual(got, want) {
		t.Errorf("now playing = %v\nwant %v", got, want)
	}
	if room := e["media_player.den"].State(v); room["title"] != "So What" || room["source"] != "Savant Music" {
		t.Errorf("room = %v", room)
	}
	h := &fakeHost{states: map[string]string{}}
	room := e["media_player.den"].(*MediaRoom)
	room.setActive(music.Dash())
	must(t, player.Do(context.Background(), h, "shuffle", Params{"shuffle": false}))
	must(t, player.Do(context.Background(), h, "next", nil))
	if want := []string{music.Key() + " ShuffleOff", music.Key() + " SkipUp"}; !reflect.DeepEqual(h.reqs, want) {
		t.Errorf("sent %q", h.reqs)
	}
}

func fmtAttrs(m map[string]any) string {
	var b strings.Builder
	for k, v := range m {
		b.WriteString(k)
		b.WriteString(fmt.Sprint(v))
	}
	return b.String()
}

func TestThermostatAndGarage(t *testing.T) {
	inv := appInventory(t)
	e := byID(must2(Build(inv, quiet)))
	tstat, ok := e["climate.den_tstat_1"].(*Thermostat)
	if !ok {
		t.Fatalf("no thermostat: %v", keys(e))
	}
	if tstat.Single || !reflect.DeepEqual(tstat.Attributes["modes"], []string{"heat", "cool", "auto"}) ||
		tstat.Attributes["min_temp"] != 50.0 || tstat.Device.Name != "Climate" ||
		!reflect.DeepEqual(tstat.Attributes["fan_modes"], []string{"auto", "on"}) {
		t.Errorf("thermostat = %+v", tstat.Meta)
	}
	p := "Den Tstat.HVAC_controller."
	v := Values{p + "ThermostatCurrentTemperature_1": "71", p + "ThermostatCurrentHeatPoint_1": "68",
		p + "ThermostatCurrentCoolPoint_1": "76", p + "ThermostatMode_1": "Auto", p + "ThermostatHVACState_1": "Idle",
		p + "ThermostatFanMode_1": "Off or Auto"}
	want := map[string]any{"temperature": 71.0, "heat": 68.0, "cool": 76.0, "mode": "auto", "action": "idle", "fan": "auto"}
	if got := tstat.State(v); !reflect.DeepEqual(got, want) {
		t.Errorf("thermostat state = %v", got)
	}
	h := &fakeHost{states: map[string]string{}}
	ctx := context.Background()
	must(t, tstat.Do(ctx, h, "set_temperature", Params{"heat": 67.0, "cool": 77.5}))
	must(t, tstat.Do(ctx, h, "set_mode", Params{"mode": "cool"}))
	svc := "Den|Den Tstat|HVAC_controller|1|SVC_ENV_HVAC "
	if want := []string{svc + "SetHeatPointTemperature ThermostatAddress=1 HeatPointTemperature=67",
		svc + "SetCoolPointTemperature ThermostatAddress=1 CoolPointTemperature=77.5", svc + "SetHVACModeCool ThermostatAddress=1"}; !reflect.DeepEqual(h.reqs, want) {
		t.Errorf("sent %q", h.reqs)
	}

	g := e["cover.garage_controller_2"]
	if g == nil || g.Info().Device.Name != "Garage" || g.Info().Attributes["stop"] != true {
		t.Fatalf("garage = %v", g)
	}
	gs := "Garage Controller.Garage_controller."
	if got := g.State(Values{gs + "GarageDoorStatus_2": "Opening", gs + "GarageDoorError_2": "0"}); !reflect.DeepEqual(got, map[string]any{"state": "opening"}) {
		t.Errorf("garage state = %v", got)
	}
	if got := g.State(Values{gs + "GarageDoorStatus_2": "1"}); got["state"] != "open" {
		t.Errorf("garage state = %v", got)
	}
	h.reqs = nil
	must(t, g.Do(ctx, h, "close", nil))
	if want := []string{"Den|Garage Controller|Garage_controller|1|SVC_ENV_GARAGE CloseGarageDoor GarageDoorAddress=2"}; !reflect.DeepEqual(h.reqs, want) {
		t.Errorf("sent %q", h.reqs)
	}
}

func TestDeviceStatusAndRemotes(t *testing.T) {
	inv := appInventory(t)
	inv.Known = map[string]bool{}
	for _, n := range []string{"global.Den TV.ControlIsConnected", "Den TV.HD_monitor.IsPowered", "global.Den Apple TV.ControlIsConnected",
		"global.Family AVR.ControlIsConnected", "Family AVR.Receiver.CurrentInput", "Family AVR.Receiver_2.IsPowered",
		"global.Test Host.ControlIsConnected", "00AA11BB22CC33DD.BatteryLevel"} {
		inv.Known[n] = true
	}
	inv.Remotes = []ProRemote{{ID: "00AA11BB22CC33DD", Name: "Kitchen Remote", Model: "Pro Remote", Make: "Savant"}}
	e := byID(must2(Build(inv, quiet)))

	tv := e["binary_sensor.den_tv_connected"]
	if tv == nil || tv.Info().Device.Model != "OLED(xx)A1" || tv.Info().Device.Manufacturer != "LG" || tv.Info().Zone != "Den" {
		t.Fatalf("tv connected = %+v", tv)
	}
	if got := e["binary_sensor.den_tv_power"].State(Values{"Den TV.HD_monitor.IsPowered": "0.0"}); got["on"] != false {
		t.Errorf("tv power = %v", got)
	}
	atv := e["binary_sensor.den_apple_tv_connected"].Info().Device
	if atv.ID != "source_den_apple_tv" || atv.Manufacturer != "Apple" {
		t.Errorf("the Apple TV's status belongs on its source device: %+v", atv)
	}
	if e["sensor.family_avr_input"] == nil || e["binary_sensor.family_avr_zone_2_power"].Info().Name != "Zone 2 Power" {
		t.Errorf("receiver entities missing: %v", keys(e))
	}
	if e["binary_sensor.test_host_connected"] != nil {
		t.Error("the host itself already has a connection sensor")
	}
	bat := e["sensor.remote_00aa11bb22cc33dd_battery"]
	if bat == nil || bat.Info().Device.Name != "Kitchen Remote" || bat.Info().Attributes["device_class"] != nil {
		t.Fatalf("remote battery = %+v", bat)
	}
	if got := bat.State(Values{"00AA11BB22CC33DD.BatteryLevel": "87"}); got["value"] != "87" {
		t.Errorf("battery shows Savant's value as is: %v", got)
	}
	if e["sensor.remote_00aa11bb22cc33dd_wifi"] != nil {
		t.Error("only states the remote has get a sensor")
	}
	if got := findRemotes(inv); !reflect.DeepEqual(got, []string{"00AA11BB22CC33DD"}) {
		t.Errorf("remotes = %v", got)
	}
}

func keys[V any](m map[string]V) []string { return sortedKeys(m) }

func TestAlarmAndPool(t *testing.T) {
	e := byID(must2(Build(appInventory(t), quiet)))
	h := &fakeHost{states: map[string]string{}}
	ctx := context.Background()

	panel := "alarm_control_panel.security_alarm_panel_1"
	for raw, want := range map[string]any{"ArmedAway": "armed_away", "ArmedStay": "armed_home", "ArmedNightStay": "armed_night",
		"Disarmed": "disarmed", "AlarmTriggered": "triggered", "Exit Delay": nil} {
		if got := e[panel].State(Values{"Alarm Panel.Security_system.CurrentPartitionArmingStatus_1": raw}); got["state"] != want || got["status"] != raw {
			t.Errorf("%q -> %v, want %v", raw, got, want)
		}
	}
	if m := e[panel].Info().Attributes["modes"]; !reflect.DeepEqual(m, []string{"arm_away", "arm_home"}) {
		t.Errorf("alarm modes = %v", m)
	}
	must(t, e[panel].Do(ctx, h, "arm_away", Params{"code": "1234"}))
	if want := []string{"SAVANT_GLOBAL_ENV_ZONE|Alarm Panel|Security_system|1|SVC_ENV_SECURITYSYSTEM ArmAlarmAway PartitionNumber=1 UserCode=1234"}; !reflect.DeepEqual(h.reqs, want) {
		t.Errorf("sent %q", h.reqs)
	}
	if err := e[panel].Do(ctx, h, "arm_night", nil); err == nil {
		t.Error("the panel has no ArmAlarmNight request")
	}
	zone := e["sensor.security_zone_alarm_panel_1_3"]
	if got := zone.State(Values{"Alarm Panel.Security_system.CurrentZoneStatus_3": "Faulted"}); got["value"] != "Faulted" {
		t.Errorf("zone shows Savant's own text: %v", got)
	}

	h.reqs = nil
	must(t, e["switch.pool_aux_pool_controller_2"].Do(ctx, h, "turn_on", nil))
	if want := []string{"Den|Pool Controller|Pool_controller|1|SVC_ENV_POOLANDSPA SetAuxiliary2On"}; !reflect.DeepEqual(h.reqs, want) {
		t.Errorf("sent %q", h.reqs)
	}
}
