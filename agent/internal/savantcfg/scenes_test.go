package savantcfg

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

const testSceneDB = `
CREATE TABLE Scenes ( identifier TEXT UNIQUE, name TEXT, imageKey TEXT, tags TEXT, isGlobal INTEGER, isActive INTEGER, isScheduled INTEGER, hasCustomImage INTEGER , alias TEXT, isSceneOn INTEGER);
CREATE TABLE SceneUserMap ( id INTEGER PRIMARY KEY, sceneIdentifier TEXT, user Text, sceneOrder INTEGER );
CREATE TABLE SceneDefinitions ( id INTEGER PRIMARY KEY, sceneIdentifier TEXT, definition TEXT );
INSERT INTO Scenes (identifier, name, isGlobal, alias) VALUES
  ('A718B42D-0873-4FB3-8D26-6B4154E964B9', 'Bath Time', 1, ''),
  ('6B32980B-55EB-4DF4-9D24-B8230870D05F', 'Chill', 0, NULL),
  ('11111111-2222-3333-4444-555555555555', 'Movie | Night', 0, 'Movie Night');
INSERT INTO SceneUserMap (sceneIdentifier, user) VALUES
  ('A718B42D-0873-4FB3-8D26-6B4154E964B9', 'someone@example.com'),
  ('A718B42D-0873-4FB3-8D26-6B4154E964B9', 'admin'),
  ('6B32980B-55EB-4DF4-9D24-B8230870D05F', 'someone@example.com');
INSERT INTO SceneDefinitions (sceneIdentifier, definition) VALUES
  ('A718B42D-0873-4FB3-8D26-6B4154E964B9', '{"av":{"PS5.Game_console":{"serviceID":"SVC_AV_GAME","rooms":["Room 1"]}},"fadeTime":3,"power":{"rooms":{"Room 1":{"Room 1-PS5-Game_console-1-SVC_AV_GAME":1}}}}'),
  ('6B32980B-55EB-4DF4-9D24-B8230870D05F', '{"volume":{"Great Room":20},"power":{"lightingOff":["Great Room"],"rooms":{"Great Room":{}}}}');
`

func TestReadAppScenes(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not installed")
	}
	db := filepath.Join(t.TempDir(), "dashboard.sqlite")
	if out, err := exec.Command("sqlite3", db, testSceneDB).CombinedOutput(); err != nil {
		t.Fatalf("create db: %v %s", err, out)
	}
	// A ~/.sqliterc like the one on the SHR-2000 must not change the output.
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".sqliterc"), []byte(".mode column\n.headers on\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	got, err := ReadAppScenes(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	want := []AppScene{
		{ID: "A718B42D-0873-4FB3-8D26-6B4154E964B9", Name: "Bath Time", User: "admin", Global: true, Rooms: []string{"Room 1"}},
		{ID: "6B32980B-55EB-4DF4-9D24-B8230870D05F", Name: "Chill", User: "someone@example.com", Rooms: []string{"Great Room"}},
		{ID: "11111111-2222-3333-4444-555555555555", Name: "Movie Night"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("scenes:\n got %+v\nwant %+v", got, want)
	}
}

func TestSceneDB(t *testing.T) {
	got := SceneDB("/data/RPM/GNUstep/Library/ApplicationSupport/RacePointMedia/userConfig.rpmConfig")
	if got != "/data/RPM/GNUstep/Library/ApplicationSupport/RacePointMedia/statusfiles/dis/dashboard.sqlite" {
		t.Errorf("SceneDB = %q", got)
	}
}

func TestUnreadableSceneOutputIsAnError(t *testing.T) {
	if _, err := parseAppScenes("identifier  name\n----------  ----\nABC         Bath Time\n"); err == nil {
		t.Error("column-mode output should be reported, not read as zero scenes")
	}
	if got, err := parseAppScenes(""); err != nil || len(got) != 0 {
		t.Errorf("an empty database is fine: %v %v", got, err)
	}
}
