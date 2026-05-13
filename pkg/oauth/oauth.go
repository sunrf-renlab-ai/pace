package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// Anthropic does not (as of writing) publish a stable third-party OAuth client_id
// for the Claude.ai consumer subscription. The most reliable way for Pace to
// inherit the user's auth is to spawn `claude -p` as a subprocess; if the user
// has run `claude setup-token` (or is logged into Claude Code), the subprocess
// inherits that auth. This package provides the scaffolding for a future
// first-party OAuth flow — endpoints + client_id are env-overridable so the
// user can wire it up the moment Anthropic publishes them.
const (
	defaultAuthzURL = "https://claude.ai/oauth/authorize"
	defaultTokenURL = "https://api.anthropic.com/oauth/token"
	defaultClientID = "pace-cli"
	defaultScope    = "user:profile inference"
)

type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int       `json:"expires_in"`
	ObtainedAt   time.Time `json:"obtained_at"`
}

func (t *Token) Expired() bool {
	if t.ExpiresIn == 0 {
		return false
	}
	return time.Now().After(t.ObtainedAt.Add(time.Duration(t.ExpiresIn) * time.Second))
}

func tokenPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "pace", "auth.json"), nil
}

func Save(t *Token) error {
	p, err := tokenPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, _ := json.Marshal(t)
	return os.WriteFile(p, b, 0o600)
}

func Load() (*Token, error) {
	p, err := tokenPath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var t Token
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// LoadAuthEnv returns env vars to inject into `claude -p` subprocesses.
// Returns nil, nil if no token present (caller should rely on inherited
// claude CLI auth via os.Environ pass-through).
func LoadAuthEnv() (map[string]string, error) {
	t, err := Load()
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return map[string]string{"ANTHROPIC_AUTH_TOKEN": t.AccessToken}, nil
}

// Login runs the full PKCE + browser flow, blocking until the user authorizes
// or the context is cancelled.
func Login(ctx context.Context) (*Token, error) {
	authzURL := envOr("PACE_OAUTH_AUTHZ_URL", defaultAuthzURL)
	tokenURL := envOr("PACE_OAUTH_TOKEN_URL", defaultTokenURL)
	clientID := envOr("PACE_OAUTH_CLIENT_ID", defaultClientID)

	verifier := randomString(64)
	challenge := pkceS256(verifier)
	state := randomString(16)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "state mismatch", 400)
			errCh <- errors.New("state mismatch")
			return
		}
		if e := r.URL.Query().Get("error"); e != "" {
			http.Error(w, "auth error: "+e, 400)
			errCh <- errors.New("auth error: " + e)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code", 400)
			errCh <- errors.New("missing code")
			return
		}
		fmt.Fprint(w, "<html><body><h2>Pace authorized. You can close this tab.</h2></body></html>")
		codeCh <- code
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	defer srv.Close()

	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", defaultScope)
	q.Set("state", state)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")

	authURL := authzURL + "?" + q.Encode()
	openBrowser(authURL)
	fmt.Fprintln(os.Stderr, "Opening browser to authorize Pace...")
	fmt.Fprintln(os.Stderr, "  if it doesn't open, visit:", authURL)

	var code string
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case e := <-errCh:
		return nil, e
	case code = <-codeCh:
	}

	body := url.Values{}
	body.Set("grant_type", "authorization_code")
	body.Set("code", code)
	body.Set("redirect_uri", redirectURI)
	body.Set("client_id", clientID)
	body.Set("code_verifier", verifier)

	resp, err := http.PostForm(tokenURL, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("token endpoint %d: %s", resp.StatusCode, string(respBody))
	}
	var t Token
	if err := json.Unmarshal(respBody, &t); err != nil {
		return nil, err
	}
	t.ObtainedAt = time.Now().UTC()
	if err := Save(&t); err != nil {
		return nil, err
	}
	return &t, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func randomString(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)[:n]
}

func pkceS256(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return
	}
	cmd.Start()
}
