package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// haClient talks to the Home Assistant REST API.
type haClient struct {
	baseURL string
	token   string
	entity  string
	http    *http.Client
}

func newHAClient(baseURL, token, entity string) *haClient {
	return &haClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		entity:  entity,
		http:    &http.Client{Timeout: 3 * time.Second},
	}
}

// haState is the subset of an entity's state we care about.
type haState struct {
	State      string `json:"state"`
	Attributes struct {
		RGBColor   []int `json:"rgb_color"`
		Brightness *int  `json:"brightness"`
	} `json:"attributes"`
}

func (c *haClient) do(ctx context.Context, method, path string, payload any) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		snippet := strings.TrimSpace(string(data))
		if len(snippet) > 200 {
			snippet = snippet[:200] + "..."
		}
		return nil, fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, snippet)
	}
	return data, nil
}

// Ping checks that the URL is reachable and the token is accepted.
func (c *haClient) Ping(ctx context.Context) error {
	_, err := c.do(ctx, http.MethodGet, "/api/", nil)
	return err
}

// State returns the current state of the configured entity.
func (c *haClient) State(ctx context.Context) (*haState, error) {
	data, err := c.do(ctx, http.MethodGet, "/api/states/"+c.entity, nil)
	if err != nil {
		return nil, err
	}
	var st haState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func (c *haClient) callLight(ctx context.Context, service string, extra map[string]any) error {
	payload := map[string]any{"entity_id": c.entity}
	for k, v := range extra {
		payload[k] = v
	}
	_, err := c.do(ctx, http.MethodPost, "/api/services/light/"+service, payload)
	return err
}

// SetColor sets the LED color and brightness. transitionMS <= 0 means "instant".
func (c *haClient) SetColor(ctx context.Context, col RGB, brightness, transitionMS int) error {
	extra := map[string]any{
		"rgb_color":  []int{int(col.R), int(col.G), int(col.B)},
		"brightness": brightness,
	}
	if transitionMS > 0 {
		extra["transition"] = float64(transitionMS) / 1000
	}
	return c.callLight(ctx, "turn_on", extra)
}

func (c *haClient) TurnOn(ctx context.Context) error  { return c.callLight(ctx, "turn_on", nil) }
func (c *haClient) TurnOff(ctx context.Context) error { return c.callLight(ctx, "turn_off", nil) }

// Restore puts the LED back into a previously saved state.
func (c *haClient) Restore(ctx context.Context, st *haState) error {
	if st == nil {
		return nil
	}
	if st.State == "off" {
		return c.TurnOff(ctx)
	}
	extra := map[string]any{}
	if len(st.Attributes.RGBColor) == 3 {
		extra["rgb_color"] = st.Attributes.RGBColor
	}
	if st.Attributes.Brightness != nil {
		extra["brightness"] = *st.Attributes.Brightness
	}
	return c.callLight(ctx, "turn_on", extra)
}
