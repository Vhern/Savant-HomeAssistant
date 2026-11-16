// Package transport talks to the Savant host through sclibridge.
package transport

import (
	"context"
	"fmt"
	"strings"
)

type Service struct {
	Zone             string `json:"zone"`
	Component        string `json:"component"`
	LogicalComponent string `json:"logical_component"`
	Variant          string `json:"variant"`
	Type             string `json:"type"`
}

func (s Service) Key() string {
	return strings.Join([]string{s.Zone, s.Component, s.LogicalComponent, s.Variant, s.Type}, "|")
}

func (s Service) Dash() string {
	return strings.Join([]string{s.Zone, s.Component, s.LogicalComponent, s.Variant, s.Type}, "-")
}

func (s Service) String() string {
	return fmt.Sprintf("%s/%s/%s/%s/%s", s.Zone, s.Component, s.LogicalComponent, s.Variant, s.Type)
}

type Request struct {
	Service  Service
	Name     string
	Args     map[string]string
	ArgOrder []string
}

type Scene struct {
	Name string `json:"name"`
	ID   string `json:"id"`
	User string `json:"user"`
}

type Transport interface {
	ListZones(ctx context.Context) ([]string, error)
	ListServices(ctx context.Context, zone string) ([]Service, error)
	StateNames(ctx context.Context, filter string) ([]string, error)
	ReadStates(ctx context.Context, names []string) (map[string]string, error)
	ServiceRequest(ctx context.Context, req Request) error
	Scenes(ctx context.Context) ([]Scene, error)
	ActivateScene(ctx context.Context, s Scene) error
}
