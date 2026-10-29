package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type transformRequest struct {
	ToFormat string
	Body     []byte
	Headers  map[string][]string
	Metadata map[string]any
}

type pluginState struct {
	config         Config
	resolver       *Resolver
	rewritten      atomic.Uint64
	skipped        atomic.Uint64
	diagnosticMu   sync.Mutex
	lastSkipReason string
}

var stateMu sync.RWMutex
var activeState = &pluginState{config: defaultConfig(), resolver: newResolver(defaultConfig())}

func currentState() *pluginState {
	stateMu.RLock()
	defer stateMu.RUnlock()
	return activeState
}

func handleMethod(method string, raw []byte) (any, error) {
	switch method {
	case "plugin.register", "plugin.reconfigure":
		var lifecycle struct {
			ConfigYAML []byte `json:"config_yaml"`
		}
		if len(raw) > 0 && json.Unmarshal(raw, &lifecycle) != nil {
			return nil, errors.New("invalid lifecycle request")
		}
		c, err := parseConfig(lifecycle.ConfigYAML)
		if err != nil {
			return nil, err
		}
		next := &pluginState{config: c, resolver: newResolver(c)}
		stateMu.Lock()
		activeState = next
		stateMu.Unlock()
		return registration(), nil
	case "plugin.quiesce", "plugin.shutdown", "request.intercept_before":
		return map[string]any{}, nil
	case "management.register":
		return map[string]any{"routes": []any{map[string]any{"Method": "GET", "Path": "/plugins/" + pluginID + "/status"}}}, nil
	case "management.handle":
		s := currentState()
		s.diagnosticMu.Lock()
		reason := s.lastSkipReason
		s.diagnosticMu.Unlock()
		body, _ := json.Marshal(map[string]any{"plugin": pluginID, "version": pluginVersion, "config": s.config, "observations": s.resolver.Snapshot(), "rewritten_calls": s.rewritten.Load(), "skipped_calls": s.skipped.Load(), "last_skip_reason": reason, "measurement": "cached probe observation; rotating or destination-dependent exits can differ"})
		return map[string]any{"StatusCode": 200, "Headers": map[string][]string{"Content-Type": {"application/json"}, "Cache-Control": {"no-store"}}, "Body": body}, nil
	case "request.normalize", "request.intercept_after":
		var req transformRequest
		if json.Unmarshal(raw, &req) != nil {
			return nil, errors.New("invalid request transform envelope")
		}
		result := map[string]any{}
		if method == "request.normalize" {
			result["Body"] = req.Body
		}
		if !isOpenAIFormat(req.ToFormat) {
			return result, nil
		}
		s := currentState()
		zone := s.config.FixedTimezone
		if s.config.Mode == "auto" {
			// The translation normalizer has no selected credential. Leave it
			// untouched and rewrite in the after-auth hook, including WS frames.
			if method == "request.normalize" {
				return result, nil
			}
			route, err := resolveRoute(s.config, req)
			if err == nil {
				var observation Observation
				observation, err = s.resolver.Resolve(route)
				if err == nil {
					zone = observation.Timezone
				}
			}
			if err != nil {
				zone = s.config.FallbackTimezone
				s.diagnosticMu.Lock()
				s.lastSkipReason = err.Error()
				s.diagnosticMu.Unlock()
			}
			if zone == "" {
				s.skipped.Add(1)
				return result, nil
			}
		}
		body, err := normalizeBodyInZone(req.Body, time.Now(), zone)
		if err != nil {
			// A malformed or unsupported payload must not disrupt the model request.
			s.skipped.Add(1)
			return result, nil
		}
		result["Body"] = body
		headers := map[string][]string{}
		if method == "request.intercept_after" {
			for name := range req.Headers {
				key := strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(name))
				switch key {
				case "timezone", "xtimezone", "xclienttimezone", "clienttimezone", "openaitimezone":
					headers[name] = []string{zone}
				}
			}
			if len(headers) > 0 {
				result["Headers"] = headers
			}
		}
		if !bytes.Equal(body, req.Body) || len(headers) > 0 {
			s.rewritten.Add(1)
		}
		return result, nil
	default:
		return nil, errors.New("unsupported plugin method")
	}
}

func isOpenAIFormat(format string) bool {
	switch strings.ToLower(format) {
	case "codex", "openai", "openai-response":
		return true
	default:
		return false
	}
}

func registration() map[string]any {
	field := func(name, kind, description string, value any) map[string]any {
		return map[string]any{"Name": name, "Type": kind, "Default": value, "Description": description}
	}
	d := defaultConfig()
	mode := field("mode", "enum", "Automatic egress-IP timezone or an explicitly fixed IANA timezone.", "auto")
	mode["EnumValues"] = []string{"auto", "fixed"}
	family := field("address_family", "enum", "Address family for probes. Must match the model request's egress family.", "auto")
	family["EnumValues"] = []string{"auto", "ipv4", "ipv6"}
	return map[string]any{
		"schema_version": 2,
		"metadata": map[string]any{
			"Name": pluginID, "Version": pluginVersion, "Author": "Renwang-Huang",
			"GitHubRepository": "https://github.com/Renwang-Huang/cpa-egress-timezone",
			"ConfigFields": []any{
				mode,
				field("cpa_config_path", "string", "CPA config path inside its runtime. Empty discovers --config or ./config.yaml.", ""),
				field("probe_url", "string", "Empty probes the selected upstream origin at /cdn-cgi/trace; custom endpoints must follow the same route.", ""),
				field("geoip_url", "string", "IP-to-timezone JSON endpoint with {ip}; only the observed public IP is submitted.", d.GeoIPURL),
				field("cache_ttl_seconds", "integer", "Seconds an observation may be used (1-86400); expired observations are never reused.", d.CacheTTLSeconds),
				field("geoip_cache_ttl_seconds", "integer", "Cache IP-to-timezone mappings separately (1-604800 seconds); exits are still reprobed at cache_ttl_seconds.", d.GeoIPCacheTTLSeconds),
				field("retry_seconds", "integer", "Failure cooldown in seconds (1-3600).", d.RetrySeconds),
				field("lookup_timeout_ms", "integer", "Total probe and GeoIP budget in milliseconds (100-15000).", d.LookupTimeoutMS),
				field("fallback_timezone", "string", "Empty preserves the request on lookup failure; set only for an explicit fixed fallback.", ""),
				field("fixed_timezone", "string", "IANA timezone used only in fixed mode.", d.FixedTimezone),
				field("allow_http", "boolean", "Allow plaintext HTTP probes and origins, for isolated testing only.", false),
				family,
				field("max_cached_routes", "integer", "Bound on cached credential routes (1-4096).", d.MaxCachedRoutes),
			},
		},
		"capabilities": map[string]bool{"request_normalizer": true, "request_interceptor": true, "management_api": true},
	}
}
