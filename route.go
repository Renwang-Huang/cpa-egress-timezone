package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Route struct {
	ProxyURL string
	Origin   string
	AuthID   string
}

func (r Route) key() string {
	sum := sha256.Sum256([]byte(r.AuthID + "\x00" + r.Origin + "\x00" + r.ProxyURL))
	return hex.EncodeToString(sum[:12])
}

type keyConfig struct {
	APIKey   string            `yaml:"api-key"`
	BaseURL  string            `yaml:"base-url"`
	ProxyURL string            `yaml:"proxy-url"`
	Prefix   string            `yaml:"prefix"`
	Headers  map[string]string `yaml:"headers"`
}

type compatConfig struct {
	Name          string      `yaml:"name"`
	BaseURL       string      `yaml:"base-url"`
	Disabled      bool        `yaml:"disabled"`
	APIKeyEntries []keyConfig `yaml:"api-key-entries"`
}

type cpaConfig struct {
	ProxyURL string         `yaml:"proxy-url"`
	AuthDir  string         `yaml:"auth-dir"`
	CodexKey []keyConfig    `yaml:"codex-api-key"`
	Compat   []compatConfig `yaml:"openai-compatibility"`
}

// resolveRoute uses the selected credential, never a downstream forwarding IP.
// CPA v8.0.47 does not expose execution-scoped proxy overrides in this hook.
// Deployments using those overrides must not enable automatic rewriting.
func resolveRoute(c Config, req transformRequest) (Route, error) {
	id, _ := req.Metadata["selected_auth_id"].(string)
	if id == "" {
		return Route{}, errors.New("selected credential is unavailable")
	}
	// An embedding host can explicitly publish this field. Unknown field values
	// are rejected instead of falling back to another route.
	requestProxy, hasRequestProxy := req.Metadata["request_proxy_url"]
	var override string
	if hasRequestProxy {
		var ok bool
		override, ok = requestProxy.(string)
		if !ok {
			return Route{}, errors.New("invalid request proxy metadata")
		}
	}
	raw, err := readLimitedFile(c.CPAConfigPath, 4<<20)
	if err != nil {
		return Route{}, errors.New("CPA configuration is unavailable; set cpa_config_path")
	}
	host, err := decodeHostConfig(raw)
	if err != nil {
		return Route{}, errors.New("CPA configuration is invalid")
	}
	routes := configuredRoutes(host)
	route, known := routes[id]
	if !known {
		// File-backed credentials use CPA's selected auth ID as their file name.
		dir := host.AuthDir
		if strings.HasPrefix(dir, "~/") {
			home, e := os.UserHomeDir()
			if e != nil {
				return Route{}, errors.New("cannot resolve auth directory")
			}
			dir = filepath.Join(home, dir[2:])
		}
		if dir == "" {
			return Route{}, errors.New("auth directory is not configured")
		}
		if filepath.Base(id) != id || id == "." || id == ".." || !strings.HasSuffix(id, ".json") {
			return Route{}, errors.New("credential route is not available in local configuration")
		}
		authRaw, e := readLimitedFile(filepath.Join(dir, id), 1<<20)
		if e != nil {
			return Route{}, errors.New("selected credential route is unavailable")
		}
		var auth struct {
			Type     string `json:"type"`
			ProxyURL string `json:"proxy_url"`
		}
		if json.Unmarshal(authRaw, &auth) != nil || auth.Type != "codex" {
			return Route{}, errors.New("selected file credential is not a supported Codex route")
		}
		// Codex file credentials use the backend origin. A base_url in their
		// opaque metadata is not a routing attribute in the supported host.
		route = Route{ProxyURL: strings.TrimSpace(auth.ProxyURL), Origin: "https://chatgpt.com"}
	}
	route.AuthID = id
	if route.ProxyURL == "" {
		route.ProxyURL = host.ProxyURL
	}
	if hasRequestProxy && override != "" {
		route.ProxyURL = override
	}
	u, err := url.Parse(route.Origin)
	if err != nil || u.Hostname() == "" || u.User != nil {
		return Route{}, errors.New("upstream origin is invalid")
	}
	route.Origin = u.Scheme + "://" + u.Host
	if err := validateEndpoint(route.Origin, c.AllowHTTP); err != nil {
		return Route{}, err
	}
	return route, nil
}

// v8 management writes grouped api-keys and nested request/OAuth sections.
// Their presence takes precedence even for empty values, matching the host.
func decodeHostConfig(raw []byte) (cpaConfig, error) {
	var host cpaConfig
	if err := yaml.Unmarshal(raw, &host); err != nil {
		return host, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil || len(doc.Content) != 1 {
		return host, errors.New("invalid host YAML")
	}
	root := doc.Content[0]
	for _, field := range []struct {
		path   []string
		target *string
	}{
		{[]string{"requests", "proxy-url"}, &host.ProxyURL},
		{[]string{"oauth", "auth-dir"}, &host.AuthDir},
	} {
		if node := yamlAt(root, field.path...); node != nil {
			*field.target = ""
			if err := node.Decode(field.target); err != nil {
				return host, err
			}
		}
	}
	if host.AuthDir == "" {
		host.AuthDir = "~/.cli-proxy-api"
	}
	if groups := yamlAt(root, "api-keys", "codex"); groups != nil {
		if groups.Kind != yaml.SequenceNode {
			return host, errors.New("invalid Codex groups")
		}
		host.CodexKey = nil
		for _, group := range groups.Content {
			var defaults keyConfig
			if err := group.Decode(&defaults); err != nil {
				return host, err
			}
			keys := yamlAt(group, "keys")
			if keys == nil || keys.Kind != yaml.SequenceNode {
				return host, errors.New("invalid Codex keys")
			}
			for _, key := range keys.Content {
				entry := defaults
				for _, field := range []struct {
					name   string
					target any
				}{
					{"api-key", &entry.APIKey}, {"proxy-url", &entry.ProxyURL},
					{"prefix", &entry.Prefix}, {"headers", &entry.Headers},
				} {
					if node := yamlAt(key, field.name); node != nil && node.Tag != "!!null" {
						if field.name == "headers" {
							entry.Headers = nil
						}
						if err := node.Decode(field.target); err != nil {
							return host, err
						}
					}
				}
				host.CodexKey = append(host.CodexKey, entry)
			}
		}
	}
	if groups := yamlAt(root, "api-keys", "openai-compatibility"); groups != nil {
		if groups.Kind != yaml.SequenceNode {
			return host, errors.New("invalid compatibility groups")
		}
		host.Compat = nil
		for _, group := range groups.Content {
			var entry compatConfig
			if err := group.Decode(&entry); err != nil {
				return host, err
			}
			keys := yamlAt(group, "keys")
			if keys == nil || keys.Kind != yaml.SequenceNode {
				return host, errors.New("invalid compatibility keys")
			}
			if err := keys.Decode(&entry.APIKeyEntries); err != nil {
				return host, err
			}
			host.Compat = append(host.Compat, entry)
		}
	}
	return host, nil
}

func yamlAt(node *yaml.Node, path ...string) *yaml.Node {
	for _, name := range path {
		if node == nil || node.Kind != yaml.MappingNode {
			return nil
		}
		var next *yaml.Node
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Value == name {
				next = node.Content[i+1]
				break
			}
		}
		node = next
	}
	return node
}

func configuredRoutes(host cpaConfig) map[string]Route {
	out := make(map[string]Route)
	counters := make(map[string]int)
	for _, entry := range host.CodexKey {
		if strings.TrimSpace(entry.APIKey) == "" && strings.TrimSpace(entry.BaseURL) == "" {
			continue
		}
		id := stableID(counters, "codex:apikey", entry.APIKey, entry.BaseURL, entry.ProxyURL, entry.Prefix, sortedHeaders(entry.Headers))
		base := strings.TrimSpace(entry.BaseURL)
		if base == "" {
			base = "https://chatgpt.com"
		}
		out[id] = Route{ProxyURL: strings.TrimSpace(entry.ProxyURL), Origin: base}
	}
	for _, compat := range host.Compat {
		if compat.Disabled {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(compat.Name))
		if name == "" {
			name = "openai-compatibility"
		}
		for _, entry := range compat.APIKeyEntries {
			id := stableID(counters, "openai-compatibility:"+name, entry.APIKey, compat.BaseURL, entry.ProxyURL)
			out[id] = Route{ProxyURL: strings.TrimSpace(entry.ProxyURL), Origin: strings.TrimSpace(compat.BaseURL)}
		}
		if len(compat.APIKeyEntries) == 0 {
			id := stableID(counters, "openai-compatibility:"+name, compat.BaseURL)
			out[id] = Route{Origin: strings.TrimSpace(compat.BaseURL)}
		}
	}
	return out
}

// This convention matches CLIProxyAPI v8.0.47's stable credential IDs.
// See THIRD_PARTY_NOTICES.md for the original MIT-licensed implementation.
func stableID(counters map[string]int, kind string, parts ...string) string {
	h := sha256.New()
	_, _ = h.Write([]byte(kind))
	for _, part := range parts {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(strings.TrimSpace(part)))
	}
	base := kind + ":" + hex.EncodeToString(h.Sum(nil))[:12]
	i := counters[base]
	counters[base]++
	if i > 0 {
		return fmt.Sprintf("%s-%d", base, i)
	}
	return base
}

func sortedHeaders(headers map[string]string) string {
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		b.WriteString(key)
		b.WriteByte(0)
		b.WriteString(headers[key])
		b.WriteByte(0)
	}
	return b.String()
}

func readLimitedFile(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(b)) > limit {
		return nil, errors.New("file exceeds size limit")
	}
	return b, err
}
