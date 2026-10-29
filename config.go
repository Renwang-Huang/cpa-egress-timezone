package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const pluginID = "egress-timezone"
const pluginVersion = "2.0.0"

type Config struct {
	Mode                 string `yaml:"mode" json:"mode"`
	FixedTimezone        string `yaml:"fixed_timezone" json:"fixed_timezone"`
	CPAConfigPath        string `yaml:"cpa_config_path" json:"-"`
	ProbeURL             string `yaml:"probe_url" json:"-"`
	GeoIPURL             string `yaml:"geoip_url" json:"-"`
	CacheTTLSeconds      int    `yaml:"cache_ttl_seconds" json:"cache_ttl_seconds"`
	GeoIPCacheTTLSeconds int    `yaml:"geoip_cache_ttl_seconds" json:"geoip_cache_ttl_seconds"`
	RetrySeconds         int    `yaml:"retry_seconds" json:"retry_seconds"`
	LookupTimeoutMS      int    `yaml:"lookup_timeout_ms" json:"lookup_timeout_ms"`
	FallbackTimezone     string `yaml:"fallback_timezone" json:"fallback_timezone,omitempty"`
	AllowHTTP            bool   `yaml:"allow_http" json:"allow_http"`
	AddressFamily        string `yaml:"address_family" json:"address_family"`
	MaxCachedRoutes      int    `yaml:"max_cached_routes" json:"max_cached_routes"`
}

func defaultConfig() Config {
	return Config{Mode: "auto", FixedTimezone: "America/Los_Angeles", GeoIPURL: "https://ipwho.is/{ip}", CacheTTLSeconds: 60, GeoIPCacheTTLSeconds: 21600, RetrySeconds: 10, LookupTimeoutMS: 4000, AddressFamily: "auto", MaxCachedRoutes: 128}
}

func parseConfig(raw []byte) (Config, error) {
	c := defaultConfig()
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return c, errors.New("invalid plugin YAML")
	}
	if c.Mode != "auto" && c.Mode != "fixed" {
		return c, errors.New("mode must be auto or fixed")
	}
	for _, zone := range []string{c.FixedTimezone, c.FallbackTimezone} {
		if zone != "" {
			if _, err := loadZone(zone); err != nil {
				return c, err
			}
		}
	}
	if c.Mode == "fixed" && c.FixedTimezone == "" {
		return c, errors.New("fixed_timezone is required in fixed mode")
	}
	if c.CacheTTLSeconds < 1 || c.CacheTTLSeconds > 86400 || c.GeoIPCacheTTLSeconds < 1 || c.GeoIPCacheTTLSeconds > 604800 || c.RetrySeconds < 1 || c.RetrySeconds > 3600 || c.LookupTimeoutMS < 100 || c.LookupTimeoutMS > 15000 || c.MaxCachedRoutes < 1 || c.MaxCachedRoutes > 4096 {
		return c, errors.New("cache, retry, timeout, or capacity is outside its documented range")
	}
	if c.AddressFamily != "auto" && c.AddressFamily != "ipv4" && c.AddressFamily != "ipv6" {
		return c, errors.New("address_family must be auto, ipv4, or ipv6")
	}
	if c.ProbeURL != "" {
		if err := validateEndpoint(c.ProbeURL, c.AllowHTTP); err != nil {
			return c, fmt.Errorf("probe_url: %w", err)
		}
	}
	if strings.Count(c.GeoIPURL, "{ip}") != 1 {
		return c, errors.New("geoip_url must contain exactly one {ip} placeholder")
	}
	if err := validateEndpoint(strings.ReplaceAll(c.GeoIPURL, "{ip}", "8.8.8.8"), c.AllowHTTP); err != nil {
		return c, fmt.Errorf("geoip_url: %w", err)
	}
	if c.CPAConfigPath == "" {
		c.CPAConfigPath = discoverConfigPath(os.Args)
	}
	return c, nil
}

func discoverConfigPath(args []string) string {
	for i, arg := range args {
		for _, flag := range []string{"--config", "-config"} {
			if arg == flag && i+1 < len(args) {
				return args[i+1]
			}
			if strings.HasPrefix(arg, flag+"=") {
				return strings.TrimPrefix(arg, flag+"=")
			}
		}
	}
	return filepath.Join(".", "config.yaml")
}

func loadZone(name string) (*time.Location, error) {
	if name == "" || name == "Local" {
		return nil, errors.New("an explicit IANA timezone is required")
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, errors.New("timezone is not present in the embedded IANA database")
	}
	return loc, nil
}

func validateEndpoint(raw string, allowHTTP bool) error {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "https" && !(allowHTTP && u.Scheme == "http")) {
		return errors.New("endpoint must use HTTPS without URL credentials or a fragment; HTTP requires allow_http")
	}
	return nil
}
