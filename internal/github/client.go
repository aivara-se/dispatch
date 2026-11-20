// Package github is the engine's one App client: it mints an installation
// token for a repository, reads the pull request a condition asks about, and
// writes the blocked label. It makes no other call.
package github

import (
	"bytes"
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
	"strconv"
	"sync"
	"time"
)

const defaultBase = "https://api.github.com"

// DefaultTimeout bounds each request the client makes.
const DefaultTimeout = 15 * time.Second

// Client holds the App's key and the tokens it minted, one per repository.
type Client struct {
	appID int64
	key   *rsa.PrivateKey
	base  string
	http  *http.Client
	mu    sync.Mutex

	// Timeout bounds each request; New sets it to DefaultTimeout. Set it
	// before the first call, and keep it shorter than the delivery it serves.
	Timeout time.Duration

	tokens map[string]token
}

type token struct {
	value   string
	expires time.Time
}

// New reads an RSA private key in PEM and returns a client for the App.
func New(appID int64, keyPEM []byte, baseURL string, httpClient *http.Client) (*Client, error) {
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return nil, errors.New("github: key is not PEM")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, errors.New("github: key is not an RSA private key")
		}
		rsaKey, ok := parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("github: key is not an RSA private key")
		}
		key = rsaKey
	}
	if baseURL == "" {
		baseURL = defaultBase
	}
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Client{
		appID:   appID,
		key:     key,
		base:    baseURL,
		http:    httpClient,
		Timeout: DefaultTimeout,
		tokens:  map[string]token{},
	}, nil
}

// token returns an installation token for a repository, minted once and kept
// until it is two minutes from expiry.
func (c *Client) token(ctx context.Context, owner, repo string) (string, error) {
	name := owner + "/" + repo
	c.mu.Lock()
	defer c.mu.Unlock()
	if cached, ok := c.tokens[name]; ok && time.Until(cached.expires) > 2*time.Minute {
		return cached.value, nil
	}

	now := time.Now()
	signed, err := c.jwt(now)
	if err != nil {
		return "", err
	}
	installation, err := c.installation(ctx, signed, owner, repo)
	if err != nil {
		return "", err
	}
	value, expires, err := c.createToken(ctx, signed, installation)
	if err != nil {
		return "", err
	}
	c.tokens[name] = token{value: value, expires: expires}
	return value, nil
}

func (c *Client) installation(ctx context.Context, signed, owner, repo string) (int64, error) {
	path := fmt.Sprintf("/repos/%s/%s/installation", url.PathEscape(owner), url.PathEscape(repo))
	var body struct {
		ID int64 `json:"id"`
	}
	if err := c.app(ctx, http.MethodGet, path, signed, &body); err != nil {
		return 0, err
	}
	if body.ID == 0 {
		return 0, fmt.Errorf("github: %s/%s has no installation", owner, repo)
	}
	return body.ID, nil
}

func (c *Client) createToken(ctx context.Context, signed string, installation int64) (string, time.Time, error) {
	path := "/app/installations/" + strconv.FormatInt(installation, 10) + "/access_tokens"
	var body struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := c.app(ctx, http.MethodPost, path, signed, &body); err != nil {
		return "", time.Time{}, err
	}
	if body.Token == "" {
		return "", time.Time{}, errors.New("github: empty installation token")
	}
	return body.Token, body.ExpiresAt, nil
}

// app sends one call signed as the App itself.
func (c *Client) app(ctx context.Context, method, path, jwt string, out any) error {
	return c.bound(ctx, func(ctx context.Context) error {
		return c.send(ctx, method, path, jwt, nil, out)
	})
}

// read sends one call on a repository's installation token.
func (c *Client) read(ctx context.Context, owner, repo, method, path string, body []byte, out any) error {
	return c.bound(ctx, func(ctx context.Context) error {
		token, err := c.token(ctx, owner, repo)
		if err != nil {
			return err
		}
		return c.send(ctx, method, path, token, body, out)
	})
}

// bound runs one call inside the client's per-request timeout, so no call to
// GitHub can outlive it.
func (c *Client) bound(ctx context.Context, call func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	return call(ctx)
}

func (c *Client) send(ctx context.Context, method, path, token string, body []byte, out any) error {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("github: %s: %w", path, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
		return fmt.Errorf("github: %s: %s", path, response.Status)
	}
	if out == nil {
		io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
		return nil
	}
	return json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(out)
}

// jwt signs the App claims GitHub accepts for ten minutes.
func (c *Client) jwt(now time.Time) (string, error) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, err := json.Marshal(struct {
		IssuedAt  int64 `json:"iat"`
		ExpiresAt int64 `json:"exp"`
		Issuer    int64 `json:"iss"`
	}{
		IssuedAt:  now.Add(-time.Minute).Unix(),
		ExpiresAt: now.Add(9 * time.Minute).Unix(),
		Issuer:    c.appID,
	})
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(claims)
	signed := header + "." + payload
	sum := sha256.Sum256([]byte(signed))
	signature, err := rsa.SignPKCS1v15(rand.Reader, c.key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return signed + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}
