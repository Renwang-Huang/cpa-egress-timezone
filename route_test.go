package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCredentialRoutes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	raw := "proxy-url: http://global.invalid:8080\nauth-dir: " + dir + `
codex-api-key:
  - api-key: test-a
    base-url: https://one.example/v1
    proxy-url: socks5://proxy-a.invalid:1080
  - api-key: test-b
openai-compatibility:
  - name: Compat
    base-url: https://two.example/v1
    api-key-entries:
      - api-key: test-c
        proxy-url: direct://
  - name: NoKey
    base-url: https://three.example/v1
`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fake.json"), []byte(`{"type":"codex","proxy_url":"http://account.invalid:8888","base_url":"https://ignored.invalid"}`), 0600); err != nil {
		t.Fatal(err)
	}
	c := defaultConfig()
	c.CPAConfigPath = path
	for _, tc := range []struct{ id, origin, proxy string }{
		{stableID(map[string]int{}, "codex:apikey", "test-a", "https://one.example/v1", "socks5://proxy-a.invalid:1080", "", ""), "https://one.example", "socks5://proxy-a.invalid:1080"},
		{stableID(map[string]int{}, "codex:apikey", "test-b", "", "", "", ""), "https://chatgpt.com", "http://global.invalid:8080"},
		{stableID(map[string]int{}, "openai-compatibility:compat", "test-c", "https://two.example/v1", "direct://"), "https://two.example", "direct://"},
		{stableID(map[string]int{}, "openai-compatibility:nokey", "https://three.example/v1"), "https://three.example", "http://global.invalid:8080"},
		{"fake.json", "https://chatgpt.com", "http://account.invalid:8888"},
	} {
		route, err := resolveRoute(c, transformRequest{Metadata: map[string]any{"selected_auth_id": tc.id}})
		if err != nil || route.Origin != tc.origin || route.ProxyURL != tc.proxy {
			t.Fatalf("%s: %+v %v", tc.id, route, err)
		}
		if strings.Contains(route.key(), "test-") {
			t.Fatal("route diagnostic leaked a credential")
		}
	}
	for _, id := range []string{"", "../fake.json", "/fake.json", "unknown", "notfound.json"} {
		if _, err := resolveRoute(c, transformRequest{Metadata: map[string]any{"selected_auth_id": id}}); err == nil {
			t.Fatalf("accepted %s", id)
		}
	}
	r, err := resolveRoute(c, transformRequest{Metadata: map[string]any{"selected_auth_id": "fake.json", "request_proxy_url": "direct://"}})
	if err != nil || r.ProxyURL != "direct://" {
		t.Fatalf("override: %+v %v", r, err)
	}
}

func TestRouteIsolationAndStableDuplicates(t *testing.T) {
	counters := map[string]int{}
	a := stableID(counters, "kind", "a")
	if b := stableID(counters, "kind", "a"); b != a+"-1" {
		t.Fatal(b)
	}
	r := Route{AuthID: "one", Origin: "https://one.example", ProxyURL: "direct://"}
	s := r
	s.AuthID = "two"
	u := r
	u.ProxyURL = "http://another.invalid:80"
	if r.key() == s.key() || r.key() == u.key() {
		t.Fatal("credential routes share a cache key")
	}
}

func TestV8PrecedenceAndGroupedKeyOverrides(t *testing.T) {
	raw := []byte(`proxy-url: http://legacy.invalid
auth-dir: /legacy
codex-api-key:
  - api-key: ignored
requests:
  proxy-url: ''
oauth:
  auth-dir: /v8/auth
api-keys:
  codex:
    - base-url: https://v8.example/v1
      proxy-url: http://group.invalid:80
      prefix: group
      headers: {X-Group: inherited}
      keys:
        - api-key: test-a
          proxy-url: direct
          headers: {X-Key: overridden}
        - api-key: test-b
          proxy-url: null
          headers: null
  openai-compatibility:
    - name: Test
      base-url: https://compat.example/v1
      keys: [{api-key: test-c, proxy-url: direct}]
`)
	host, err := decodeHostConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	if host.ProxyURL != "" || host.AuthDir != "/v8/auth" || len(host.CodexKey) != 2 {
		t.Fatalf("precedence: %+v", host)
	}
	a, b := host.CodexKey[0], host.CodexKey[1]
	if a.BaseURL != "https://v8.example/v1" || a.ProxyURL != "direct" || a.Prefix != "group" || len(a.Headers) != 1 || a.Headers["X-Key"] != "overridden" {
		t.Fatalf("overrides: %+v", a)
	}
	if b.ProxyURL != "http://group.invalid:80" || len(b.Headers) != 1 || b.Headers["X-Group"] != "inherited" {
		t.Fatalf("inheritance: %+v", b)
	}
	if len(host.Compat) != 1 || len(host.Compat[0].APIKeyEntries) != 1 {
		t.Fatalf("compat: %+v", host.Compat)
	}
	empty, err := decodeHostConfig([]byte("codex-api-key: [{api-key: ignored}]\napi-keys: {codex: []}"))
	if err != nil || len(empty.CodexKey) != 0 {
		t.Fatal("empty v8 list failed to override legacy")
	}
}
