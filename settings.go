package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kbinani/screenshot"
	"gopkg.in/yaml.v3"
)

//go:embed settings.html
var settingsHTML []byte

// settingsServer serves the local settings page. It listens on 127.0.0.1 only
// and every request needs the per-run access token, so other programs and
// web pages can't drive it.
type settingsServer struct {
	loop   *syncer
	reload func() error // re-read the config file and apply it

	startOnce sync.Once
	url       string
	startErr  error
	token     string
	host      string // "127.0.0.1:port"

	saveMu sync.Mutex
}

// URL starts the server on first use and returns the address to open.
func (s *settingsServer) URL() (string, error) {
	s.startOnce.Do(func() {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			s.startErr = err
			return
		}
		s.start(ln)
	})
	return s.url, s.startErr
}

// start serves on ln (separate from URL so tests can supply a listener).
func (s *settingsServer) start(ln net.Listener) {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	s.token = hex.EncodeToString(buf)
	s.host = ln.Addr().String()
	s.url = "http://" + s.host + "/?t=" + s.token

	srv := &http.Server{
		Handler:           s.handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Errorf("Settings server stopped: %v", err)
		}
	}()
	logger.Infof("Settings page listening on http://%s/", s.host)
}

func (s *settingsServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/state", s.api(http.MethodGet, s.handleState))
	mux.HandleFunc("/api/config", s.api(http.MethodPost, s.handleSave))
	mux.HandleFunc("/api/sync", s.api(http.MethodPost, s.handleSync))
	mux.HandleFunc("/api/light", s.api(http.MethodPost, s.handleLight))
	mux.HandleFunc("/api/discover", s.api(http.MethodPost, s.handleDiscover))
	return mux
}

func (s *settingsServer) tokenOK(got string) bool {
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) == 1
}

// hostOK rejects requests whose Host header isn't our own address
// (protects against DNS-rebinding).
func (s *settingsServer) hostOK(r *http.Request) bool {
	return r.Host == s.host
}

func (s *settingsServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if !s.hostOK(r) || !s.tokenOK(r.URL.Query().Get("t")) {
		http.Error(w, "forbidden - open the page from the tray menu (Settings)", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'; img-src 'self' data:")
	_, _ = w.Write(settingsHTML)
}

type apiHandler func(w http.ResponseWriter, r *http.Request)

func (s *settingsServer) api(method string, h apiHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.hostOK(r) || !s.tokenOK(r.Header.Get("X-Token")) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		if r.Method != method {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+s.host {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "bad origin"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
		h(w, r)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func errJSON(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

type displayInfo struct {
	Index  int `json:"index"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

func listDisplays() []displayInfo {
	n := screenshot.NumActiveDisplays()
	out := make([]displayInfo, 0, n)
	for i := 0; i < n; i++ {
		b := screenshot.GetDisplayBounds(i)
		out = append(out, displayInfo{Index: i, Width: b.Dx(), Height: b.Dy()})
	}
	return out
}

// GET /api/state
func (s *settingsServer) handleState(w http.ResponseWriter, r *http.Request) {
	cfg := currentConfig()
	env := cfg.Env
	tokenSet := env.HA_TOKEN != ""
	env.HA_TOKEN = "" // never send the token to the page
	snap := s.loop.Snapshot()
	writeJSON(w, http.StatusOK, map[string]any{
		"config":      env,
		"token_set":   tokenSet,
		"running":     snap.Running,
		"status":      snap.Status,
		"detected":    snap.Detected,
		"sent":        snap.Sent,
		"displays":    listDisplays(),
		"version":     version,
		"config_path": configPath,
	})
}

// POST /api/config - validate, write the YAML file, apply.
func (s *settingsServer) handleSave(w http.ResponseWriter, r *http.Request) {
	var in EnvConfig
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		errJSON(w, http.StatusBadRequest, fmt.Errorf("invalid request: %w", err))
		return
	}

	s.saveMu.Lock()
	defer s.saveMu.Unlock()

	fileCfg, err := LoadConfig(configPath)
	if err != nil {
		errJSON(w, http.StatusInternalServerError, err)
		return
	}
	if strings.TrimSpace(in.HA_TOKEN) == "" {
		in.HA_TOKEN = fileCfg.Env.HA_TOKEN // blank means "keep the current token"
	}
	cand := &Config{Env: in}
	cand.applyDefaults()

	// Validate what will actually run (environment overrides included).
	check := *cand
	check.applyEnvOverrides()
	if err := check.Validate(); err != nil {
		errJSON(w, http.StatusBadRequest, err)
		return
	}
	if err := writeEnvToYAML(configPath, cand.Env); err != nil {
		errJSON(w, http.StatusInternalServerError, fmt.Errorf("could not write %s: %w", configPath, err))
		return
	}
	if err := s.reload(); err != nil {
		errJSON(w, http.StatusInternalServerError, err)
		return
	}
	logger.Info("Settings saved from the settings page")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// POST /api/sync {"action":"start"|"stop"}
func (s *settingsServer) handleSync(w http.ResponseWriter, r *http.Request) {
	var in struct{ Action string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		errJSON(w, http.StatusBadRequest, err)
		return
	}
	switch in.Action {
	case "start":
		s.loop.Start()
	case "stop":
		s.loop.Stop()
	default:
		errJSON(w, http.StatusBadRequest, errors.New("action must be start or stop"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// POST /api/light {"action":"on"|"off"}
func (s *settingsServer) handleLight(w http.ResponseWriter, r *http.Request) {
	var in struct{ Action string }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		errJSON(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	c := newClientFromConfig()
	var err error
	switch in.Action {
	case "on":
		err = c.TurnOn(ctx)
	case "off":
		err = c.TurnOff(ctx)
	default:
		errJSON(w, http.StatusBadRequest, errors.New("action must be on or off"))
		return
	}
	if err != nil {
		errJSON(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type lightInfo struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
}

// POST /api/discover - tests the connection and lists the light entities.
//
// It only ever talks to the Home Assistant server that is already saved in the
// configuration; the request carries no URL or token. Accepting a URL from the
// page would let any caller make the app (and its saved token) contact an
// arbitrary host.
func (s *settingsServer) handleDiscover(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	c := newClientFromConfig()
	data, err := c.do(ctx, http.MethodGet, "/api/states", nil)
	if err != nil {
		errJSON(w, http.StatusBadGateway, err)
		return
	}
	var states []struct {
		EntityID   string `json:"entity_id"`
		State      string `json:"state"`
		Attributes struct {
			FriendlyName string `json:"friendly_name"`
		} `json:"attributes"`
	}
	if err := json.Unmarshal(data, &states); err != nil {
		errJSON(w, http.StatusBadGateway, err)
		return
	}
	lights := []lightInfo{}
	for _, st := range states {
		if strings.HasPrefix(st.EntityID, "light.") {
			lights = append(lights, lightInfo{st.EntityID, st.Attributes.FriendlyName, st.State})
		}
	}
	sort.Slice(lights, func(i, j int) bool { return lights[i].ID < lights[j].ID })
	writeJSON(w, http.StatusOK, map[string]any{"lights": lights})
}

// ---------------------------------------------------------------------------
// YAML writing

// writeEnvToYAML updates the env: section of the file in place, keeping the
// user's comments, key order and any unknown keys. Missing keys are appended.
func writeEnvToYAML(path string, e EnvConfig) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return errors.New("unexpected YAML structure (expected a mapping at the top level)")
	}
	root := doc.Content[0]
	env := mappingValue(root, "env")
	if env == nil {
		env = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "env"}, env)
	}
	if env.Kind != yaml.MappingNode {
		return errors.New(`"env" in the YAML file is not a mapping`)
	}

	str := func(v string) *yaml.Node {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v, Style: yaml.DoubleQuotedStyle}
	}
	num := func(v int) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(v)} }
	boolean := func(v bool) *yaml.Node {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(v)}
	}
	float := func(v float64) *yaml.Node {
		s := strconv.FormatFloat(v, 'f', -1, 64)
		if !strings.ContainsAny(s, ".") {
			s += ".0"
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: s}
	}

	setKey(env, "HA_URL", str(e.HA_URL))
	setKey(env, "HA_TOKEN", str(e.HA_TOKEN))
	setKey(env, "LED_ENTITY", str(e.LED_ENTITY))
	setKey(env, "EXPORT_JSON", boolean(e.EXPORT_JSON))
	setKey(env, "EXPORT_SCREENSHOT", boolean(e.EXPORT_SCREENSHOT))
	setKey(env, "COLOR_CHANGE_THRESHOLD", float(e.COLOR_CHANGE_THRESHOLD))
	setKey(env, "UPDATE_INTERVAL_MS", num(e.UPDATE_INTERVAL_MS))
	setKey(env, "LOG_LEVEL", str(e.LOG_LEVEL))
	setKey(env, "DISPLAY_INDEX", num(e.DISPLAY_INDEX))
	setKey(env, "COLOR_MODE", str(e.COLOR_MODE))
	setKey(env, "BRIGHTNESS", num(e.BRIGHTNESS))
	setKey(env, "AUTO_BRIGHTNESS", boolean(e.AUTO_BRIGHTNESS))
	setKey(env, "MIN_BRIGHTNESS", num(e.MIN_BRIGHTNESS))
	setKey(env, "TRANSITION_MS", num(e.TRANSITION_MS))
	setKey(env, "MIN_CALL_INTERVAL_MS", num(e.MIN_CALL_INTERVAL_MS))
	setKey(env, "KEEP_COLOR_ON_STOP", boolean(e.KEEP_COLOR_ON_STOP))

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}

	// Keep a backup of the previous file, then replace atomically.
	_ = os.WriteFile(path+".bak", data, 0o600)
	tmp, err := os.CreateTemp(filepath.Dir(path), ".ledsync-*.yaml")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

func mappingValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// setKey replaces the value of key (keeping the comments attached to it) or
// appends the key when it doesn't exist yet.
func setKey(m *yaml.Node, key string, val *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			old := m.Content[i+1]
			val.HeadComment, val.LineComment, val.FootComment = old.HeadComment, old.LineComment, old.FootComment
			m.Content[i+1] = val
			return
		}
	}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, val)
}
