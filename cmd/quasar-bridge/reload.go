package main

import (
	"errors"
	"reflect"

	"quasarbridge/internal/bridge"
)

func reloadRouting(path string, initial config, engine *bridge.Engine) error {
	next, err := readConfig(path)
	if err != nil {
		return err
	}
	// Listener, credentials, backend clients and warm-up jobs remain bound to the
	// original deployment. Do not silently pretend those settings were reloaded.
	a, b := initial, next
	a.Routing = bridge.RoutingConfig{}
	b.Routing = bridge.RoutingConfig{}
	if !reflect.DeepEqual(a, b) {
		return errors.New("non-routing settings changed; restart required")
	}
	return engine.SetRouting(next.Routing)
}
