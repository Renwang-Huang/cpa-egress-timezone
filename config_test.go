package main

import "testing"

func TestConfigValidation(t *testing.T) {
	for _, raw := range []string{
		"mode: wrong", "mode: fixed\nfixed_timezone: ''", "fixed_timezone: Local",
		"fallback_timezone: Invalid/Zone", "cache_ttl_seconds: 0", "lookup_timeout_ms: 16000",
		"retry_seconds: -1", "max_cached_routes: 0", "address_family: other",
		"probe_url: http://example.com", "probe_url: https://user:pass@example.com",
		"geoip_url: https://example.com/", "geoip_url: https://example.com/{ip}/{ip}",
		"mode: [bad]",
	} {
		if _, err := parseConfig([]byte(raw)); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
	c, err := parseConfig(nil)
	if err != nil || c.Mode != "auto" || c.FallbackTimezone != "" {
		t.Fatalf("defaults: %+v %v", c, err)
	}
	if _, err := parseConfig([]byte("allow_http: true\nprobe_url: http://127.0.0.1/trace")); err != nil {
		t.Fatal(err)
	}
}

func TestConfigPathDiscovery(t *testing.T) {
	for _, args := range [][]string{{"cpa", "--config", "/data/cpa.yaml"}, {"cpa", "-config=/data/cpa.yaml"}, {"cpa", "--config=/data/cpa.yaml"}} {
		if got := discoverConfigPath(args); got != "/data/cpa.yaml" {
			t.Fatal(got)
		}
	}
}
