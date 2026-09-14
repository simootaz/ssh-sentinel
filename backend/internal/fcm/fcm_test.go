package fcm

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const testEmail = "push@ssh-sentinel-test.iam.gserviceaccount.com"

// testKey is generated once: a 2048-bit key takes a moment and every test
// can share it.
var (
	testKeyOnce sync.Once
	testKey     *rsa.PrivateKey
)

func rsaTestKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	testKeyOnce.Do(func() {
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(err)
		}
		testKey = k
	})
	return testKey
}

func pkcs8PEM(t *testing.T, key *rsa.PrivateKey) string {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func pkcs1PEM(key *rsa.PrivateKey) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}

func serviceAccountJSON(t *testing.T, fields map[string]string) []byte {
	t.Helper()
	b, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// fakeGoogle stands in for the token endpoint and the FCM send endpoint. It
// checks what the real endpoints check and records what it saw.
type fakeGoogle struct {
	t   *testing.T
	pub *rsa.PublicKey
	srv *httptest.Server

	// respond answers a send call; hit is 1 for the first call.
	respond func(w http.ResponseWriter, hit int)

	mu        sync.Mutex
	tokenHits int
	sendHits  int
	auths     []string         // Authorization header of every send call
	messages  []map[string]any // decoded "message" of every send call
}

func newFakeGoogle(t *testing.T, pub *rsa.PublicKey, respond func(w http.ResponseWriter, hit int)) *fakeGoogle {
	t.Helper()
	g := &fakeGoogle{t: t, pub: pub, respond: respond}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", g.token)
	mux.HandleFunc("POST /send", g.send)
	g.srv = httptest.NewServer(mux)
	t.Cleanup(g.srv.Close)
	return g
}

func (g *fakeGoogle) tokenURL() string { return g.srv.URL + "/token" }

func (g *fakeGoogle) hits() (token, send int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.tokenHits, g.sendHits
}

func (g *fakeGoogle) auth(i int) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.auths[i]
}

func (g *fakeGoogle) message(i int) map[string]any {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.messages[i]
}

// token verifies the JWT bearer grant and issues "token-<n>".
func (g *fakeGoogle) token(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	g.tokenHits++
	n := g.tokenHits
	g.mu.Unlock()

	if err := r.ParseForm(); err != nil {
		g.t.Errorf("token: parse form: %v", err)
	}
	if got := r.PostForm.Get("grant_type"); got != grantType {
		g.t.Errorf("token: grant_type = %q, want %q", got, grantType)
	}
	g.verifyAssertion(r.PostForm.Get("assertion"))

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"access_token":"token-%d","expires_in":3599,"token_type":"Bearer"}`, n)
}

func (g *fakeGoogle) verifyAssertion(assertion string) {
	parts := strings.Split(assertion, ".")
	if len(parts) != 3 {
		g.t.Errorf("token: assertion has %d parts, want 3", len(parts))
		return
	}
	decode := func(s string) []byte {
		b, err := base64.RawURLEncoding.DecodeString(s)
		if err != nil {
			g.t.Errorf("token: assertion part is not base64url without padding: %v", err)
		}
		return b
	}

	var header map[string]string
	if err := json.Unmarshal(decode(parts[0]), &header); err != nil {
		g.t.Errorf("token: header: %v", err)
	}
	if header["alg"] != "RS256" || header["typ"] != "JWT" {
		g.t.Errorf("token: header = %v, want alg RS256 and typ JWT", header)
	}

	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(g.pub, crypto.SHA256, digest[:], decode(parts[2])); err != nil {
		g.t.Errorf("token: signature does not verify: %v", err)
	}

	var claims jwtClaims
	if err := json.Unmarshal(decode(parts[1]), &claims); err != nil {
		g.t.Errorf("token: claims: %v", err)
	}
	if claims.Iss != testEmail {
		g.t.Errorf("token: iss = %q, want %q", claims.Iss, testEmail)
	}
	if claims.Scope != scope {
		g.t.Errorf("token: scope = %q, want %q", claims.Scope, scope)
	}
	if claims.Aud != g.tokenURL() {
		g.t.Errorf("token: aud = %q, want %q", claims.Aud, g.tokenURL())
	}
	if claims.Exp-claims.Iat != 3600 {
		g.t.Errorf("token: exp - iat = %d s, want 3600", claims.Exp-claims.Iat)
	}
}

// send records the call and answers with the test's respond function.
func (g *fakeGoogle) send(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Message map[string]any `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		g.t.Errorf("send: body is not JSON: %v", err)
	}
	g.mu.Lock()
	g.sendHits++
	hit := g.sendHits
	g.auths = append(g.auths, r.Header.Get("Authorization"))
	g.messages = append(g.messages, body.Message)
	g.mu.Unlock()
	g.respond(w, hit)
}

func accepted(w http.ResponseWriter, _ int) {
	fmt.Fprint(w, `{"name":"projects/test-project/messages/0:1726344700"}`)
}

func answer(status int, body string) func(w http.ResponseWriter, hit int) {
	return func(w http.ResponseWriter, _ int) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}
}

// newTestClient wires a client to a fake Google. respond decides what the
// send endpoint answers.
func newTestClient(t *testing.T, respond func(w http.ResponseWriter, hit int)) (*Client, *fakeGoogle) {
	t.Helper()
	key := rsaTestKey(t)
	g := newFakeGoogle(t, &key.PublicKey, respond)
	sa := serviceAccountJSON(t, map[string]string{
		"type":         "service_account",
		"client_email": testEmail,
		"private_key":  pkcs8PEM(t, key),
		"token_uri":    g.tokenURL(),
	})
	c, err := New("test-project", sa, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.SendURL = g.srv.URL + "/send"
	return c, g
}

var testData = map[string]string{
	"type":       "access_request",
	"request_id": "5f1c9b2e-2c3a-4f6a-9b1e-0d3a2b7c4e11",
	"username":   "deploy",
	"source_ip":  "",
}

func mustSend(t *testing.T, c *Client, token string) {
	t.Helper()
	if err := c.Send(context.Background(), token, testData); err != nil {
		t.Fatalf("Send: %v", err)
	}
}

func TestSendHappyPath(t *testing.T) {
	c, g := newTestClient(t, accepted)
	mustSend(t, c, "device-token-1")

	if tok, snd := g.hits(); tok != 1 || snd != 1 {
		t.Fatalf("hits: token %d send %d, want 1 and 1", tok, snd)
	}
	if got := g.auth(0); got != "Bearer token-1" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer token-1")
	}

	m := g.message(0)
	if m["token"] != "device-token-1" {
		t.Errorf("message.token = %v, want device-token-1", m["token"])
	}
	data, _ := m["data"].(map[string]any)
	if len(data) != len(testData) {
		t.Errorf("message.data = %v, want %v", data, testData)
	}
	for k, v := range testData {
		if data[k] != v {
			t.Errorf("message.data[%q] = %v, want %q", k, data[k], v)
		}
	}
	android, _ := m["android"].(map[string]any)
	if android["priority"] != "high" || android["ttl"] != "60s" {
		t.Errorf("message.android = %v, want priority high and ttl 60s", android)
	}
	if _, has := m["notification"]; has {
		t.Errorf("message carries a notification block; messages must be data-only")
	}
}

func TestSendReusesCachedToken(t *testing.T) {
	c, g := newTestClient(t, accepted)
	mustSend(t, c, "device-token-1")
	mustSend(t, c, "device-token-2")

	if tok, snd := g.hits(); tok != 1 || snd != 2 {
		t.Fatalf("hits: token %d send %d, want 1 and 2", tok, snd)
	}
	if g.auth(0) != "Bearer token-1" || g.auth(1) != "Bearer token-1" {
		t.Errorf("both sends must use token-1, got %q and %q", g.auth(0), g.auth(1))
	}
}

func TestSendRemintsExpiredToken(t *testing.T) {
	c, g := newTestClient(t, accepted)
	base := time.Date(2026, 9, 14, 20, 0, 0, 0, time.UTC)
	now := base
	c.now = func() time.Time { return now }

	mustSend(t, c, "device-token-1")

	// The token lives 3599 s and is replaced 60 s before expiry.
	now = base.Add(3500 * time.Second) // 99 s left: still good
	mustSend(t, c, "device-token-1")
	if tok, _ := g.hits(); tok != 1 {
		t.Fatalf("token minted again with 99 s left: %d mints", tok)
	}

	now = base.Add(3540 * time.Second) // 59 s left: inside the margin
	mustSend(t, c, "device-token-1")
	if tok, _ := g.hits(); tok != 2 {
		t.Fatalf("token not re-minted with 59 s left: %d mints", tok)
	}
	if got := g.auth(2); got != "Bearer token-2" {
		t.Errorf("third send used %q, want the new token-2", got)
	}
}

func TestSendUnregisteredOn404(t *testing.T) {
	c, _ := newTestClient(t, answer(http.StatusNotFound, `{"error":{"code":404,"message":"Requested entity was not found.","status":"NOT_FOUND"}}`))
	err := c.Send(context.Background(), "dead-token", testData)
	if !errors.Is(err, ErrUnregistered) {
		t.Fatalf("Send = %v, want ErrUnregistered", err)
	}
}

func TestSendUnregisteredFromErrorBody(t *testing.T) {
	cases := []struct{ name, body string }{
		{"details errorCode", `{"error":{"code":400,"message":"The registration token is not a valid FCM registration token","status":"INVALID_ARGUMENT","details":[{"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError","errorCode":"UNREGISTERED"}]}}`},
		{"status", `{"error":{"code":400,"message":"gone","status":"NOT_FOUND"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 400, not 404: the body alone must be enough.
			c, _ := newTestClient(t, answer(http.StatusBadRequest, tc.body))
			err := c.Send(context.Background(), "dead-token", testData)
			if !errors.Is(err, ErrUnregistered) {
				t.Fatalf("Send = %v, want ErrUnregistered", err)
			}
		})
	}
}

func TestSendServerError(t *testing.T) {
	c, _ := newTestClient(t, answer(http.StatusInternalServerError, `{"error":{"code":500,"message":"Internal error encountered.","status":"INTERNAL"}}`))
	err := c.Send(context.Background(), "device-token-1", testData)
	if err == nil {
		t.Fatal("Send returned nil on a 500")
	}
	if errors.Is(err, ErrUnregistered) {
		t.Fatalf("a 500 must not look like a dead token: %v", err)
	}
	if !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "Internal error encountered") {
		t.Errorf("error must carry the status and the body, got: %v", err)
	}
}

func TestSendErrorBodyExcerptIsBounded(t *testing.T) {
	c, _ := newTestClient(t, answer(http.StatusBadGateway, strings.Repeat("x", 5000)))
	err := c.Send(context.Background(), "device-token-1", testData)
	if err == nil || len(err.Error()) > 300 {
		t.Fatalf("error must be short, got %d bytes: %.80v", len(fmt.Sprint(err)), err)
	}
}

func TestSendRetriesOnceAfter401(t *testing.T) {
	c, g := newTestClient(t, func(w http.ResponseWriter, hit int) {
		if hit == 1 {
			answer(http.StatusUnauthorized, `{"error":{"code":401,"status":"UNAUTHENTICATED"}}`)(w, hit)
			return
		}
		accepted(w, hit)
	})
	mustSend(t, c, "device-token-1")

	if tok, snd := g.hits(); tok != 2 || snd != 2 {
		t.Fatalf("hits: token %d send %d, want 2 and 2", tok, snd)
	}
	if g.auth(0) != "Bearer token-1" || g.auth(1) != "Bearer token-2" {
		t.Errorf("retry must use a fresh token, got %q then %q", g.auth(0), g.auth(1))
	}
}

func TestSendGivesUpAfterSecond401(t *testing.T) {
	c, g := newTestClient(t, answer(http.StatusUnauthorized, `{"error":{"code":401,"status":"UNAUTHENTICATED"}}`))
	err := c.Send(context.Background(), "device-token-1", testData)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("Send = %v, want an error mentioning 401", err)
	}
	if tok, snd := g.hits(); tok != 2 || snd != 2 {
		t.Fatalf("hits: token %d send %d, want exactly one retry (2 and 2)", tok, snd)
	}
}

func TestSendRespectsContext(t *testing.T) {
	c, g := newTestClient(t, accepted)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := c.Send(ctx, "device-token-1", testData)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Send = %v, want context.Canceled", err)
	}
	if _, snd := g.hits(); snd != 0 {
		t.Errorf("a send went out on a canceled context")
	}
}

func TestSendRejectsEmptyDeviceToken(t *testing.T) {
	c, g := newTestClient(t, accepted)
	if err := c.Send(context.Background(), "", testData); err == nil {
		t.Fatal("Send accepted an empty device token")
	}
	if tok, snd := g.hits(); tok != 0 || snd != 0 {
		t.Errorf("hits: token %d send %d, want none", tok, snd)
	}
}

func TestNewRejectsBadKey(t *testing.T) {
	key := rsaTestKey(t)
	good := map[string]string{"client_email": testEmail, "private_key": pkcs8PEM(t, key), "token_uri": "https://example.test/token"}
	without := func(field string) []byte {
		m := map[string]string{}
		for k, v := range good {
			if k != field {
				m[k] = v
			}
		}
		return serviceAccountJSON(t, m)
	}
	with := func(field, value string) []byte {
		m := map[string]string{}
		for k, v := range good {
			m[k] = v
		}
		m[field] = value
		return serviceAccountJSON(t, m)
	}

	cases := []struct {
		name string
		raw  []byte
	}{
		{"not JSON", []byte("{not json")},
		{"empty", nil},
		{"missing client_email", without("client_email")},
		{"missing private_key", without("private_key")},
		{"private_key not PEM", with("private_key", "-----BEGIN NOTHING")},
		{"private_key PEM but garbage", with("private_key", string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("garbage")})))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if c, err := New("test-project", tc.raw, nil); err == nil {
				t.Fatalf("New accepted the key, got client %+v", c)
			}
		})
	}

	if _, err := New("", serviceAccountJSON(t, good), nil); err == nil {
		t.Error("New accepted an empty project id")
	}
}

func TestNewAcceptsPKCS1Key(t *testing.T) {
	key := rsaTestKey(t)
	g := newFakeGoogle(t, &key.PublicKey, accepted)
	sa := serviceAccountJSON(t, map[string]string{
		"client_email": testEmail,
		"private_key":  pkcs1PEM(key),
		"token_uri":    g.tokenURL(),
	})
	c, err := New("test-project", sa, nil)
	if err != nil {
		t.Fatalf("New rejected a PKCS#1 key: %v", err)
	}
	c.SendURL = g.srv.URL + "/send"
	// The fake verifies the signature, so a working send proves the key was read right.
	mustSend(t, c, "device-token-1")
}

func TestNewDefaults(t *testing.T) {
	key := rsaTestKey(t)
	sa := serviceAccountJSON(t, map[string]string{"client_email": testEmail, "private_key": pkcs8PEM(t, key)})
	c, err := New("my-project", sa, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.TokenURL != "https://oauth2.googleapis.com/token" {
		t.Errorf("TokenURL = %q, want the Google default", c.TokenURL)
	}
	if want := "https://fcm.googleapis.com/v1/projects/my-project/messages:send"; c.SendURL != want {
		t.Errorf("SendURL = %q, want %q", c.SendURL, want)
	}
	if c.http == nil || c.http.Timeout != 10*time.Second {
		t.Errorf("nil http client must become one with a 10 s timeout, got %+v", c.http)
	}

	own := &http.Client{Timeout: 3 * time.Second}
	c, err = New("my-project", sa, own)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.http != own {
		t.Error("a given http client must be used as is")
	}
}
