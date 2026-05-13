package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	tok := &Token{AccessToken: "tok-abc", RefreshToken: "ref", TokenType: "Bearer", ExpiresIn: 3600, ObtainedAt: time.Now().UTC()}
	if err := Save(tok); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.AccessToken != "tok-abc" {
		t.Errorf("access = %q", got.AccessToken)
	}
}

func TestLoadAuthEnvMissingReturnsNil(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	env, err := LoadAuthEnv()
	if err != nil {
		t.Fatalf("LoadAuthEnv: %v", err)
	}
	if env != nil {
		t.Errorf("env = %v, want nil", env)
	}
}

func TestLoadAuthEnvWithToken(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	Save(&Token{AccessToken: "abc", ObtainedAt: time.Now().UTC()})
	env, err := LoadAuthEnv()
	if err != nil {
		t.Fatalf("LoadAuthEnv: %v", err)
	}
	if env["ANTHROPIC_AUTH_TOKEN"] != "abc" {
		t.Errorf("env = %v, want ANTHROPIC_AUTH_TOKEN=abc", env)
	}
}

func TestExpired(t *testing.T) {
	expired := &Token{ExpiresIn: 1, ObtainedAt: time.Now().Add(-2 * time.Second).UTC()}
	if !expired.Expired() {
		t.Errorf("expected expired")
	}
	fresh := &Token{ExpiresIn: 3600, ObtainedAt: time.Now().UTC()}
	if fresh.Expired() {
		t.Errorf("fresh token should not be expired")
	}
	noExpiry := &Token{ExpiresIn: 0, ObtainedAt: time.Now().Add(-24 * time.Hour).UTC()}
	if noExpiry.Expired() {
		t.Errorf("token with no expiry should never be expired")
	}
}

// TestLoginFlowAgainstFakeProvider exercises the full PKCE flow against a
// stub OAuth server we run in-process.
func TestLoginFlowAgainstFakeProvider(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	// Stub authorize endpoint: capture the redirect_uri + state, immediately
	// redirect back with a synthesized code (simulates user clicking Authorize).
	var capturedRedirect, capturedState string
	authzSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRedirect = r.URL.Query().Get("redirect_uri")
		capturedState = r.URL.Query().Get("state")
		// Simulate browser redirect by hitting the callback ourselves in a goroutine
		// AFTER returning to the test's openBrowser stub. We'll do it inline below.
		w.WriteHeader(204)
	}))
	defer authzSrv.Close()

	// Stub token endpoint: returns a valid token JSON for any code.
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "fake-access",
			"refresh_token": "fake-refresh",
			"token_type":    "Bearer",
			"expires_in":    3600,
		})
	}))
	defer tokenSrv.Close()

	t.Setenv("MENTOR_OAUTH_AUTHZ_URL", authzSrv.URL+"/authorize")
	t.Setenv("MENTOR_OAUTH_TOKEN_URL", tokenSrv.URL+"/token")

	// Trigger the callback ourselves while Login is blocking.
	go func() {
		// Wait until the authz endpoint has been hit (capturedRedirect populated)
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if capturedRedirect != "" {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if capturedRedirect == "" {
			return
		}
		http.Get(capturedRedirect + "?code=synthetic&state=" + capturedState)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tok, err := Login(ctx)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if tok.AccessToken != "fake-access" {
		t.Errorf("access = %q", tok.AccessToken)
	}
}

func TestPKCEChallengeIsDeterministic(t *testing.T) {
	// PKCE S256 must be deterministic for the same verifier.
	c1 := pkceS256("abc")
	c2 := pkceS256("abc")
	if c1 != c2 {
		t.Errorf("PKCE not deterministic: %s vs %s", c1, c2)
	}
	if strings.Contains(c1, "=") {
		t.Errorf("PKCE should be unpadded base64url: %s", c1)
	}
}

// Sanity: ensure Save uses 0o600 permissions (no world/group read of token).
func TestSavePermissions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	Save(&Token{AccessToken: "x", ObtainedAt: time.Now().UTC()})
	p, _ := tokenPath()
	st, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if st.Mode().Perm()&0o077 != 0 {
		t.Errorf("token file is world/group readable: %v", st.Mode().Perm())
	}
}
