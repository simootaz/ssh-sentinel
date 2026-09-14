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
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OAuth 2.0 JWT bearer grant (RFC 7523): how a service account gets an
// access token without a user. Sign a JWT with the key from the key file,
// trade it for a bearer token that lives one hour.
const (
	defaultTokenURL = "https://oauth2.googleapis.com/token"
	grantType       = "urn:ietf:params:oauth:grant-type:jwt-bearer"
	scope           = "https://www.googleapis.com/auth/firebase.messaging"
	jwtLifetime     = time.Hour // the maximum Google accepts

	// refreshMargin is how long before expiry a cached token is replaced,
	// so that a token never expires in the middle of a push round.
	refreshMargin = time.Minute
)

// serviceAccount is what New reads from the Firebase key file. The file has
// more fields; these three are the ones the grant needs.
type serviceAccount struct {
	email    string
	key      *rsa.PrivateKey
	tokenURI string
}

func parseServiceAccount(raw []byte) (*serviceAccount, error) {
	var f struct {
		ClientEmail string `json:"client_email"`
		PrivateKey  string `json:"private_key"`
		TokenURI    string `json:"token_uri"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("fcm: service account key is not valid JSON: %w", err)
	}
	if f.ClientEmail == "" {
		return nil, errors.New("fcm: service account key: client_email is missing")
	}
	if f.PrivateKey == "" {
		return nil, errors.New("fcm: service account key: private_key is missing")
	}
	key, err := parsePrivateKey(f.PrivateKey)
	if err != nil {
		return nil, err
	}
	if f.TokenURI == "" {
		f.TokenURI = defaultTokenURL
	}
	return &serviceAccount{email: f.ClientEmail, key: key, tokenURI: f.TokenURI}, nil
}

// parsePrivateKey reads the PEM key of the key file. Firebase ships PKCS#8
// ("PRIVATE KEY"); PKCS#1 ("RSA PRIVATE KEY") is accepted too, for keys
// converted by hand with openssl.
func parsePrivateKey(pemText string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		return nil, errors.New("fcm: service account key: private_key is not PEM")
	}
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		rsaKey, ok := k.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("fcm: service account key: private_key is not an RSA key")
		}
		return rsaKey, nil
	}
	rsaKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("fcm: service account key: cannot parse private_key: %w", err)
	}
	return rsaKey, nil
}

// accessTokenFor returns a valid access token, minting one when the cached
// token is missing or about to expire. The lock is held during the mint so
// that the concurrent sends of one push round share a single token request
// instead of each minting their own.
func (c *Client) accessTokenFor(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if c.accessToken != "" && now.Before(c.expiresAt.Add(-refreshMargin)) {
		return c.accessToken, nil
	}
	tok, ttl, err := c.mint(ctx, now)
	if err != nil {
		return "", err
	}
	c.accessToken = tok
	c.expiresAt = now.Add(ttl)
	return tok, nil
}

// forget drops the cached token after FCM rejected it, unless another
// goroutine already replaced it, in which case the new one is kept.
func (c *Client) forget(rejected string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.accessToken == rejected {
		c.accessToken = ""
		c.expiresAt = time.Time{}
	}
}

// tokenResponse is the answer of the token endpoint. expires_in is in seconds.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
}

// mint signs a JWT and exchanges it for an access token. It returns the
// token and how long it is valid. A missing expires_in gives zero: a token
// of unknown lifetime is used once and not cached.
func (c *Client) mint(ctx context.Context, now time.Time) (string, time.Duration, error) {
	assertion, err := c.signJWT(now)
	if err != nil {
		return "", 0, err
	}
	form := url.Values{"grant_type": {grantType}, "assertion": {assertion}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, fmt.Errorf("fcm: build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("fcm: token request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return "", 0, fmt.Errorf("fcm: read token response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", 0, fmt.Errorf("fcm: token request failed: HTTP %d: %s", resp.StatusCode, excerpt(body))
	}
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", 0, fmt.Errorf("fcm: decode token response: %w", err)
	}
	if tr.AccessToken == "" {
		return "", 0, errors.New("fcm: token response has no access_token")
	}
	return tr.AccessToken, time.Duration(tr.ExpiresIn) * time.Second, nil
}

// jwtClaims is the payload of the assertion. Google checks every field: the
// issuer must be the key's client_email, the audience the token endpoint,
// and exp at most one hour after iat.
type jwtClaims struct {
	Iss   string `json:"iss"`
	Scope string `json:"scope"`
	Aud   string `json:"aud"`
	Iat   int64  `json:"iat"`
	Exp   int64  `json:"exp"`
}

// signJWT builds header.claims.signature: RS256 (RSASSA-PKCS1-v1_5 with
// SHA-256), each part base64url without padding.
func (c *Client) signJWT(now time.Time) (string, error) {
	header := b64([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, err := json.Marshal(jwtClaims{
		Iss:   c.email,
		Scope: scope,
		Aud:   c.TokenURL,
		Iat:   now.Unix(),
		Exp:   now.Add(jwtLifetime).Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("fcm: encode jwt claims: %w", err)
	}
	signingInput := header + "." + b64(claims)
	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, c.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("fcm: sign jwt: %w", err)
	}
	return signingInput + "." + b64(sig), nil
}

// b64 is base64url without padding, as JWTs require.
func b64(v []byte) string { return base64.RawURLEncoding.EncodeToString(v) }
