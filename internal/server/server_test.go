package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/sebbycorp/subagent-asteroid-hub/internal/hub"
	"github.com/sebbycorp/subagent-asteroid-hub/internal/model"
	"github.com/sebbycorp/subagent-asteroid-hub/internal/traffic"
)

type emptySource struct{}

func (emptySource) Snapshot(context.Context) (model.Snapshot, error) {
	return model.Snapshot{TakenAt: time.Now(), Actors: map[string]model.Actor{}}, nil
}

type fakeWaves struct {
	waveErr error
	auto    bool
	demo    []string
}

func (f *fakeWaves) Wave(context.Context) (int, error) {
	if f.waveErr != nil {
		return 0, f.waveErr
	}
	return 3, nil
}
func (f *fakeWaves) SetAuto(on bool) { f.auto = on }
func (f *fakeWaves) Auto() bool      { return f.auto }
func (f *fakeWaves) EnsureDemoAgents(context.Context) ([]string, error) {
	return f.demo, nil
}

type rig struct {
	srv     *httptest.Server
	waves   *fakeWaves
	connect []ConnInfo
}

func newRig(t *testing.T, waveErr error) *rig {
	r := &rig{waves: &fakeWaves{waveErr: waveErr, demo: []string{"asteroids-demo-scout"}}}
	h := hub.New(emptySource{}, "env")
	connector := func(c ConnInfo) error {
		if c.API == "bad" {
			return errors.New("dial failed")
		}
		r.connect = append(r.connect, c)
		return nil
	}
	assets := fstest.MapFS{"index.html": {Data: []byte("<title>show</title>")}}
	r.srv = httptest.NewServer(New(h, r.waves, connector, assets, ConnInfo{API: "http://a:8083", UserID: "admin@kagent.dev", Token: "secret", Namespace: "kagent"}))
	t.Cleanup(r.srv.Close)
	return r
}

func post(t *testing.T, url, body string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(b)
}

func TestServerIndex(t *testing.T) {
	r := newRig(t, nil)
	resp, err := http.Get(r.srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(b), "<title>show</title>") {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
}

func TestServerWave202(t *testing.T) {
	r := newRig(t, nil)
	resp, body := post(t, r.srv.URL+"/api/wave", "")
	if resp.StatusCode != http.StatusAccepted || !strings.Contains(body, `"sent":3`) {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	resp2, err := http.Get(r.srv.URL + "/traffic")
	if err != nil || resp2.StatusCode != http.StatusAccepted {
		t.Fatalf("GET /traffic: %v %v", err, resp2.StatusCode)
	}
}

func TestServerWaveConflict409(t *testing.T) {
	for _, e := range []error{traffic.ErrWaveRunning, traffic.ErrNoAgents} {
		r := newRig(t, e)
		resp, body := post(t, r.srv.URL+"/api/wave", "")
		if resp.StatusCode != http.StatusConflict || !strings.Contains(body, e.Error()) {
			t.Fatalf("%v: %d %s", e, resp.StatusCode, body)
		}
	}
}

func TestServerAutoToggle(t *testing.T) {
	r := newRig(t, nil)
	resp, _ := post(t, r.srv.URL+"/api/auto", `{"on":true}`)
	if resp.StatusCode != 200 || !r.waves.auto {
		t.Fatalf("auto not set: %d", resp.StatusCode)
	}
}

func TestServerDemoAgents(t *testing.T) {
	r := newRig(t, nil)
	resp, body := post(t, r.srv.URL+"/api/demo-agents", "")
	if resp.StatusCode != 200 || !strings.Contains(body, "asteroids-demo-scout") {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
}

func TestServerSSEFirstMessageIsState(t *testing.T) {
	r := newRig(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", r.srv.URL+"/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type %q", ct)
	}
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var m struct {
			Type  string    `json:"type"`
			State hub.State `json:"state"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &m); err != nil {
			t.Fatal(err)
		}
		if m.Type != "state" || m.State.Env != "env" {
			t.Fatalf("first message %+v", m)
		}
		return
	}
	t.Fatalf("no data line: %v", sc.Err())
}

func TestServerConfigHidesToken(t *testing.T) {
	r := newRig(t, nil)
	resp, err := http.Get(r.srv.URL + "/api/config")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(b), "secret") || !strings.Contains(string(b), "http://a:8083") {
		t.Fatalf("config %s", b)
	}
}

func TestServerConfigReconnectCallsConnector(t *testing.T) {
	r := newRig(t, nil)
	resp, body := post(t, r.srv.URL+"/api/config", `{"api":"http://b:8083","userId":"me","token":"t2"}`)
	if resp.StatusCode != 200 || len(r.connect) != 1 || r.connect[0].API != "http://b:8083" || r.connect[0].Token != "t2" {
		t.Fatalf("%d %s %+v", resp.StatusCode, body, r.connect)
	}
	if r.connect[0].Namespace != "kagent" {
		t.Fatalf("namespace not carried over: %+v", r.connect[0])
	}
	getResp, _ := http.Get(r.srv.URL + "/api/config")
	b, _ := io.ReadAll(getResp.Body)
	if !strings.Contains(string(b), "http://b:8083") {
		t.Fatalf("config not updated: %s", b)
	}
	resp, body = post(t, r.srv.URL+"/api/config", `{"api":"bad"}`)
	if resp.StatusCode != 400 || !strings.Contains(body, "dial failed") {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
}
