// Package geo looks up the country, city and ASN of a source IP, best effort.
// The answer decorates the push and feeds the geo rules; a failure leaves it
// empty and the request goes on with the country unknown (docs/architecture.md,
// section 7 row 10, section 8 item 9). The push must never wait on it, so a
// lookup is capped at one second and answers are cached per IP for a day.
package geo

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/simootaz/ssh-sentinel/backend/internal/model"
)

const (
	// lookupTimeout bounds one lookup whatever the http client's own timeout is.
	lookupTimeout = time.Second

	// positiveTTL is how long a located IP stays cached; negativeTTL how long
	// a failure does, so that a provider that is down is not asked again for
	// every login attempt.
	positiveTTL = 24 * time.Hour
	negativeTTL = time.Hour

	// maxEntries bounds the cache. Past it the whole map is dropped: simpler
	// than tracking recency, and a day of lookups rarely gets near it.
	maxEntries = 10_000

	// maxBody caps a provider answer; the useful part is a few hundred bytes.
	maxBody = 64 << 10
)

// Client queries an HTTP geolocation endpoint with a short timeout and
// caches the answers per IP. Safe for concurrent use.
type Client struct {
	url  string // lookup URL, with {ip} or as a base to append the IP to; "" disables
	http *http.Client
	now  func() time.Time

	mu    sync.Mutex
	cache map[string]entry
}

// entry is one cached answer. A nil geo is a negative answer: the provider
// had nothing usable, or could not be reached.
type entry struct {
	geo       *model.Geo
	expiresAt time.Time
}

// New builds a client. An empty lookupURL disables lookups: Lookup returns
// nil without a request. A nil httpClient gets a 1 s timeout.
func New(lookupURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: lookupTimeout}
	}
	return &Client{
		url:   strings.TrimSpace(lookupURL),
		http:  httpClient,
		now:   time.Now,
		cache: map[string]entry{},
	}
}

// Lookup returns the geolocation of ip, or nil when unknown or disabled. It
// returns within about a second in every case.
func (c *Client) Lookup(ctx context.Context, ip string) *model.Geo {
	if c.url == "" {
		return nil
	}
	addr, ok := routable(ip)
	if !ok {
		return nil
	}
	key := addr.String()
	if g, found := c.cached(key); found {
		return g
	}
	g := c.fetch(ctx, key)
	if ctx.Err() != nil {
		// The caller gave up (the agent hung up), which says nothing about
		// the provider: do not remember a failure that was ours.
		return nil
	}
	c.remember(key, g)
	return g
}

// routable parses ip and reports whether a provider can locate it.
// Loopback, private, link-local, unspecified and multicast addresses are
// skipped without a request: the answer would be meaningless and the call
// would count against the provider's quota.
func routable(ip string) (netip.Addr, bool) {
	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return netip.Addr{}, false
	}
	addr = addr.Unmap().WithZone("")
	if addr.IsLoopback() || addr.IsPrivate() || addr.IsUnspecified() || addr.IsMulticast() ||
		addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsInterfaceLocalMulticast() {
		return netip.Addr{}, false
	}
	return addr, true
}

// cached returns the cached answer for key and whether there was one. The
// caller gets a copy, so the cached value cannot be changed from outside.
func (c *Client) cached(key string) (*model.Geo, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.cache[key]
	if !ok || !c.now().Before(e.expiresAt) {
		return nil, false
	}
	if e.geo == nil {
		return nil, true
	}
	g := *e.geo
	return &g, true
}

// remember stores an answer, positive or negative, with its expiry. The
// cache keeps its own copy: the caller owns the value it was returned.
func (c *Client) remember(key string, g *model.Geo) {
	ttl := negativeTTL
	var stored *model.Geo
	if g != nil {
		ttl = positiveTTL
		copied := *g
		stored = &copied
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.cache) >= maxEntries {
		c.cache = map[string]entry{}
	}
	c.cache[key] = entry{geo: stored, expiresAt: c.now().Add(ttl)}
}

// fetch asks the provider. Every failure is a nil: the caller stores geo as
// null and moves on, nothing is logged.
func (c *Client) fetch(ctx context.Context, ip string) *model.Geo {
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.lookupURL(ip), nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil
	}
	return parse(body)
}

// lookupURL puts the IP into the configured URL: in place of {ip} when the
// URL has it (http://ip-api.com/json/{ip}?fields=...), else appended as the
// last path element (https://ipinfo.io -> https://ipinfo.io/1.2.3.4).
func (c *Client) lookupURL(ip string) string {
	escaped := url.PathEscape(ip)
	if strings.Contains(c.url, "{ip}") {
		return strings.ReplaceAll(c.url, "{ip}", escaped)
	}
	return strings.TrimRight(c.url, "/") + "/" + escaped
}

// parse reads the fields the common free providers more or less agree on:
// ip-api.com (countryCode, city, as), ipinfo.io (country, city, org) and
// ipapi.co (country_code, city, asn). Anything else is ignored, and an
// answer with nothing usable is nil.
func parse(body []byte) *model.Geo {
	var fields map[string]any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber() // keeps a numeric asn as its digits, not 1.5169e+04
	if err := dec.Decode(&fields); err != nil {
		return nil
	}
	g := &model.Geo{
		Country: country(fields),
		City:    text(fields["city"]),
		ASN:     asn(fields),
	}
	if *g == (model.Geo{}) {
		return nil
	}
	return g
}

// country returns the ISO 3166-1 alpha-2 code, upper case. Only a two-letter
// value counts: "country" is a code on ipinfo.io but a name on ip-api.com.
func country(f map[string]any) string {
	for _, key := range []string{"country_code", "countryCode", "country"} {
		if s := strings.ToUpper(text(f[key])); isCountryCode(s) {
			return s
		}
	}
	return ""
}

func isCountryCode(s string) bool {
	if len(s) != 2 {
		return false
	}
	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}

// asn returns the AS number as "AS<n>". Providers give it as "AS15169", as
// a number, or at the start of an organisation string ("AS15169 Google LLC").
func asn(f map[string]any) string {
	for _, key := range []string{"asn", "as", "org"} {
		switch v := f[key].(type) {
		case json.Number:
			if n, err := v.Int64(); err == nil {
				return "AS" + strconv.FormatInt(n, 10)
			}
		case string:
			if s := asnFromString(v); s != "" {
				return s
			}
		}
	}
	return ""
}

// asnFromString takes the first word of s when it is an AS number, with or
// without the AS prefix. "ASUSTEK" or "Google LLC" give nothing.
func asnFromString(s string) string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return ""
	}
	first := strings.ToUpper(words[0])
	if strings.HasPrefix(first, "AS") && isDigits(first[2:]) {
		return first
	}
	if isDigits(first) {
		return "AS" + first
	}
	return ""
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// text returns v trimmed when it is a string, else "".
func text(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}
