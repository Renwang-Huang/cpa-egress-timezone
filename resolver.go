package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"
)

type Observation struct {
	Route      string    `json:"route"`
	IP         string    `json:"ip,omitempty"`
	Timezone   string    `json:"timezone,omitempty"`
	Source     string    `json:"source"`
	ObservedAt time.Time `json:"observed_at,omitempty"`
	ExpiresAt  time.Time `json:"expires_at,omitempty"`
	LastError  string    `json:"last_error,omitempty"`
}

type cacheEntry struct {
	Observation
	retryAt time.Time
	busy    chan struct{}
}

type geoEntry struct {
	zone      string
	expiresAt time.Time
	busy      chan struct{}
}

type Resolver struct {
	config Config
	mu     sync.Mutex
	cache  map[string]*cacheEntry
	geo    map[netip.Addr]*geoEntry
	now    func() time.Time
	lookup func(context.Context, Route) (Observation, error)
}

func newResolver(c Config) *Resolver {
	r := &Resolver{config: c, cache: make(map[string]*cacheEntry), geo: make(map[netip.Addr]*geoEntry), now: time.Now}
	r.lookup = r.lookupRoute
	return r
}

// Resolve never reuses an expired observation. Concurrent misses for a route
// share one bounded lookup. No provider credentials or request content is sent.
func (r *Resolver) Resolve(route Route) (Observation, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(r.config.LookupTimeoutMS)*time.Millisecond)
	defer cancel()
	key := route.key()
	for {
		r.mu.Lock()
		now := r.now()
		e := r.cache[key]
		if e != nil && e.busy == nil && e.Timezone != "" && now.Before(e.ExpiresAt) {
			result := e.Observation
			r.mu.Unlock()
			return result, nil
		}
		if e != nil && e.busy != nil {
			done := e.busy
			r.mu.Unlock()
			select {
			case <-ctx.Done():
				return Observation{}, errors.New("lookup budget exceeded")
			case <-done:
				continue
			}
		}
		if e != nil && now.Before(e.retryAt) {
			r.mu.Unlock()
			return Observation{}, errors.New("lookup is in retry cooldown")
		}
		if e == nil {
			if len(r.cache) >= r.config.MaxCachedRoutes {
				var oldestKey string
				var oldest time.Time
				for k, candidate := range r.cache {
					if candidate.busy == nil && (oldestKey == "" || candidate.ObservedAt.Before(oldest)) {
						oldestKey, oldest = k, candidate.ObservedAt
					}
				}
				if oldestKey == "" {
					r.mu.Unlock()
					return Observation{}, errors.New("all route cache slots are busy")
				}
				delete(r.cache, oldestKey)
			}
			e = &cacheEntry{}
			r.cache[key] = e
		}
		e.busy = make(chan struct{})
		done := e.busy
		r.mu.Unlock()

		observation, err := r.lookup(ctx, route)
		r.mu.Lock()
		now = r.now()
		if err == nil {
			observation.Route = key
			observation.ObservedAt = now
			observation.ExpiresAt = now.Add(time.Duration(r.config.CacheTTLSeconds) * time.Second)
			e.Observation = observation
			e.retryAt = time.Time{}
		} else {
			// Keep the prior observation visible as historical diagnostics only.
			e.Route = key
			e.LastError = err.Error()
			e.retryAt = now.Add(time.Duration(r.config.RetrySeconds) * time.Second)
		}
		e.busy = nil
		close(done)
		r.mu.Unlock()
		return observation, err
	}
}

func (r *Resolver) Snapshot() []Observation {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Observation, 0, len(r.cache))
	for _, e := range r.cache {
		out = append(out, e.Observation)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Route < out[j].Route })
	return out
}

func (r *Resolver) lookupRoute(ctx context.Context, route Route) (Observation, error) {
	client, cleanup, err := newProbeClient(route.ProxyURL, r.config.AddressFamily)
	if err != nil {
		return Observation{}, err
	}
	defer cleanup()
	probeURL := r.config.ProbeURL
	source := "upstream-origin-trace"
	if probeURL == "" {
		probeURL = route.Origin + "/cdn-cgi/trace"
	} else {
		source = "configured-probe"
	}
	body, err := fetchSmall(ctx, client, probeURL)
	if err != nil {
		return Observation{}, errors.New("egress probe failed")
	}
	ip, err := parseObservedIP(body)
	if err != nil {
		return Observation{}, err
	}
	zone, err := r.lookupZone(ctx, client, ip)
	if err != nil {
		return Observation{}, err
	}
	return Observation{IP: ip.String(), Timezone: zone, Source: source}, nil
}

// Cache geography independently of the short route observation. The exit is
// still reprobed each route TTL; an unchanged IP needs no new GeoIP request.
func (r *Resolver) lookupZone(ctx context.Context, client *http.Client, ip netip.Addr) (string, error) {
	for {
		r.mu.Lock()
		e := r.geo[ip]
		if e != nil && e.busy == nil && e.zone != "" && r.now().Before(e.expiresAt) {
			zone := e.zone
			r.mu.Unlock()
			return zone, nil
		}
		if e != nil && e.busy != nil {
			done := e.busy
			r.mu.Unlock()
			select {
			case <-ctx.Done():
				return "", errors.New("GeoIP lookup budget exceeded")
			case <-done:
				continue
			}
		}
		if e == nil {
			if len(r.geo) >= r.config.MaxCachedRoutes {
				var oldestIP netip.Addr
				var oldest time.Time
				for candidateIP, candidate := range r.geo {
					if candidate.busy == nil && (!oldestIP.IsValid() || candidate.expiresAt.Before(oldest)) {
						oldestIP, oldest = candidateIP, candidate.expiresAt
					}
				}
				if !oldestIP.IsValid() {
					r.mu.Unlock()
					return "", errors.New("all GeoIP cache slots are busy")
				}
				delete(r.geo, oldestIP)
			}
			e = &geoEntry{}
			r.geo[ip] = e
		}
		e.busy = make(chan struct{})
		done := e.busy
		r.mu.Unlock()
		geoURL := strings.ReplaceAll(r.config.GeoIPURL, "{ip}", url.PathEscape(ip.String()))
		body, err := fetchSmall(ctx, client, geoURL)
		var zone string
		if err == nil {
			zone, err = parseGeoIP(body, ip)
		} else {
			err = errors.New("GeoIP lookup failed")
		}
		r.mu.Lock()
		if err == nil {
			e.zone = zone
			e.expiresAt = r.now().Add(time.Duration(r.config.GeoIPCacheTTLSeconds) * time.Second)
		}
		e.busy = nil
		close(done)
		r.mu.Unlock()
		return zone, err
	}
}

func fetchSmall(ctx context.Context, client *http.Client, target string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "cpa-egress-timezone/"+pluginVersion)
	req.Header.Set("Accept", "application/json, text/plain")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("probe returned non-200 status")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
	if len(body) > 64<<10 {
		return nil, errors.New("probe response exceeds 64 KiB")
	}
	return body, err
}

func parseObservedIP(body []byte) (netip.Addr, error) {
	var raw string
	var data struct {
		IP string `json:"ip"`
	}
	if json.Unmarshal(body, &data) == nil {
		raw = data.IP
	} else {
		s := bufio.NewScanner(strings.NewReader(string(body)))
		for s.Scan() {
			if strings.HasPrefix(s.Text(), "ip=") {
				if raw != "" {
					return netip.Addr{}, errors.New("probe returned multiple IP addresses")
				}
				raw = strings.TrimPrefix(s.Text(), "ip=")
			}
		}
	}
	ip, err := netip.ParseAddr(strings.TrimSpace(raw))
	if err != nil || !publicIP(ip.Unmap()) {
		return netip.Addr{}, errors.New("probe did not return a public IP address")
	}
	return ip.Unmap(), nil
}

func publicIP(ip netip.Addr) bool {
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, reserved := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32"} {
		if netip.MustParsePrefix(reserved).Contains(ip) {
			return false
		}
	}
	return true
}

func parseGeoIP(body []byte, ip netip.Addr) (string, error) {
	var data struct {
		IP       string          `json:"ip"`
		Success  *bool           `json:"success"`
		Timezone json.RawMessage `json:"timezone"`
	}
	if json.Unmarshal(body, &data) != nil || (data.Success != nil && !*data.Success) {
		return "", errors.New("GeoIP provider rejected the lookup")
	}
	returnedIP, err := netip.ParseAddr(data.IP)
	if err != nil || returnedIP.Unmap() != ip {
		return "", errors.New("GeoIP response does not match the observed egress IP")
	}
	var zone string
	if json.Unmarshal(data.Timezone, &zone) != nil {
		var nested struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(data.Timezone, &nested) != nil {
			return "", errors.New("GeoIP response has no timezone")
		}
		zone = nested.ID
	}
	if _, err := loadZone(zone); err != nil {
		return "", err
	}
	return zone, nil
}

func newProbeClient(proxyURL, family string) (*http.Client, func(), error) {
	dialer := &net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}
	tr := &http.Transport{TLSHandshakeTimeout: 3 * time.Second, MaxIdleConns: 2}
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if family == "ipv4" {
			network = "tcp4"
		}
		if family == "ipv6" {
			network = "tcp6"
		}
		return dialer.DialContext(ctx, network, addr)
	}
	switch strings.TrimSpace(proxyURL) {
	case "":
		tr.Proxy = http.ProxyFromEnvironment
	case "direct", "direct://", "none":
		tr.Proxy = nil
	default:
		if family != "auto" {
			return nil, func() {}, errors.New("address_family must be auto with an explicit proxy; its exit family is controlled by the proxy")
		}
		u, err := url.Parse(proxyURL)
		if err != nil || u.Hostname() == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
			return nil, func() {}, errors.New("invalid outbound proxy configuration")
		}
		switch u.Scheme {
		case "http", "https":
			tr.Proxy = http.ProxyURL(u)
		case "socks5", "socks5h":
			var auth *proxy.Auth
			if u.User != nil {
				password, _ := u.User.Password()
				auth = &proxy.Auth{User: u.User.Username(), Password: password}
			}
			d, err := proxy.SOCKS5("tcp", u.Host, auth, dialer)
			if err != nil {
				return nil, func() {}, errors.New("cannot initialize SOCKS proxy")
			}
			cd, ok := d.(proxy.ContextDialer)
			if !ok {
				return nil, func() {}, errors.New("SOCKS proxy lacks cancellation support")
			}
			tr.DialContext = cd.DialContext
		default:
			return nil, func() {}, errors.New("unsupported outbound proxy scheme")
		}
	}
	client := &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return client, tr.CloseIdleConnections, nil
}
