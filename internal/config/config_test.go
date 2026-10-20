package config

import (
	"testing"
	"time"
)

func env(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func TestFromEnvDefaults(t *testing.T) {
	c, err := FromEnv(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	want := Config{
		API: "http://host.docker.internal:8083", Gateway: "", UserID: "admin@kagent.dev",
		Namespace: "kagent", Listen: ":7790", WaveInterval: 20 * time.Second, AutoWaves: true, DemoAgents: false,
	}
	if c != want {
		t.Fatalf("got  %+v\nwant %+v", c, want)
	}
}

func TestFromEnvOverrides(t *testing.T) {
	c, err := FromEnv(env(map[string]string{
		"KAGENT_API": "http://localhost:8083", "KAGENT_GATEWAY": "http://gw:8083", "KAGENT_USER_ID": "me@x.dev",
		"KAGENT_TOKEN": "tok", "KAGENT_CA_FILE": "/ca.pem", "KAGENT_NAMESPACE": "agents", "LISTEN_ADDR": ":9000",
		"WAVE_INTERVAL": "45s", "AUTO_WAVES": "false", "DEMO_AGENTS": "true",
	}))
	if err != nil {
		t.Fatal(err)
	}
	want := Config{
		API: "http://localhost:8083", Gateway: "http://gw:8083", UserID: "me@x.dev", Token: "tok", CAFile: "/ca.pem",
		Namespace: "agents", Listen: ":9000", WaveInterval: 45 * time.Second, AutoWaves: false, DemoAgents: true,
	}
	if c != want {
		t.Fatalf("got  %+v\nwant %+v", c, want)
	}
}

func TestFromEnvBadDuration(t *testing.T) {
	if _, err := FromEnv(env(map[string]string{"WAVE_INTERVAL": "soon"})); err == nil {
		t.Fatal("want error for bad WAVE_INTERVAL")
	}
	if _, err := FromEnv(env(map[string]string{"AUTO_WAVES": "maybe"})); err == nil {
		t.Fatal("want error for bad AUTO_WAVES")
	}
}
