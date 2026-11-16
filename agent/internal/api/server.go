// Package api is the WebSocket API the integration connects to.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"savant-ha/internal/agent"
	"savant-ha/internal/transport"
)

const Protocol = 1

type Server struct {
	eng   *agent.Engine
	token string
	log   *slog.Logger
	up    websocket.Upgrader
}

func New(eng *agent.Engine, token string, log *slog.Logger) *Server {
	return &Server{eng: eng, token: token, log: log, up: websocket.Upgrader{
		CheckOrigin: func(*http.Request) bool { return true },
	}}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.health)
	mux.HandleFunc("/ws", s.ws)
	return mux
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"version": s.eng.Version(), "protocol": Protocol, "ready": s.eng.IsReady()})
}

func (s *Server) authorized(r *http.Request) bool {
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return s.token != "" && subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) == 1
}

type request struct {
	ID       int               `json:"id"`
	Type     string            `json:"type"`
	EntityID string            `json:"entity_id"`
	Action   string            `json:"action"`
	Params   agent.Params      `json:"params"`
	Zone     string            `json:"zone"`
	Comp     string            `json:"component"`
	Logical  string            `json:"logical_component"`
	Variant  string            `json:"variant"`
	SvcType  string            `json:"service_type"`
	Request  string            `json:"request"`
	Args     map[string]string `json:"args"`
}

type result struct {
	Type    string `json:"type"`
	ID      int    `json:"id"`
	Success bool   `json:"success"`
	Result  any    `json:"result,omitempty"`
	Error   string `json:"error,omitempty"`
}

const (
	pingEvery = 30 * time.Second
	readWait  = 75 * time.Second
)

func (s *Server) ws(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := s.up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	s.log.Info("client connected", "remote", r.RemoteAddr)
	ctx, cancel := context.WithCancel(r.Context())
	out := make(chan any, 64)
	done := make(chan struct{})

	go func() {
		defer close(done)
		ping := time.NewTicker(pingEvery)
		defer ping.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case m := <-out:
				conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := conn.WriteJSON(m); err != nil {
					cancel()
					return
				}
			case <-ping.C:
				if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)); err != nil {
					cancel()
					return
				}
			}
		}
	}()
	send := func(m any) {
		select {
		case out <- m:
		case <-ctx.Done():
		}
	}

	var sub *agent.Sub
	defer func() {
		cancel()
		<-done
		if sub != nil {
			s.eng.Unsubscribe(sub)
		}
		conn.Close()
		s.log.Info("client disconnected", "remote", r.RemoteAddr)
	}()

	send(map[string]any{"type": "hello", "version": s.eng.Version(), "protocol": Protocol, "ready": s.eng.IsReady()})
	conn.SetReadLimit(1 << 20)
	conn.SetReadDeadline(time.Now().Add(readWait))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(readWait)) })

	for {
		var req request
		if err := conn.ReadJSON(&req); err != nil {
			return
		}
		conn.SetReadDeadline(time.Now().Add(readWait))
		switch req.Type {
		case "subscribe":
			if sub != nil {
				send(result{"result", req.ID, false, nil, "already subscribed"})
				continue
			}
			select {
			case <-s.eng.Ready():
			case <-time.After(60 * time.Second):
				send(result{"result", req.ID, false, nil, "host not ready yet"})
				continue
			case <-ctx.Done():
				return
			}
			var snap agent.Snapshot
			sub, snap = s.eng.Subscribe()
			send(result{"result", req.ID, true, snap, ""})
			go func(sub *agent.Sub) {
				for ev := range sub.C {
					send(ev)
				}
				cancel()
			}(sub)
		default:
			go func(req request) { send(s.handle(ctx, req)) }(req)
		}
	}
}

func (s *Server) handle(ctx context.Context, req request) result {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var (
		res any
		err error
	)
	switch req.Type {
	case "command":
		err = s.eng.Command(ctx, req.EntityID, req.Action, req.Params)
	case "service_request":
		if req.Zone == "" || req.SvcType == "" || req.Request == "" {
			err = errors.New("service_request needs zone, service_type and request")
			break
		}
		err = s.eng.ServiceRequest(ctx, transport.Request{
			Service: transport.Service{Zone: req.Zone, Component: req.Comp, LogicalComponent: req.Logical, Variant: req.Variant, Type: req.SvcType},
			Name:    req.Request,
			Args:    req.Args,
		})
	case "diagnostics":
		res = s.eng.Diagnostics()
	case "rediscover":
		err = s.eng.Rediscover(ctx)
	default:
		err = errors.New("unknown request type " + req.Type)
	}
	if err != nil {
		return result{"result", req.ID, false, nil, err.Error()}
	}
	return result{"result", req.ID, true, res, ""}
}
