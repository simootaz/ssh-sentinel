package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Environment variables the adapter reads on top of the backend's own
// (infra/README.md, "What the function receives"). Terraform sets them; the
// SCW_* names are the ones the Scaleway SDK and CLI read by default.
const (
	envFCMKey      = "FCM_SERVICE_ACCOUNT_JSON"      // what the core reads
	envFCMSecretID = "FCM_SERVICE_ACCOUNT_SECRET_ID" // uuid of the secret holding it
	envSecretKey   = "SCW_SECRET_KEY"
	envRegion      = "SCW_DEFAULT_REGION"
)

// secretManagerURL is the Scaleway API. The test replaces it with a local
// server.
const secretManagerURL = "https://api.scaleway.com"

// maxSecretBody caps the answer read from Secret Manager. A secret is at
// most 64 KiB and a Firebase key about 2.3 KB.
const maxSecretBody = 1 << 20

// secretSource reads secret values from Scaleway Secret Manager over its
// HTTPS API, with the standard library only.
type secretSource struct {
	baseURL string
	http    *http.Client
}

// access returns the latest version of a secret, decoded.
//
// The call is the one documented in infra/README.md:
//
//	GET {base}/secret-manager/v1beta1/regions/{region}/secrets/{id}/versions/latest/access
//	X-Auth-Token: {SCW_SECRET_KEY}
//
// and the answer is JSON with a "data" field holding the value in base64.
func (s secretSource) access(ctx context.Context, region, secretID, token string) ([]byte, error) {
	url := fmt.Sprintf("%s/secret-manager/v1beta1/regions/%s/secrets/%s/versions/latest/access",
		strings.TrimRight(s.baseURL, "/"), region, secretID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Auth-Token", token)
	req.Header.Set("Accept", "application/json")

	client := s.http
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("secret manager: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSecretBody))
	if err != nil {
		return nil, fmt.Errorf("secret manager: read answer: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		// Scaleway answers {"message": "...", "type": "..."} on errors; the
		// message is what an operator needs (wrong key, wrong region, no
		// SecretManagerSecretAccess policy).
		var apiErr struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(body, &apiErr)
		if apiErr.Message == "" {
			apiErr.Message = strings.TrimSpace(string(body))
		}
		return nil, fmt.Errorf("secret manager: HTTP %d: %s", resp.StatusCode, apiErr.Message)
	}

	var answer struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		return nil, fmt.Errorf("secret manager: answer is not JSON: %w", err)
	}
	if answer.Data == "" {
		return nil, errors.New("secret manager: answer has no data field")
	}
	value, err := base64.StdEncoding.DecodeString(answer.Data)
	if err != nil {
		return nil, fmt.Errorf("secret manager: data is not base64: %w", err)
	}
	return value, nil
}

// withFCMKey returns the environment the core should read.
//
// On Scaleway the Firebase key does not fit in an environment variable, so
// Terraform passes FCM_SERVICE_ACCOUNT_SECRET_ID instead of
// FCM_SERVICE_ACCOUNT_JSON. When the JSON variable is empty and the secret
// id is set, the key is fetched and the returned getenv answers it for
// FCM_SERVICE_ACCOUNT_JSON; everything else passes through unchanged. The
// core's own rules then apply: both FCM variables set means push on, both
// empty means push off, one without the other is a configuration error.
//
// fetched reports whether a lookup happened, for the start log line.
func withFCMKey(ctx context.Context, getenv func(string) string, src secretSource) (env func(string) string, fetched bool, err error) {
	if strings.TrimSpace(getenv(envFCMKey)) != "" {
		return getenv, false, nil // set directly, nothing to fetch
	}
	secretID := strings.TrimSpace(getenv(envFCMSecretID))
	if secretID == "" {
		return getenv, false, nil // neither: push disabled, the core says so
	}

	token := strings.TrimSpace(getenv(envSecretKey))
	region := strings.TrimSpace(getenv(envRegion))
	var missing []string
	if token == "" {
		missing = append(missing, envSecretKey)
	}
	if region == "" {
		missing = append(missing, envRegion)
	}
	if len(missing) > 0 {
		return nil, false, fmt.Errorf("configuration: %s is set but %s missing, cannot read the FCM key from Secret Manager",
			envFCMSecretID, strings.Join(missing, " and "))
	}

	key, err := src.access(ctx, region, secretID, token)
	if err != nil {
		return nil, false, fmt.Errorf("read FCM key from Secret Manager: %w", err)
	}
	value := string(key)

	return func(name string) string {
		if name == envFCMKey {
			return value
		}
		return getenv(name)
	}, true, nil
}
