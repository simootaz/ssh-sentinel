// Package fcm sends FCM HTTP v1 data messages to the phones, with the
// standard library only. The Firebase service account key signs a JWT, the
// JWT is exchanged for a short-lived OAuth 2.0 access token, and the access
// token authorizes the send calls (docs/architecture.md, section 6).
//
// Messages are data-only, never a notification block: the app builds the
// notification itself, with the action buttons, in every app state
// (section 5, "Push messages").
package fcm

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ErrUnregistered is returned when FCM reports the device token as unknown:
// the app was uninstalled or the token rotated. The caller logs it and moves
// on to the next phone.
var ErrUnregistered = errors.New("fcm: token not registered")

const (
	// messageTTL is how long FCM keeps trying to deliver. The request itself
	// expires 30 s after creation, so a later delivery would be useless.
	messageTTL = "60s"

	// maxBody caps how much of a response is read. Error bodies are small;
	// a misbehaving proxy must not make the backend buffer megabytes.
	maxBody = 64 << 10

	// errExcerpt is how much of an error body ends up in the error message.
	errExcerpt = 200
)

// Client sends messages for one Firebase project. Safe for concurrent use:
// the handler pushes to every phone at once.
type Client struct {
	// SendURL is the FCM v1 send endpoint of the project. New sets it; the
	// tests point it at a local server.
	SendURL string
	// TokenURL is the OAuth 2.0 token endpoint, from the key file.
	TokenURL string

	http  *http.Client
	email string          // client_email of the service account, the JWT issuer
	key   *rsa.PrivateKey // its private key, signs the JWT
	now   func() time.Time

	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time // when accessToken stops being valid
}

// New builds a client from the Firebase project id and the service account
// key JSON (the file downloaded from the Firebase console, passed through
// FCM_SERVICE_ACCOUNT_JSON). A nil httpClient gets a 10 s timeout.
func New(projectID string, serviceAccountJSON []byte, httpClient *http.Client) (*Client, error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, errors.New("fcm: project id is empty")
	}
	sa, err := parseServiceAccount(serviceAccountJSON)
	if err != nil {
		return nil, err
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{
		SendURL:  "https://fcm.googleapis.com/v1/projects/" + projectID + "/messages:send",
		TokenURL: sa.tokenURI,
		http:     httpClient,
		email:    sa.email,
		key:      sa.key,
		now:      time.Now,
	}, nil
}

// sendRequest is the body of a send call: one device token, string data,
// high priority on Android and a short TTL. No notification block, on
// purpose (see the package comment).
type sendRequest struct {
	Message message `json:"message"`
}

type message struct {
	Token   string            `json:"token"`
	Data    map[string]string `json:"data"`
	Android androidConfig     `json:"android"`
}

type androidConfig struct {
	Priority string `json:"priority"`
	TTL      string `json:"ttl"`
}

// Send delivers a high-priority data-only message to one device token. It
// returns nil when FCM accepted the message, ErrUnregistered when the token
// is dead, and an error carrying the HTTP status otherwise. A 401 means the
// access token was revoked early: the client mints a new one and retries once.
func (c *Client) Send(ctx context.Context, token string, data map[string]string) error {
	if token == "" {
		return errors.New("fcm: empty device token")
	}
	body, err := json.Marshal(sendRequest{Message: message{
		Token:   token,
		Data:    data,
		Android: androidConfig{Priority: "high", TTL: messageTTL},
	}})
	if err != nil {
		return fmt.Errorf("fcm: encode message: %w", err)
	}

	accessToken, err := c.accessTokenFor(ctx)
	if err != nil {
		return err
	}
	status, resp, err := c.post(ctx, accessToken, body)
	if err != nil {
		return err
	}
	if status == http.StatusUnauthorized {
		c.forget(accessToken)
		if accessToken, err = c.accessTokenFor(ctx); err != nil {
			return err
		}
		if status, resp, err = c.post(ctx, accessToken, body); err != nil {
			return err
		}
	}
	return checkResponse(status, resp)
}

// post does one send call and returns the status and the body. The body is
// read in full (up to maxBody) so that the connection can be reused.
func (c *Client) post(ctx context.Context, accessToken string, body []byte) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.SendURL, bytes.NewReader(body))
	if err != nil {
		return 0, nil, fmt.Errorf("fcm: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("fcm: send: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return 0, nil, fmt.Errorf("fcm: read response: %w", err)
	}
	return resp.StatusCode, respBody, nil
}

// errorResponse is the part of an FCM v1 error body that tells a dead token
// apart from a real failure. FCM puts UNREGISTERED in details[].errorCode
// and NOT_FOUND in status; both are checked.
type errorResponse struct {
	Error struct {
		Status  string `json:"status"`
		Details []struct {
			ErrorCode string `json:"errorCode"`
		} `json:"details"`
	} `json:"error"`
}

// checkResponse maps a send answer to nil, ErrUnregistered or an error.
func checkResponse(status int, body []byte) error {
	if status >= 200 && status < 300 {
		return nil
	}
	if status == http.StatusNotFound || saysUnregistered(body) {
		return ErrUnregistered
	}
	return fmt.Errorf("fcm: send failed: HTTP %d: %s", status, excerpt(body))
}

func saysUnregistered(body []byte) bool {
	var e errorResponse
	if json.Unmarshal(body, &e) != nil {
		return false
	}
	if deadToken(e.Error.Status) {
		return true
	}
	for _, d := range e.Error.Details {
		if deadToken(d.ErrorCode) {
			return true
		}
	}
	return false
}

func deadToken(code string) bool { return code == "UNREGISTERED" || code == "NOT_FOUND" }

// excerpt returns the start of a body for an error message, on one line.
func excerpt(body []byte) string {
	s := strings.Join(strings.Fields(string(body)), " ")
	if len(s) > errExcerpt {
		s = s[:errExcerpt] + "..."
	}
	return s
}
