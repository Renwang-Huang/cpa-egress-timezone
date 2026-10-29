package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This test is opt-in. It makes trace/GeoIP requests, never a model API request.
// Run in the same network and filesystem namespace as CPA, with read-only
// configuration/auth mounts. Account names, credentials and IPs are not printed.
func TestLiveProbeOptIn(t *testing.T) {
	path := os.Getenv("CPA_EGRESS_LIVE_CONFIG")
	if path == "" {
		t.Skip("set CPA_EGRESS_LIVE_CONFIG for an explicit live trace/GeoIP probe")
	}
	raw, err := readLimitedFile(path, 4<<20)
	if err != nil {
		t.Fatal("live CPA configuration is not readable")
	}
	host, err := decodeHostConfig(raw)
	if err != nil {
		t.Fatal("live CPA configuration is invalid")
	}
	id := os.Getenv("CPA_EGRESS_LIVE_AUTH_ID")
	if id == "" {
		dir := host.AuthDir
		if strings.HasPrefix(dir, "~/") {
			home, e := os.UserHomeDir()
			if e != nil {
				t.Fatal("home is unavailable")
			}
			dir = filepath.Join(home, dir[2:])
		}
		files, e := os.ReadDir(dir)
		if e != nil {
			t.Fatal("live auth directory is not readable; specify a configured auth ID explicitly")
		}
		for _, file := range files {
			if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
				continue
			}
			body, e := readLimitedFile(filepath.Join(dir, file.Name()), 1<<20)
			if e != nil {
				continue
			}
			var entry struct {
				Type     string `json:"type"`
				Disabled bool   `json:"disabled"`
			}
			if json.Unmarshal(body, &entry) == nil && entry.Type == "codex" && !entry.Disabled {
				id = file.Name()
				break
			}
		}
	}
	if id == "" {
		t.Fatal("no supported live credential route was found")
	}
	c := defaultConfig()
	c.CPAConfigPath = path
	route, err := resolveRoute(c, transformRequest{Metadata: map[string]any{"selected_auth_id": id}})
	if err != nil {
		t.Fatal(err)
	}
	observation, err := newResolver(c).Resolve(route)
	if err != nil {
		t.Fatal(err)
	}
	if observation.IP == "" || observation.Timezone == "" {
		t.Fatal("live observation is incomplete")
	}
	fmt.Printf("LIVE probe_source=%s timezone=%s public_ip_observed=true (IP and credential identity omitted)\n", observation.Source, observation.Timezone)
}
