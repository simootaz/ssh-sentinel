// Package client talks to the backend over contract v1 (docs/architecture.md, section 5).
//
// Errors are classified the way the decision table needs them: a *RejectedError means the
// backend answered 4xx (it is up and refuses us), an *UnreachableError means anything else went
// wrong (network, TLS, timeout, 5xx, malformed body).
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxBody = 1 << 20

// Client is a backend client authenticated with one server token.
type Client struct {
	baseURL   string
	token     string
	http      *http.Client
	UserAgent string
}

// New returns a client for baseURL, without a trailing slash. Timeouts come from the context
// passed to each call.
func New(baseURL, token string) *Client {
	return NewWithHTTP(baseURL, token, &http.Client{})
}

// NewWithHTTP is New with a caller-supplied http.Client, for tests.
func NewWithHTTP(baseURL, token string, hc *http.Client) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, http: hc, UserAgent: "ssh-sentinel"}
}

// AccessRequest is the body of POST /access-request. Pointers become JSON null when nil,
// which is what the contract wants for an unknown source IP or command.
type AccessRequest struct {
	Context  string  `json:"context"`
	Mode     string  `json:"mode"`
	Username string  `json:"username"`
	SourceIP *string `json:"source_ip"`
	Hostname string  `json:"hostname"`
	TTY      string  `json:"tty"`
	Command  *string `json:"command"`
}

// AccessResponse is the 200 body of POST /access-request.
type AccessResponse struct {
	RequestID string  `json:"request_id"`
	Verdict   string  `json:"verdict"`
	Reason    string  `json:"reason"`
	DecidedAt *string `json:"decided_at"`
}

// WhitelistEntry is one item of GET /whitelist as seen by an agent.
type WhitelistEntry struct {
	ID        string     `json:"id"`
	Username  string     `json:"username"`
	Context   string     `json:"context"`
	Server    *string    `json:"server"`
	ExpiresAt *time.Time `json:"expires_at"`
}

// RejectedError: the backend answered 4xx.
type RejectedError struct {
	Status  int
	Message string
}

func (e *RejectedError) Error() string {
	return fmt.Sprintf("backend rejected the request: %d %s", e.Status, e.Message)
}

// UnreachableError: no usable answer from the backend.
type UnreachableError struct{ Cause error }

func (e *UnreachableError) Error() string { return "backend unreachable: " + e.Cause.Error() }
func (e *UnreachableError) Unwrap() error { return e.Cause }

// IsRejected reports whether err is a 4xx answer.
func IsRejected(err error) bool {
	var r *RejectedError
	return errors.As(err, &r)
}

// IsUnreachable reports whether err means the backend gave no usable answer.
func IsUnreachable(err error) bool {
	var u *UnreachableError
	return errors.As(err, &u)
}

// AccessRequest posts one login or sudo request and blocks until the backend answers.
func (c *Client) AccessRequest(ctx context.Context, req AccessRequest) (*AccessResponse, error) {
	var resp AccessResponse
	if err := c.do(ctx, http.MethodPost, "/access-request", req, &resp); err != nil {
		return nil, err
	}
	if resp.Verdict != "approve" && resp.Verdict != "deny" {
		return nil, &UnreachableError{Cause: fmt.Errorf("malformed response: verdict %q", resp.Verdict)}
	}
	return &resp, nil
}

// Whitelist returns the unexpired entries for this server plus the global ones.
func (c *Client) Whitelist(ctx context.Context) ([]WhitelistEntry, error) {
	var out struct {
		Items []WhitelistEntry `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, "/whitelist", nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return &UnreachableError{Cause: err}
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.UserAgent)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := c.http.Do(req)
	if err != nil {
		return &UnreachableError{Cause: err}
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if err != nil {
		return &UnreachableError{Cause: err}
	}

	switch {
	case res.StatusCode >= 200 && res.StatusCode < 300:
		if out == nil {
			return nil
		}
		if err := json.Unmarshal(data, out); err != nil {
			return &UnreachableError{Cause: fmt.Errorf("malformed response: %w", err)}
		}
		return nil
	case res.StatusCode >= 400 && res.StatusCode < 500:
		return &RejectedError{Status: res.StatusCode, Message: errorMessage(data)}
	default:
		return &UnreachableError{Cause: fmt.Errorf("status %d", res.StatusCode)}
	}
}

func errorMessage(data []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &e) == nil && e.Error != "" {
		return e.Error
	}
	return "no error message"
}
