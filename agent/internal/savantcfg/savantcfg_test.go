package savantcfg

import (
	"reflect"
	"testing"
)

func TestReadDataTablesFixture(t *testing.T) {
	rows, err := ReadDataTables("../../testdata/shr2000-dataTableInfo.plist")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1 (door lock): %+v", len(rows), rows)
	}
	r := rows[0]
	if r.Table != "Door Lock" || r.Entity != "Lock" || r.Zone() != "Room 1" || r.Controller != "Door Lock Controller" ||
		r.LogicalComponent != "Door_lock" || r.ServiceID != "SVC_ENV_DOORLOCK" || !r.Enabled {
		t.Errorf("row = %+v", r)
	}
	if got := r.State("DoorLockStatus"); got != "Door Lock Controller.Door_lock.DoorLockStatus_1" {
		t.Errorf("state = %q", got)
	}
	if r.Fields["DoorLockAddress"] != "1" {
		t.Errorf("DoorLockAddress = %q", r.Fields["DoorLockAddress"])
	}
}

func TestReadServicesFixture(t *testing.T) {
	s, err := ReadServices("../../testdata/shr2000-serviceImplementation.xml.gz")
	if err != nil {
		t.Fatal(err)
	}
	lock := s.Find("Room 1", "Door Lock Controller", "Door_lock", "SVC_ENV_DOORLOCK", "")
	if lock == nil {
		t.Fatal("door lock service not found")
	}
	if got := lock.Requests["LockDoorLock"]; !reflect.DeepEqual(got, []string{"DoorLockAddress", "DoorLockAddress2"}) {
		t.Errorf("LockDoorLock args = %v", got)
	}
	light := s.Find("Room 1", "Vhern's", "RacePointMedia_host", "SVC_ENV_LIGHTING", "")
	if light == nil {
		t.Fatal("lighting service not found")
	}
	want := []string{"Address1", "Address2", "Address3", "Address4", "Address5", "Address6", "DimmerLevel", "FadeTime", "DelayTime"}
	if got := light.Requests["DimmerSet"]; !reflect.DeepEqual(got[:9], want) {
		t.Errorf("DimmerSet args = %v", got)
	}
	tv := s.Find("Room 1", "HD TV", "HD_monitor", "SVC_AV_TV", "1")
	if tv == nil || tv.Alias != "HD TV" || !tv.HasRequest("PowerOn") || !tv.HasRequest("MuteOn") || tv.HasRequest("SetVolume") {
		t.Errorf("tv = %+v", tv)
	}
	for _, d := range s.List {
		for name := range d.Requests {
			if len(name) > 6 && name[:6] == "Room 1" {
				t.Fatalf("inter-service request leaked: %q", name)
			}
		}
	}
}

func TestReadManifest(t *testing.T) {
	m, err := ReadManifest("../../testdata/shr2000-uimanifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if m.HostUID != "842F579B837F0000" || m.TemperatureUnits != "Fahrenheit" || m.ChildAliases["Room 1-PS5-Game_console-1-SVC_AV_GAME"] != "PS5" {
		t.Errorf("manifest = %+v", m)
	}
}
