package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLookupUsesSelectedProxyAndObservedIP(t *testing.T) {
	var calls atomic.Int32
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("probe sent provider identity")
		}
		switch r.URL.Host + r.URL.Path {
		case "upstream.invalid/cdn-cgi/trace":
			fmt.Fprint(w, "ip=8.8.8.8\nloc=US\n")
		case "geo.invalid/8.8.8.8":
			fmt.Fprint(w, `{"ip":"8.8.8.8","success":true,"timezone":{"id":"America/New_York"}}`)
		default:
			t.Errorf("unexpected probe: %s", r.URL)
			w.WriteHeader(404)
		}
	}))
	defer proxyServer.Close()
	c := defaultConfig()
	c.GeoIPURL = "http://geo.invalid/{ip}"
	c.AllowHTTP = true
	r := newResolver(c)
	route := Route{Origin: "http://upstream.invalid", ProxyURL: proxyServer.URL, AuthID: "test"}
	o, err := r.Resolve(route)
	if err != nil || o.IP != "8.8.8.8" || o.Timezone != "America/New_York" || calls.Load() != 2 {
		t.Fatalf("%+v %v calls=%d", o, err, calls.Load())
	}
	_, err = r.Resolve(route)
	if err != nil || calls.Load() != 2 {
		t.Fatal("cache miss", err)
	}
}

func TestCacheRefreshFailureAndConcurrency(t *testing.T) {
	c := defaultConfig()
	c.CacheTTLSeconds = 1
	c.MaxCachedRoutes = 2
	r := newResolver(c)
	var clock atomic.Int64
	clock.Store(time.Now().Unix())
	r.now = func() time.Time { return time.Unix(clock.Load(), 0) }
	var calls atomic.Int32
	var fail atomic.Bool
	r.lookup = func(ctx context.Context, route Route) (Observation, error) {
		calls.Add(1)
		select {
		case <-ctx.Done():
			return Observation{}, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
		if fail.Load() {
			return Observation{}, errors.New("test failure")
		}
		zone := "Asia/Tokyo"
		if calls.Load() > 1 {
			zone = "Europe/Berlin"
		}
		return Observation{IP: "8.8.8.8", Timezone: zone}, nil
	}
	route := Route{AuthID: "a"}
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o, e := r.Resolve(route)
			if e != nil || o.Timezone != "Asia/Tokyo" {
				t.Errorf("%+v %v", o, e)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("duplicate lookups: %d", calls.Load())
	}
	clock.Add(2)
	o, e := r.Resolve(route)
	if e != nil || o.Timezone != "Europe/Berlin" {
		t.Fatalf("exit did not refresh: %+v %v", o, e)
	}
	clock.Add(2)
	fail.Store(true)
	if _, e = r.Resolve(route); e == nil {
		t.Fatal("failed lookup used stale timezone")
	}
	if _, e = r.Resolve(route); e == nil || calls.Load() != 3 {
		t.Fatal("cooldown not enforced")
	}
	if snapshot := r.Snapshot(); len(snapshot) != 1 || snapshot[0].LastError == "" {
		t.Fatal("no failure diagnostic")
	}
	clock.Add(20)
	fail.Store(false)
	for _, id := range []string{"b", "c", "d"} {
		if _, e = r.Resolve(Route{AuthID: id}); e != nil {
			t.Fatal(e)
		}
	}
	if len(r.Snapshot()) != 2 {
		t.Fatal("unbounded cache")
	}
}

func TestLookupTimeoutAndResponseValidation(t *testing.T) {
	c := defaultConfig()
	c.LookupTimeoutMS = 100
	r := newResolver(c)
	r.lookup = func(ctx context.Context, _ Route) (Observation, error) { <-ctx.Done(); return Observation{}, ctx.Err() }
	start := time.Now()
	if _, err := r.Resolve(Route{}); err == nil || time.Since(start) > time.Second {
		t.Fatal("timeout not enforced", err)
	}
	for _, raw := range []string{"ip=127.0.0.1", "ip=10.0.0.1", "ip=100.64.0.1", "ip=192.0.2.1", "ip=::ffff:192.168.1.1", "ip=garbage", "ip=8.8.8.8\nip=1.1.1.1"} {
		if _, err := parseObservedIP([]byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	for _, raw := range []string{`{"ip":"8.8.8.8"}`, `{"ip":"8.8.8.8","timezone":"Local"}`, `{"ip":"1.1.1.1","timezone":"UTC"}`, `{"success":false}`} {
		if _, err := parseGeoIP([]byte(raw), netip.MustParseAddr("8.8.8.8")); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	if _, err := parseObservedIP([]byte(`{"ip":"2606:4700:4700::1111"}`)); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/big" {
			fmt.Fprint(w, strings.Repeat("a", 65537))
			return
		}
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/ok", 302)
			return
		}
		w.WriteHeader(403)
	}))
	defer server.Close()
	client, closeClient, err := newProbeClient("direct://", "auto")
	if err != nil {
		t.Fatal(err)
	}
	defer closeClient()
	for _, path := range []string{"/big", "/redirect", "/forbidden"} {
		if _, err := fetchSmall(context.Background(), client, server.URL+path); err == nil {
			t.Fatal("accepted", path)
		}
	}
	if _, _, err := newProbeClient("socks5://localhost:1080", "ipv4"); err == nil {
		t.Fatal("cannot force remote proxy exit family")
	}
}
