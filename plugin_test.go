package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAutoHookFailureAndDynamicZones(t *testing.T) {
	previous := currentState()
	defer func() { stateMu.Lock(); activeState = previous; stateMu.Unlock() }()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("auth-dir: "+dir), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "test.json"), []byte(`{"type":"codex"}`), 0600); err != nil {
		t.Fatal(err)
	}
	lifecycle, _ := json.Marshal(map[string]any{"config_yaml": []byte("cpa_config_path: " + path)})
	if _, err := handleMethod("plugin.register", lifecycle); err != nil {
		t.Fatal(err)
	}
	request := transformRequest{ToFormat: "codex", Body: []byte(`{"timezone":"Asia/Shanghai","input":"hello"}`), Metadata: map[string]any{"selected_auth_id": "test.json"}, Headers: map[string][]string{"User-Agent": {"identity"}, "Originator": {"codex-tui"}, "Authorization": {"Bearer fake"}}}
	raw, _ := json.Marshal(request)
	res, err := handleMethod("request.normalize", raw)
	if err != nil || string(res.(map[string]any)["Body"].([]byte)) != string(request.Body) {
		t.Fatal("rewrote before credential selection")
	}
	s := currentState()
	s.resolver.lookup = func(context.Context, Route) (Observation, error) {
		return Observation{Timezone: "Pacific/Auckland"}, nil
	}
	res, err = handleMethod("request.intercept_after", raw)
	if err != nil || !strings.Contains(string(res.(map[string]any)["Body"].([]byte)), "Pacific/Auckland") {
		t.Fatal("auto rewrite failed", res, err)
	}
	if _, ok := res.(map[string]any)["Headers"]; ok {
		t.Fatal("changed identity headers")
	}
	s.resolver = newResolver(s.config)
	s.resolver.lookup = func(context.Context, Route) (Observation, error) {
		return Observation{}, errors.New("probe unavailable")
	}
	res, err = handleMethod("request.intercept_after", raw)
	if err != nil || res.(map[string]any)["Body"] != nil {
		t.Fatal("failed lookup changed payload", res, err)
	}
	s.config.FallbackTimezone = "UTC"
	res, err = handleMethod("request.intercept_after", raw)
	if err != nil || !strings.Contains(string(res.(map[string]any)["Body"].([]byte)), "UTC") {
		t.Fatal("explicit fallback not used")
	}
	status, err := handleMethod("management.handle", nil)
	if err != nil || strings.Contains(string(status.(map[string]any)["Body"].([]byte)), dir) {
		t.Fatal("status leaked config path")
	}
}

func TestNonLATimezoneDateBoundary(t *testing.T) {
	raw := []byte(`{"input":"<environment_context><current_date>2026-12-31</current_date><timezone>America/Los_Angeles</timezone></environment_context>"}`)
	now := time.Date(2026, 12, 31, 23, 30, 0, 0, time.UTC)
	out, err := normalizeBodyInZone(raw, now, "Pacific/Auckland")
	if err != nil || !strings.Contains(string(out), "2027-01-01") || !strings.Contains(string(out), "Pacific/Auckland") {
		t.Fatalf("%s %v", out, err)
	}
}
