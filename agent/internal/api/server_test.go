package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"savant-ha/internal/agent"
	"savant-ha/internal/config"
	"savant-ha/internal/transport"
)

type host struct {
	mu     sync.Mutex
	active string
	reqs   []string
}

func (h *host) ListZones(context.Context) ([]string, error) { return []string{"Den"}, nil }
func (h *host) ListServices(context.Context, string) ([]transport.Service, error) {
	return []transport.Service{
		{Zone: "Den", Component: "Apple TV", LogicalComponent: "Media_server", Variant: "1", Type: "SVC_AV_APPLEREMOTEMEDIASERVER"},
		{Zone: "Den", Component: "Host", Variant: "1", Type: "SVC_GEN_GENERIC"},
	}, nil
}
func (h *host) StateNames(context.Context, string) ([]string, error) { return nil, nil }
func (h *host) ReadStates(_ context.Context, names []string) (map[string]string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := map[string]string{}
	for _, n := range names {
		switch n {
		case "Den.ActiveService":
			out[n] = h.active
		case "Host.SoftwareVersion":
			out[n] = "11.0.5"
		}
	}
	return out, nil
}
func (h *host) ServiceRequest(_ context.Context, r transport.Request) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.reqs = append(h.reqs, r.Service.Dash()+" "+r.Name)
	if r.Name == "PowerOn" {
		h.active = r.Service.Dash()
	}
	return nil
}
func (h *host) Scenes(context.Context) ([]transport.Scene, error)    { return nil, nil }
func (h *host) ActivateScene(context.Context, transport.Scene) error { return nil }

func TestWebSocketAPI(t *testing.T) {
	cfg := config.Defaults()
	cfg.ConfigDir = t.TempDir()
	cfg.PollInterval = config.Duration(50 * time.Millisecond)
	h := &host{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	eng := agent.NewEngine(h, cfg, "1.2.3", log)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go eng.Run(ctx)
	srv := httptest.NewServer(New(eng, "secret", log).Handler())
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	if _, resp, err := websocket.DefaultDialer.Dial(url, http.Header{"Authorization": {"Bearer wrong"}}); err == nil || resp.StatusCode != 401 {
		t.Fatalf("bad token should get 401, got %v", err)
	}
	c, _, err := websocket.DefaultDialer.Dial(url, http.Header{"Authorization": {"Bearer secret"}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(5 * time.Second))

	var hello map[string]any
	must(t, c.ReadJSON(&hello))
	if hello["type"] != "hello" || hello["version"] != "1.2.3" {
		t.Fatalf("hello = %v", hello)
	}
	must(t, c.WriteJSON(map[string]any{"id": 1, "type": "subscribe"}))
	res := readUntil(t, c, func(m map[string]any) bool { return m["type"] == "result" && m["id"] == 1.0 })
	snap := res["result"].(map[string]any)
	if snap["host"].(map[string]any)["software"] != "11.0.5" {
		t.Errorf("host = %v", snap["host"])
	}
	var ids []string
	for _, e := range snap["entities"].([]any) {
		ids = append(ids, e.(map[string]any)["id"].(string))
	}
	if !contains(ids, "media_player.den") || !contains(ids, "sensor.host_software") {
		t.Errorf("entities = %v", ids)
	}

	must(t, c.WriteJSON(map[string]any{"id": 2, "type": "command", "entity_id": "media_player.den", "action": "select_source", "params": map[string]any{"source": "Apple TV"}}))
	if r := readUntil(t, c, func(m map[string]any) bool { return m["type"] == "result" && m["id"] == 2.0 }); r["success"] != true {
		t.Fatalf("command result = %v", r)
	}
	ev := readUntil(t, c, func(m map[string]any) bool {
		st, _ := m["state"].(map[string]any)
		return m["type"] == "state" && m["entity_id"] == "media_player.den" && st["on"] == true
	})
	if ev["state"].(map[string]any)["source"] != "Apple TV" {
		t.Errorf("state event = %v", ev)
	}

	must(t, c.WriteJSON(map[string]any{"id": 3, "type": "command", "entity_id": "light.nope", "action": "turn_on"}))
	if r := readUntil(t, c, func(m map[string]any) bool { return m["type"] == "result" && m["id"] == 3.0 }); r["success"] != false || r["error"] == "" {
		t.Errorf("unknown entity result = %v", r)
	}
	must(t, c.WriteJSON(map[string]any{"id": 4, "type": "diagnostics"}))
	if r := readUntil(t, c, func(m map[string]any) bool { return m["type"] == "result" && m["id"] == 4.0 }); r["success"] != true {
		t.Errorf("diagnostics = %v", r)
	}
}

func readUntil(t *testing.T, c *websocket.Conn, ok func(map[string]any) bool) map[string]any {
	t.Helper()
	for {
		var m map[string]any
		if err := c.ReadJSON(&m); err != nil {
			t.Fatalf("read: %v", err)
		}
		if ok(m) {
			return m
		}
	}
}

func contains(l []string, s string) bool {
	for _, v := range l {
		if v == s {
			return true
		}
	}
	return false
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
