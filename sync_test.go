package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kbinani/screenshot"
)

type haCall struct {
	method, path string
	body         map[string]any
}

// fakeHA records every request and serves a fixed "original" light state.
type fakeHA struct {
	mu    sync.Mutex
	calls []haCall
	fail  bool // when set, service calls return 500
}

func (f *fakeHA) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	f.mu.Lock()
	f.calls = append(f.calls, haCall{r.Method, r.URL.Path, body})
	fail := f.fail
	f.mu.Unlock()

	if strings.HasPrefix(r.URL.Path, "/api/states/") {
		w.Write([]byte(`{"state":"on","attributes":{"rgb_color":[9,8,7],"brightness":42}}`))
		return
	}
	if fail && strings.HasPrefix(r.URL.Path, "/api/services/") {
		http.Error(w, "boom", http.StatusInternalServerError)
		return
	}
	w.Write([]byte(`[]`))
}

func (f *fakeHA) snapshot() []haCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]haCall(nil), f.calls...)
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func testConfig(url string) *Config {
	c := &Config{}
	c.Env.HA_URL = url
	c.Env.HA_TOKEN = "tok"
	c.Env.LED_ENTITY = "light.test"
	c.Env.UPDATE_INTERVAL_MS = 20
	c.Env.MIN_CALL_INTERVAL_MS = 20
	c.applyDefaults()
	return c
}

func countPath(calls []haCall, p string) int {
	n := 0
	for _, c := range calls {
		if c.path == p {
			n++
		}
	}
	return n
}

// Runs the real capture loop against a fake Home Assistant: it must send a
// color, then restore the original state when stopped.
func TestSyncer_SendsColorAndRestoresOnStop(t *testing.T) {
	if screenshot.NumActiveDisplays() == 0 {
		t.Skip("no display available")
	}
	ha := &fakeHA{}
	srv := httptest.NewServer(ha)
	defer srv.Close()
	cfgPtr.Store(testConfig(srv.URL))
	baseDir = t.TempDir()

	s := &syncer{}
	s.Start()
	if !s.Running() {
		t.Fatal("expected Running after Start")
	}
	waitFor(t, "first color call", func() bool {
		return countPath(ha.snapshot(), "/api/services/light/turn_on") >= 1
	})
	s.Stop()
	if s.Running() {
		t.Fatal("expected not Running after Stop")
	}

	calls := ha.snapshot()
	first := -1
	for i, c := range calls {
		if c.path == "/api/services/light/turn_on" {
			first = i
			break
		}
	}
	if first < 0 || calls[first].body["entity_id"] != "light.test" || calls[first].body["rgb_color"] == nil {
		t.Fatalf("bad color call: %+v", calls)
	}
	last := calls[len(calls)-1]
	rgb, _ := last.body["rgb_color"].([]any)
	if last.path != "/api/services/light/turn_on" || len(rgb) != 3 || rgb[0] != float64(9) || last.body["brightness"] != float64(42) {
		t.Errorf("original state not restored, last call: %+v", last)
	}

	s.Stop() // a second Stop must be harmless
}

func TestSyncer_KeepColorOnStop(t *testing.T) {
	if screenshot.NumActiveDisplays() == 0 {
		t.Skip("no display available")
	}
	ha := &fakeHA{}
	srv := httptest.NewServer(ha)
	defer srv.Close()
	cfg := testConfig(srv.URL)
	cfg.Env.KEEP_COLOR_ON_STOP = true
	cfgPtr.Store(cfg)
	baseDir = t.TempDir()

	s := &syncer{}
	s.Start()
	waitFor(t, "first color call", func() bool { return countPath(ha.snapshot(), "/api/services/light/turn_on") >= 1 })
	s.Stop()

	for _, c := range ha.snapshot() {
		if rgb, ok := c.body["rgb_color"].([]any); ok && len(rgb) == 3 && rgb[0] == float64(9) {
			t.Error("original state restored although KEEP_COLOR_ON_STOP is set")
		}
	}
}

func TestSyncer_RecoversAfterHAError(t *testing.T) {
	if screenshot.NumActiveDisplays() == 0 {
		t.Skip("no display available")
	}
	ha := &fakeHA{fail: true}
	srv := httptest.NewServer(ha)
	defer srv.Close()
	cfg := testConfig(srv.URL)
	cfg.Env.KEEP_COLOR_ON_STOP = true
	cfgPtr.Store(cfg)
	baseDir = t.TempDir()

	var mu sync.Mutex
	var statuses []string
	s := &syncer{onStatus: func(m string) { mu.Lock(); statuses = append(statuses, m); mu.Unlock() }}
	s.Start()
	defer s.Stop()

	waitFor(t, "failed attempt", func() bool { return countPath(ha.snapshot(), "/api/services/light/turn_on") >= 1 })
	ha.mu.Lock()
	ha.fail = false
	ha.mu.Unlock()
	// After the (1s) backoff the loop must retry the same color and succeed.
	waitFor(t, "status back to Running", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(statuses) >= 2 && statuses[len(statuses)-1] == "Running" && strings.Contains(strings.Join(statuses, ","), "Home Assistant error")
	})
}
