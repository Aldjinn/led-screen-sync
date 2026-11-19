package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testYAML = `# my config
env:
  # Home Assistant URL
  HA_URL: "http://ha.local:8123"
  HA_TOKEN: "secret-token-123"
  LED_ENTITY: "light.old"   # the strip
  EXPORT_JSON: false
  COLOR_CHANGE_THRESHOLD: 32.0
  UPDATE_INTERVAL_MS: 100
  LOG_LEVEL: "info"
  SOME_FUTURE_KEY: keep-me
`

// newSettingsTest starts a settings server (real handlers) on a temp config.
func newSettingsTest(t *testing.T) (*settingsServer, *httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "led-screen-sync.yaml")
	if err := os.WriteFile(path, []byte(testYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath, baseDir = path, dir
	cfg, err := loadAndValidate(path)
	if err != nil {
		t.Fatal(err)
	}
	cfgPtr.Store(cfg)

	s := &settingsServer{loop: &syncer{}, reload: reloadConfig, token: "tok"}
	srv := httptest.NewServer(s.handler())
	t.Cleanup(srv.Close)
	s.host = strings.TrimPrefix(srv.URL, "http://")
	return s, srv, path
}

func do(t *testing.T, srv *httptest.Server, method, path string, body any, hdr map[string]string) (int, map[string]any) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, srv.URL+path, rd)
	req.Header.Set("X-Token", "tok")
	for k, v := range hdr {
		if k == "Host" {
			req.Host = v
			continue
		}
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestSettings_AccessControl(t *testing.T) {
	_, srv, _ := newSettingsTest(t)

	if code, _ := do(t, srv, "GET", "/api/state", nil, map[string]string{"X-Token": ""}); code != 403 {
		t.Errorf("missing token: got %d, want 403", code)
	}
	if code, _ := do(t, srv, "GET", "/api/state", nil, map[string]string{"X-Token": "nope"}); code != 403 {
		t.Errorf("wrong token: got %d, want 403", code)
	}
	if code, _ := do(t, srv, "GET", "/api/state", nil, map[string]string{"Host": "evil.example"}); code != 403 {
		t.Errorf("foreign Host (DNS rebinding): got %d, want 403", code)
	}
	if code, _ := do(t, srv, "POST", "/api/sync", map[string]string{"Action": "stop"}, map[string]string{"Origin": "http://evil.example"}); code != 403 {
		t.Errorf("foreign Origin: got %d, want 403", code)
	}
	if code, _ := do(t, srv, "POST", "/api/state", nil, nil); code != 405 {
		t.Errorf("wrong method: got %d, want 405", code)
	}
	if code, _ := do(t, srv, "GET", "/api/state", nil, nil); code != 200 {
		t.Errorf("valid request: got %d, want 200", code)
	}

	// The page itself needs ?t=
	resp, _ := http.Get(srv.URL + "/")
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Errorf("index without token: got %d", resp.StatusCode)
	}
	resp, _ = http.Get(srv.URL + "/?t=tok")
	page, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !bytes.Contains(page, []byte("LED Screen Sync")) {
		t.Errorf("index with token: got %d", resp.StatusCode)
	}
}

func TestSettings_StateNeverContainsToken(t *testing.T) {
	_, srv, _ := newSettingsTest(t)
	req, _ := http.NewRequest("GET", srv.URL+"/api/state", nil)
	req.Header.Set("X-Token", "tok")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if bytes.Contains(raw, []byte("secret-token-123")) {
		t.Fatal("state response leaks the access token")
	}
	var st struct {
		TokenSet bool `json:"token_set"`
		Config   EnvConfig
	}
	_ = json.Unmarshal(raw, &st)
	if !st.TokenSet || st.Config.HA_TOKEN != "" || st.Config.LED_ENTITY != "light.old" {
		t.Errorf("unexpected state: %+v", st)
	}
}

func TestSettings_SaveKeepsCommentsAndToken(t *testing.T) {
	_, srv, path := newSettingsTest(t)

	_, st := do(t, srv, "GET", "/api/state", nil, nil)
	cfg := st["config"].(map[string]any)
	cfg["LED_ENTITY"] = "light.new"
	cfg["COLOR_CHANGE_THRESHOLD"] = 75.0
	cfg["TRANSITION_MS"] = 900.0
	cfg["COLOR_MODE"] = "vibrant"
	cfg["HA_TOKEN"] = "" // blank = keep

	if code, out := do(t, srv, "POST", "/api/config", cfg, nil); code != 200 {
		t.Fatalf("save failed: %d %v", code, out)
	}

	data, _ := os.ReadFile(path)
	text := string(data)
	for _, want := range []string{"# my config", "# Home Assistant URL", "# the strip", "SOME_FUTURE_KEY: keep-me", `"secret-token-123"`, `"light.new"`, "TRANSITION_MS: 900", `COLOR_MODE: "vibrant"`} {
		if !strings.Contains(text, want) {
			t.Errorf("saved file is missing %q:\n%s", want, text)
		}
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Error("expected a .bak backup of the previous file")
	}

	// It must reload and apply.
	e := currentConfig().Env
	if e.LED_ENTITY != "light.new" || e.COLOR_CHANGE_THRESHOLD != 75 || e.TRANSITION_MS != 900 || e.HA_TOKEN != "secret-token-123" {
		t.Errorf("config not applied: %+v", e)
	}
}

func TestSettings_SaveNewToken(t *testing.T) {
	_, srv, path := newSettingsTest(t)
	_, st := do(t, srv, "GET", "/api/state", nil, nil)
	cfg := st["config"].(map[string]any)
	cfg["HA_TOKEN"] = "brand-new-token"
	if code, out := do(t, srv, "POST", "/api/config", cfg, nil); code != 200 {
		t.Fatalf("save failed: %d %v", code, out)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"brand-new-token"`) || strings.Contains(string(data), "secret-token-123") {
		t.Errorf("token not replaced:\n%s", data)
	}
}

func TestSettings_SaveRejectsInvalidAndLeavesFileAlone(t *testing.T) {
	_, srv, path := newSettingsTest(t)
	before, _ := os.ReadFile(path)

	_, st := do(t, srv, "GET", "/api/state", nil, nil)
	cfg := st["config"].(map[string]any)
	cfg["HA_URL"] = "not a url"
	code, out := do(t, srv, "POST", "/api/config", cfg, nil)
	if code != 400 || !strings.Contains(out["error"].(string), "HA_URL") {
		t.Errorf("want 400 mentioning HA_URL, got %d %v", code, out)
	}
	cfg["HA_URL"] = "http://ha.local:8123"
	cfg["COLOR_MODE"] = "rainbow"
	if code, _ := do(t, srv, "POST", "/api/config", cfg, nil); code != 400 {
		t.Errorf("bad color mode: got %d, want 400", code)
	}
	if code, _ := do(t, srv, "POST", "/api/config", map[string]any{"NOPE": 1}, nil); code != 400 {
		t.Errorf("unknown field: got %d, want 400", code)
	}

	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Error("file was modified by a rejected save")
	}
}

func TestSettings_DiscoverLights(t *testing.T) {
	_, srv, _ := newSettingsTest(t)
	ha := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer saved-or-typed" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`[
			{"entity_id":"sensor.temp","state":"20","attributes":{}},
			{"entity_id":"light.b","state":"off","attributes":{"friendly_name":"Bee"}},
			{"entity_id":"light.a","state":"on","attributes":{"friendly_name":"Ay"}}]`))
	}))
	defer ha.Close()

	code, out := do(t, srv, "POST", "/api/discover", map[string]string{"HA_URL": ha.URL, "HA_TOKEN": "saved-or-typed"}, nil)
	lights, _ := out["lights"].([]any)
	if code != 200 || len(lights) != 2 || lights[0].(map[string]any)["id"] != "light.a" {
		t.Errorf("discover: %d %v", code, out)
	}
	// Wrong token surfaces the Home Assistant error.
	code, out = do(t, srv, "POST", "/api/discover", map[string]string{"HA_URL": ha.URL, "HA_TOKEN": "wrong"}, nil)
	if code != 502 || !strings.Contains(out["error"].(string), "401") {
		t.Errorf("bad token: %d %v", code, out)
	}
	if code, _ := do(t, srv, "POST", "/api/discover", map[string]string{"HA_URL": "nonsense"}, nil); code != 400 {
		t.Errorf("bad url: got %d, want 400", code)
	}
}

func TestWriteEnvToYAML_AppendsMissingKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.yaml")
	os.WriteFile(path, []byte("env:\n  HA_URL: \"http://x:1\"\n"), 0o644)
	e := EnvConfig{HA_URL: "http://x:1", HA_TOKEN: "t", LED_ENTITY: "light.a", COLOR_CHANGE_THRESHOLD: 40, BRIGHTNESS: 200, LOG_LEVEL: "info", COLOR_MODE: "dominant"}
	if err := writeEnvToYAML(path, e); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Env.LED_ENTITY != "light.a" || cfg.Env.COLOR_CHANGE_THRESHOLD != 40 || cfg.Env.BRIGHTNESS != 200 || cfg.Env.HA_TOKEN != "t" {
		t.Errorf("round trip failed: %+v", cfg.Env)
	}
}

func TestPackColor(t *testing.T) {
	if unpackHex(0) != "" {
		t.Error("zero should mean no color")
	}
	if got := unpackHex(packColor(RGB{0, 0, 0})); got != "#000000" {
		t.Errorf("black should be a real color, got %q", got)
	}
	if got := unpackHex(packColor(RGB{255, 128, 1})); got != "#ff8001" {
		t.Errorf("got %q", got)
	}
}

// TestPreviewServer is a manual helper (skipped unless PREVIEW_ADDR is set): it
// runs the real settings page against a fake Home Assistant and the real
// capture loop, so the page can be inspected in a browser.
func TestPreviewServer(t *testing.T) {
	addr := os.Getenv("PREVIEW_ADDR")
	if addr == "" {
		t.Skip("manual helper; set PREVIEW_ADDR=127.0.0.1:PORT, PREVIEW_URL_FILE and PREVIEW_STOP_FILE")
	}
	ha := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/states":
			w.Write([]byte(`[{"entity_id":"light.wohnzimmer_led_streifen","state":"on","attributes":{"friendly_name":"LED Streifen"}},{"entity_id":"light.flur","state":"off","attributes":{"friendly_name":"Flur"}}]`))
		case strings.HasPrefix(r.URL.Path, "/api/states/"):
			w.Write([]byte(`{"state":"on","attributes":{"rgb_color":[255,0,0],"brightness":153}}`))
		default:
			w.Write([]byte(`[]`))
		}
	}))
	defer ha.Close()

	s, srv, path := newSettingsTest(t)
	srv.Close()
	if err := writeEnvToYAML(path, EnvConfig{HA_URL: ha.URL, HA_TOKEN: "t", LED_ENTITY: "light.wohnzimmer_led_streifen",
		COLOR_CHANGE_THRESHOLD: 60, UPDATE_INTERVAL_MS: 100, LOG_LEVEL: "info", COLOR_MODE: "dominant",
		BRIGHTNESS: 255, MIN_BRIGHTNESS: 10, TRANSITION_MS: 800, MIN_CALL_INTERVAL_MS: 800}); err != nil {
		t.Fatal(err)
	}
	if err := reloadConfig(); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	s.start(ln)
	s.loop.Start()
	defer s.loop.Stop()
	if err := os.WriteFile(os.Getenv("PREVIEW_URL_FILE"), []byte(s.url), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 600; i++ {
		if _, err := os.Stat(os.Getenv("PREVIEW_STOP_FILE")); err == nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}
