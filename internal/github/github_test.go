package github_test

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
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aivara-se/dispatch/internal/github"
)

func TestBlockMintsOneTokenAndLabels(t *testing.T) {
	key, keyPEM := newKey(t)
	var mu sync.Mutex
	var installations, tokens, labels int
	var labelBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/repos/aivara-se/dispatch/installation":
			installations++
			verifyJWT(t, r, key, 123456)
			w.Write([]byte(`{"id":9}`))
		case "/app/installations/9/access_tokens":
			tokens++
			verifyJWT(t, r, key, 123456)
			w.Write([]byte(`{"token":"secret-token-value","expires_at":"` + time.Now().Add(time.Hour).Format(time.RFC3339) + `"}`))
		case "/repos/aivara-se/dispatch/issues/212/labels":
			labels++
			if got := r.Header.Get("Authorization"); got != "Bearer secret-token-value" {
				t.Errorf("label authorization = %q", got)
			}
			body := make([]byte, r.ContentLength)
			r.Body.Read(body)
			labelBody = string(body)
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := github.New(123456, keyPEM, server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := client.Block(context.Background(), "aivara-se", "dispatch", 212); err != nil {
			t.Fatalf("block %d: %v", i, err)
		}
	}
	if installations != 1 || tokens != 1 || labels != 2 {
		t.Fatalf("installation = %d, token = %d, labels = %d", installations, tokens, labels)
	}
	if labelBody != `{"labels":["blocked"]}` {
		t.Fatalf("label body = %q", labelBody)
	}
}

func TestBlockReportsARejectedWrite(t *testing.T) {
	_, keyPEM := newKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/aivara-se/dispatch/installation":
			w.Write([]byte(`{"id":9}`))
		case "/app/installations/9/access_tokens":
			w.Write([]byte(`{"token":"secret-token-value","expires_at":"` + time.Now().Add(time.Hour).Format(time.RFC3339) + `"}`))
		default:
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"message":"Resource not accessible by integration"}`))
		}
	}))
	defer server.Close()

	client, err := github.New(123456, keyPEM, server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	err = client.Block(context.Background(), "aivara-se", "dispatch", 212)
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), "secret-token-value") {
		t.Fatalf("error carries the token: %v", err)
	}
}

func TestNewRefusesANonKey(t *testing.T) {
	if _, err := github.New(1, []byte("not a key"), "", nil); err == nil {
		t.Fatal("New accepted a non-key")
	}
}

func TestReadsPullRequestAndReviews(t *testing.T) {
	_, keyPEM := newKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/aivara-se/dispatch/installation":
			w.Write([]byte(`{"id":9}`))
		case r.URL.Path == "/app/installations/9/access_tokens":
			w.Write([]byte(`{"token":"secret-token-value","expires_at":"` + time.Now().Add(time.Hour).Format(time.RFC3339) + `"}`))
		case r.URL.Path == "/repos/aivara-se/dispatch/pulls/212":
			if got := r.Header.Get("Authorization"); got != "Bearer secret-token-value" {
				t.Errorf("authorization = %q", got)
			}
			w.Write([]byte(`{"head":{"sha":"s1"},"merged":true}`))
		case r.URL.Path == "/repos/aivara-se/dispatch/pulls/212/reviews":
			w.Write([]byte(`[{"user":{"login":"momo"},"state":"APPROVED","commit_id":"s1","submitted_at":"2026-11-19T00:00:00Z"}]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := github.New(123456, keyPEM, server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	pull, err := client.PullRequest(context.Background(), "aivara-se", "dispatch", 212)
	if err != nil {
		t.Fatal(err)
	}
	if pull.Head != "s1" || !pull.Merged {
		t.Fatalf("pull = %+v", pull)
	}
	reviews, err := client.Reviews(context.Background(), "aivara-se", "dispatch", 212)
	if err != nil {
		t.Fatal(err)
	}
	if len(reviews) != 1 || reviews[0].Login != "momo" || reviews[0].State != "APPROVED" || reviews[0].CommitID != "s1" {
		t.Fatalf("reviews = %+v", reviews)
	}
}

func TestReviewsFollowPages(t *testing.T) {
	_, keyPEM := newKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/aivara-se/dispatch/installation":
			w.Write([]byte(`{"id":9}`))
		case "/app/installations/9/access_tokens":
			w.Write([]byte(`{"token":"secret-token-value","expires_at":"` + time.Now().Add(time.Hour).Format(time.RFC3339) + `"}`))
		case "/repos/aivara-se/dispatch/pulls/212/reviews":
			if r.URL.Query().Get("page") == "1" {
				var body strings.Builder
				body.WriteString("[")
				for i := 0; i < 100; i++ {
					if i > 0 {
						body.WriteString(",")
					}
					body.WriteString(`{"user":{"login":"momo"},"state":"COMMENTED","commit_id":"s0","submitted_at":"2026-11-19T00:00:00Z"}`)
				}
				body.WriteString("]")
				w.Write([]byte(body.String()))
				return
			}
			w.Write([]byte(`[{"state":"APPROVED","commit_id":"s1","submitted_at":"2026-11-19T01:00:00Z"}]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := github.New(123456, keyPEM, server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	reviews, err := client.Reviews(context.Background(), "aivara-se", "dispatch", 212)
	if err != nil {
		t.Fatal(err)
	}
	if len(reviews) != 101 || reviews[100].State != "APPROVED" || reviews[100].CommitID != "s1" {
		t.Fatalf("reviews = %d, last = %+v", len(reviews), reviews[len(reviews)-1])
	}
}

func newKey(t *testing.T) (*rsa.PrivateKey, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}

func verifyJWT(t *testing.T, r *http.Request, key *rsa.PrivateKey, appID int64) {
	t.Helper()
	header := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	parts := strings.Split(header, ".")
	if len(parts) != 3 {
		t.Fatalf("authorization is not a JWT: %q", header)
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, sum[:], signature); err != nil {
		t.Fatalf("jwt signature: %v", err)
	}
	claims, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Issuer int64 `json:"iss"`
	}
	if err := json.Unmarshal(claims, &body); err != nil {
		t.Fatal(err)
	}
	if body.Issuer != appID {
		t.Fatalf("issuer = %d", body.Issuer)
	}
}
