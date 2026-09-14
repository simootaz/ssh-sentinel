package geo

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/simootaz/ssh-sentinel/backend/internal/model"
)

const publicIP = "8.8.8.8"

// provider is a fake geolocation endpoint answering a fixed body and
// counting what it received.
type provider struct {
	srv    *httptest.Server
	status int
	body   string

	mu    sync.Mutex
	hits  int
	paths []string
}

func newProvider(t *testing.T, status int, body string) *provider {
	t.Helper()
	p := &provider{status: status, body: body}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.hits++
		p.paths = append(p.paths, r.URL.RequestURI())
		p.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(p.status)
		fmt.Fprint(w, p.body)
	}))
	t.Cleanup(p.srv.Close)
	return p
}

func (p *provider) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.hits
}

func (p *provider) lastPath() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.paths) == 0 {
		return ""
	}
	return p.paths[len(p.paths)-1]
}

func expectGeo(t *testing.T, got *model.Geo, want model.Geo) {
	t.Helper()
	if got == nil {
		t.Fatalf("Lookup = nil, want %+v", want)
	}
	if *got != want {
		t.Fatalf("Lookup = %+v, want %+v", *got, want)
	}
}

func TestLookupProviderShapes(t *testing.T) {
	cases := []struct {
		name string
		body string
		want model.Geo
	}{
		{
			"ip-api.com",
			`{"status":"success","country":"France","countryCode":"FR","city":"Paris","as":"AS3215 Orange S.A.","query":"90.0.0.1"}`,
			model.Geo{Country: "FR", City: "Paris", ASN: "AS3215"},
		},
		{
			"ipinfo.io",
			`{"ip":"8.8.8.8","city":"Mountain View","region":"California","country":"US","org":"AS15169 Google LLC"}`,
			model.Geo{Country: "US", City: "Mountain View", ASN: "AS15169"},
		},
		{
			"ipapi.co",
			`{"ip":"1.1.1.1","city":"Brisbane","country_code":"au","country_name":"Australia","asn":"AS13335","org":"CLOUDFLARENET"}`,
			model.Geo{Country: "AU", City: "Brisbane", ASN: "AS13335"},
		},
		{
			"numeric asn",
			`{"country_code":"NL","asn":1136}`,
			model.Geo{Country: "NL", ASN: "AS1136"},
		},
		{
			"asn as bare digits",
			`{"country_code":"NL","asn":"1136"}`,
			model.Geo{Country: "NL", ASN: "AS1136"},
		},
		{
			"country name is not a code",
			`{"country":"Germany","city":"Berlin"}`,
			model.Geo{City: "Berlin"},
		},
		{
			"org without an AS number",
			`{"country":"de","org":"Deutsche Telekom AG"}`,
			model.Geo{Country: "DE"},
		},
		{
			"org starting with AS letters is not an ASN",
			`{"country":"TW","org":"ASUSTEK Computer Inc."}`,
			model.Geo{Country: "TW"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newProvider(t, http.StatusOK, tc.body)
			c := New(p.srv.URL, nil)
			expectGeo(t, c.Lookup(context.Background(), publicIP), tc.want)
		})
	}
}

func TestLookupURLPlaceholder(t *testing.T) {
	p := newProvider(t, http.StatusOK, `{"countryCode":"US"}`)
	c := New(p.srv.URL+"/json/{ip}?fields=countryCode,city,as", nil)
	c.Lookup(context.Background(), publicIP)
	if want := "/json/8.8.8.8?fields=countryCode,city,as"; p.lastPath() != want {
		t.Errorf("request path = %q, want %q", p.lastPath(), want)
	}
}

func TestLookupURLPathAppend(t *testing.T) {
	cases := []struct{ suffix, ip, want string }{
		{"", publicIP, "/8.8.8.8"},
		{"/", publicIP, "/8.8.8.8"},
		{"/v1/", publicIP, "/v1/8.8.8.8"},
		{"/v1", "2001:db8::1", "/v1/2001:db8::1"},
		{"", "::ffff:8.8.8.8", "/8.8.8.8"}, // IPv4-mapped addresses are unmapped first
	}
	for _, tc := range cases {
		t.Run(tc.suffix+" "+tc.ip, func(t *testing.T) {
			p := newProvider(t, http.StatusOK, `{"countryCode":"US"}`)
			c := New(p.srv.URL+tc.suffix, nil)
			c.Lookup(context.Background(), tc.ip)
			if p.lastPath() != tc.want {
				t.Errorf("request path = %q, want %q", p.lastPath(), tc.want)
			}
		})
	}
}

func TestLookupCachesAnswers(t *testing.T) {
	p := newProvider(t, http.StatusOK, `{"countryCode":"FR","city":"Paris"}`)
	c := New(p.srv.URL, nil)

	first := c.Lookup(context.Background(), publicIP)
	expectGeo(t, first, model.Geo{Country: "FR", City: "Paris"})
	first.City = "changed by the caller" // must not leak into the cache

	second := c.Lookup(context.Background(), publicIP)
	expectGeo(t, second, model.Geo{Country: "FR", City: "Paris"})
	if p.count() != 1 {
		t.Errorf("provider hit %d times, want 1", p.count())
	}

	// Another IP is another lookup.
	c.Lookup(context.Background(), "1.1.1.1")
	if p.count() != 2 {
		t.Errorf("provider hit %d times, want 2", p.count())
	}
}

func TestLookupCachesFailures(t *testing.T) {
	p := newProvider(t, http.StatusInternalServerError, `oops`)
	c := New(p.srv.URL, nil)
	for i := 0; i < 3; i++ {
		if g := c.Lookup(context.Background(), publicIP); g != nil {
			t.Fatalf("Lookup = %+v, want nil on a 500", *g)
		}
	}
	if p.count() != 1 {
		t.Errorf("a failing provider was asked %d times, want 1", p.count())
	}
}

func TestLookupCacheExpiry(t *testing.T) {
	base := time.Date(2026, 9, 14, 20, 0, 0, 0, time.UTC)

	t.Run("positive answers live a day", func(t *testing.T) {
		p := newProvider(t, http.StatusOK, `{"countryCode":"FR"}`)
		c := New(p.srv.URL, nil)
		now := base
		c.now = func() time.Time { return now }

		c.Lookup(context.Background(), publicIP)
		now = base.Add(23 * time.Hour)
		c.Lookup(context.Background(), publicIP)
		if p.count() != 1 {
			t.Fatalf("provider hit %d times after 23 h, want 1", p.count())
		}
		now = base.Add(24 * time.Hour)
		c.Lookup(context.Background(), publicIP)
		if p.count() != 2 {
			t.Fatalf("provider hit %d times after 24 h, want 2", p.count())
		}
	})

	t.Run("failures live an hour", func(t *testing.T) {
		p := newProvider(t, http.StatusServiceUnavailable, ``)
		c := New(p.srv.URL, nil)
		now := base
		c.now = func() time.Time { return now }

		c.Lookup(context.Background(), publicIP)
		now = base.Add(59 * time.Minute)
		c.Lookup(context.Background(), publicIP)
		if p.count() != 1 {
			t.Fatalf("provider hit %d times after 59 min, want 1", p.count())
		}
		now = base.Add(time.Hour)
		c.Lookup(context.Background(), publicIP)
		if p.count() != 2 {
			t.Fatalf("provider hit %d times after 1 h, want 2", p.count())
		}
	})
}

func TestLookupCacheBound(t *testing.T) {
	p := newProvider(t, http.StatusOK, `{"countryCode":"FR"}`)
	c := New(p.srv.URL, nil)
	far := time.Now().Add(time.Hour)
	for i := 0; i < maxEntries; i++ {
		c.cache[fmt.Sprintf("10.0.%d.%d", i/256, i%256)] = entry{geo: &model.Geo{Country: "XX"}, expiresAt: far}
	}
	c.Lookup(context.Background(), publicIP)
	if len(c.cache) != 1 {
		t.Fatalf("cache holds %d entries after the bound was reached, want 1", len(c.cache))
	}
}

func TestLookupTimeout(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
			fmt.Fprint(w, `{"countryCode":"FR"}`)
		case <-r.Context().Done(): // the client gave up
		}
	}))
	t.Cleanup(slow.Close)

	// An http client without its own timeout: the lookup's own 1 s cap must do.
	c := New(slow.URL, &http.Client{})
	start := time.Now()
	g := c.Lookup(context.Background(), publicIP)
	elapsed := time.Since(start)

	if g != nil {
		t.Fatalf("Lookup = %+v, want nil on timeout", *g)
	}
	if elapsed < 500*time.Millisecond || elapsed > 1800*time.Millisecond {
		t.Fatalf("Lookup took %v, want about 1 s", elapsed)
	}
}

func TestLookupDisabled(t *testing.T) {
	for _, u := range []string{"", "   "} {
		c := New(u, nil)
		if g := c.Lookup(context.Background(), publicIP); g != nil {
			t.Fatalf("disabled client returned %+v", *g)
		}
	}
}

func TestLookupSkipsUnroutableAddresses(t *testing.T) {
	p := newProvider(t, http.StatusOK, `{"countryCode":"FR"}`)
	c := New(p.srv.URL, nil)
	for _, ip := range []string{
		"", "not-an-ip", "300.1.1.1",
		"127.0.0.1", "::1", // loopback
		"10.1.2.3", "172.16.5.5", "192.168.0.1", "fd00::1", // private
		"169.254.1.1", "fe80::1", "fe80::1%eth0", // link-local
		"0.0.0.0", "::", // unspecified
		"224.0.0.1", "ff02::1", // multicast
		"::ffff:10.0.0.1", // private, once unmapped
	} {
		if g := c.Lookup(context.Background(), ip); g != nil {
			t.Errorf("Lookup(%q) = %+v, want nil", ip, *g)
		}
	}
	if p.count() != 0 {
		t.Errorf("provider hit %d times for unroutable addresses, want 0", p.count())
	}
}

func TestLookupNon2xx(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusTooManyRequests, http.StatusBadGateway} {
		p := newProvider(t, status, `{"countryCode":"FR"}`)
		c := New(p.srv.URL, nil)
		if g := c.Lookup(context.Background(), publicIP); g != nil {
			t.Errorf("HTTP %d: Lookup = %+v, want nil", status, *g)
		}
	}
}

func TestLookupInvalidJSON(t *testing.T) {
	for _, body := range []string{``, `<html>busy</html>`, `{"countryCode":`, `["FR"]`, `"FR"`} {
		p := newProvider(t, http.StatusOK, body)
		c := New(p.srv.URL, nil)
		if g := c.Lookup(context.Background(), publicIP); g != nil {
			t.Errorf("body %q: Lookup = %+v, want nil", body, *g)
		}
	}
}

func TestLookupNothingUsable(t *testing.T) {
	p := newProvider(t, http.StatusOK, `{"status":"fail","message":"reserved range","query":"8.8.8.8","country":"","asn":""}`)
	c := New(p.srv.URL, nil)
	if g := c.Lookup(context.Background(), publicIP); g != nil {
		t.Fatalf("Lookup = %+v, want nil when no field is usable", *g)
	}
}

func TestLookupCallerContextCanceled(t *testing.T) {
	p := newProvider(t, http.StatusOK, `{"countryCode":"FR"}`)
	c := New(p.srv.URL, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if g := c.Lookup(ctx, publicIP); g != nil {
		t.Fatalf("Lookup = %+v on a canceled context, want nil", *g)
	}
	// Our own cancellation is not the provider's fault: nothing was cached.
	expectGeo(t, c.Lookup(context.Background(), publicIP), model.Geo{Country: "FR"})
}

func TestNewDefaultHTTPClient(t *testing.T) {
	c := New("https://example.test", nil)
	if c.http == nil || c.http.Timeout != time.Second {
		t.Fatalf("nil http client must become one with a 1 s timeout, got %+v", c.http)
	}
	own := &http.Client{Timeout: 5 * time.Second}
	if New("https://example.test", own).http != own {
		t.Fatal("a given http client must be used as is")
	}
}
