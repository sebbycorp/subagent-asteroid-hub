// Package config reads the app's settings from the environment.
package config

import (
	"fmt"
	"strconv"
	"time"
)

// Config is everything the app needs to start.
type Config struct {
	API          string
	Gateway      string
	UserID       string
	Token        string
	CAFile       string
	Namespace    string
	Listen       string
	OTLPListen   string // agentgateway's access logs; keep unpublished
	WaveInterval time.Duration
	AutoWaves    bool
	DemoAgents   bool
}

// FromEnv builds a Config; lookup is usually os.LookupEnv.
func FromEnv(lookup func(string) (string, bool)) (Config, error) {
	str := func(k, def string) string {
		if v, ok := lookup(k); ok && v != "" {
			return v
		}
		return def
	}
	boolean := func(k string, def bool) (bool, error) {
		v, ok := lookup(k)
		if !ok || v == "" {
			return def, nil
		}
		b, err := strconv.ParseBool(v)
		if err != nil {
			return false, fmt.Errorf("%s: %w", k, err)
		}
		return b, nil
	}
	c := Config{
		API:       str("KAGENT_API", "http://host.docker.internal:8083"),
		Gateway:   str("KAGENT_GATEWAY", ""),
		UserID:    str("KAGENT_USER_ID", "admin@kagent.dev"),
		Token:     str("KAGENT_TOKEN", ""),
		CAFile:    str("KAGENT_CA_FILE", ""),
		Namespace: str("KAGENT_NAMESPACE", "kagent"),
		Listen:    str("LISTEN_ADDR", ":7790"),
		// agentgateway pushes its access logs here. Only the kind network
		// should reach it: don't publish this port.
		OTLPListen: str("OTLP_LISTEN_ADDR", ":4318"),
	}
	d, err := time.ParseDuration(str("WAVE_INTERVAL", "20s"))
	if err != nil {
		return Config{}, fmt.Errorf("WAVE_INTERVAL: %w", err)
	}
	c.WaveInterval = d
	// Chats cost model tokens, so waves are manual (T) unless asked for.
	if c.AutoWaves, err = boolean("AUTO_WAVES", false); err != nil {
		return Config{}, err
	}
	if c.DemoAgents, err = boolean("DEMO_AGENTS", false); err != nil {
		return Config{}, err
	}
	return c, nil
}
