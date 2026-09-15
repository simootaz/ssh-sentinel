package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/simootaz/ssh-sentinel/backend/internal/app"
)

// The key file Secret Manager hands back. Real ones are about 2.3 KB with a
// private key; the shape is all that matters here.
const fakeKey = `{"type": "service_account", "project_id": "my-project", "client_email": "fcm@my-project.iam.gserviceaccount.com"}`

// fakeSecretManager is the one endpoint of infra/README.md: it checks the
// region, secret id and token, counts the calls and answers like Scaleway
// does.
type fakeSecretManager struct {
	t        *testing.T
	region   string
	secretID string
	token    string
	value    string // returned base64-encoded
	status   int    // 0 means 200
	body     string // overrides the answer when set
	calls    int
}

func (f *fakeSecretManager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.calls++
	wantPath := "/secret-manager/v1beta1/regions/" + f.region + "/secrets/" + f.secretID + "/versions/latest/access"
	if r.Method != http.MethodGet || r.URL.Path != wantPath {
		f.t.Errorf("unexpected request %s %s, want GET %s", r.Method, r.URL.Path, wantPath)
		http.Error(w, `{"message": "not found"}`, http.StatusNotFound)
		return
	}
	if got := r.Header.Get("X-Auth-Token"); got != f.token {
		f.t.Errorf("X-Auth-Token = %q, want %q", got, f.token)
		http.Error(w, `{"message": "invalid token"}`, http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if f.status != 0 {
		w.WriteHeader(f.status)
	}
	if f.body != "" {
		_, _ = w.Write([]byte(f.body))
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{
		"secret_id": f.secretID,
		"revision":  "1",
		"data":      base64.StdEncoding.EncodeToString([]byte(f.value)),
	})
}

func newFake(t *testing.T) (*fakeSecretManager, secretSource) {
	t.Helper()
	f := &fakeSecretManager{
		t:        t,
		region:   "fr-par",
		secretID: "11111111-2222-3333-4444-555555555555",
		token:    "scw-secret-key",
		value:    fakeKey,
	}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, secretSource{baseURL: srv.URL, http: srv.Client()}
}

// scalewayEnv is what Terraform sets on the function: no
// FCM_SERVICE_ACCOUNT_JSON, the secret id and the SCW_* identity instead.
func scalewayEnv(f *fakeSecretManager) map[string]string {
	return map[string]string{
		"DATABASE_URL":                  "postgres://app:secret@db.example:5432/sentinel?sslmode=require",
		"ADMIN_TOKEN":                   "admin-token",
		"FCM_PROJECT_ID":                "my-project",
		"FCM_SERVICE_ACCOUNT_SECRET_ID": f.secretID,
		"SCW_ACCESS_KEY":                "SCWXXXXXXXXXXXXXXXXX",
		"SCW_SECRET_KEY":                f.token,
		"SCW_DEFAULT_PROJECT_ID":        "project-uuid",
		"SCW_DEFAULT_REGION":            f.region,
	}
}

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestWithFCMKeyFetchesFromSecretManager(t *testing.T) {
	f, src := newFake(t)
	env := scalewayEnv(f)

	getenv, fetched, err := withFCMKey(context.Background(), envOf(env), src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fetched {
		t.Error("fetched = false, want true")
	}
	if f.calls != 1 {
		t.Errorf("secret manager called %d times, want 1", f.calls)
	}
	if got := getenv("FCM_SERVICE_ACCOUNT_JSON"); got != fakeKey {
		t.Errorf("FCM_SERVICE_ACCOUNT_JSON = %q, want the decoded key", got)
	}
	// Everything else passes through untouched.
	if got := getenv("ADMIN_TOKEN"); got != "admin-token" {
		t.Errorf("ADMIN_TOKEN = %q, want pass-through", got)
	}
	if got := getenv("SCW_SECRET_KEY"); got != f.token {
		t.Errorf("SCW_SECRET_KEY = %q, want pass-through", got)
	}

	// The core sees a complete FCM configuration, as if the JSON had been
	// an ordinary variable.
	cfg, err := app.LoadConfig(getenv)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.PushEnabled() {
		t.Error("PushEnabled() = false, want true")
	}
	if cfg.FCMServiceAccountJSON != fakeKey {
		t.Errorf("cfg.FCMServiceAccountJSON = %q", cfg.FCMServiceAccountJSON)
	}
}

func TestWithFCMKeyDirectVariableWins(t *testing.T) {
	f, src := newFake(t)
	env := scalewayEnv(f)
	env["FCM_SERVICE_ACCOUNT_JSON"] = `{"type": "service_account", "direct": true}`

	getenv, fetched, err := withFCMKey(context.Background(), envOf(env), src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fetched {
		t.Error("fetched = true, want false: the variable was set directly")
	}
	if f.calls != 0 {
		t.Errorf("secret manager called %d times, want 0", f.calls)
	}
	if got := getenv("FCM_SERVICE_ACCOUNT_JSON"); !strings.Contains(got, `"direct": true`) {
		t.Errorf("FCM_SERVICE_ACCOUNT_JSON = %q, want the direct value", got)
	}
}

func TestWithFCMKeyBothEmptyDisablesPush(t *testing.T) {
	f, src := newFake(t)
	env := scalewayEnv(f)
	delete(env, "FCM_SERVICE_ACCOUNT_SECRET_ID")
	delete(env, "FCM_PROJECT_ID")

	getenv, fetched, err := withFCMKey(context.Background(), envOf(env), src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fetched || f.calls != 0 {
		t.Errorf("fetched = %v, calls = %d, want no lookup without a secret id", fetched, f.calls)
	}
	cfg, err := app.LoadConfig(getenv)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.PushEnabled() {
		t.Error("PushEnabled() = true, want false: both FCM variables empty")
	}
}

func TestWithFCMKeyMissingIdentity(t *testing.T) {
	f, src := newFake(t)
	for _, name := range []string{"SCW_SECRET_KEY", "SCW_DEFAULT_REGION"} {
		env := scalewayEnv(f)
		delete(env, name)
		_, _, err := withFCMKey(context.Background(), envOf(env), src)
		if err == nil {
			t.Errorf("without %s: expected an error", name)
			continue
		}
		if !strings.Contains(err.Error(), name) {
			t.Errorf("without %s: error %q does not name it", name, err)
		}
	}
	if f.calls != 0 {
		t.Errorf("secret manager called %d times, want 0 without credentials", f.calls)
	}
}

func TestWithFCMKeyAPIErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   string // substring of the error
	}{
		{"forbidden", http.StatusForbidden, `{"message": "insufficient permissions", "type": "permissions_denied"}`, "HTTP 403: insufficient permissions"},
		{"not found", http.StatusNotFound, `{"message": "resource is not found"}`, "HTTP 404"},
		{"not json", http.StatusOK, `<html>gateway</html>`, "not JSON"},
		{"no data", http.StatusOK, `{"secret_id": "x", "revision": "1"}`, "no data field"},
		{"bad base64", http.StatusOK, `{"data": "@@not-base64@@"}`, "not base64"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, src := newFake(t)
			f.status = c.status
			f.body = c.body
			_, _, err := withFCMKey(context.Background(), envOf(scalewayEnv(f)), src)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not contain %q", err, c.want)
			}
		})
	}
}

func TestWithFCMKeyUnreachable(t *testing.T) {
	f, _ := newFake(t)
	// A closed port: the lookup must fail with a clear error, not hang.
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()
	src := secretSource{baseURL: dead.URL}

	_, _, err := withFCMKey(context.Background(), envOf(scalewayEnv(f)), src)
	if err == nil || !strings.Contains(err.Error(), "Secret Manager") {
		t.Errorf("unreachable server: got %v", err)
	}
}

func TestWithFCMKeyHonoursContext(t *testing.T) {
	f, src := newFake(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := withFCMKey(ctx, envOf(scalewayEnv(f)), src)
	if err == nil {
		t.Error("cancelled context: expected an error")
	}
}
