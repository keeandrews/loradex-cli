package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"time"

	"github.com/keeandrews/loradex-cli/internal/output"
)

// ExchangeResp is the result of the CLI token exchange.
type ExchangeResp struct {
	Token     string `json:"token"`
	Handle    string `json:"handle"`
	ExpiresAt string `json:"expires_at"`
}

// PKCE holds a verifier and its S256 challenge.
type PKCE struct {
	Verifier  string
	Challenge string
}

// NewPKCE generates a PKCE verifier/challenge pair.
func NewPKCE() PKCE {
	v := randB64(32)
	sum := sha256.Sum256([]byte(v))
	return PKCE{Verifier: v, Challenge: base64.RawURLEncoding.EncodeToString(sum[:])}
}

// RandomState returns a random anti-CSRF state token.
func RandomState() string { return randB64(24) }

func randB64(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// callbackResult is delivered by the loopback handler.
type callbackResult struct {
	code  string
	state string
	err   error
}

// LoopbackServer captures the browser redirect on a random localhost port.
type LoopbackServer struct {
	listener net.Listener
	server   *http.Server
	resultCh chan callbackResult
}

// StartLoopback binds 127.0.0.1:0 and serves /callback.
func StartLoopback() (*LoopbackServer, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &LoopbackServer{listener: ln, resultCh: make(chan callbackResult, 1)}
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		code, state := q.Get("code"), q.Get("state")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if code == "" {
			errMsg := q.Get("error")
			if errMsg == "" {
				errMsg = "missing code"
			}
			fmt.Fprintf(w, callbackHTML, "Authorization failed", "You can close this tab and return to the terminal.")
			s.resultCh <- callbackResult{err: fmt.Errorf("authorization failed: %s", errMsg)}
			return
		}
		fmt.Fprintf(w, callbackHTML, "You're signed in to loradex", "You can close this tab and return to the terminal.")
		s.resultCh <- callbackResult{code: code, state: state}
	})
	s.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = s.server.Serve(ln) }()
	return s, nil
}

// Port returns the bound port.
func (s *LoopbackServer) Port() int { return s.listener.Addr().(*net.TCPAddr).Port }

// CallbackURL is the redirect target the web flow must call.
func (s *LoopbackServer) CallbackURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d/callback", s.Port())
}

// Wait blocks until the callback fires, verifying state, or times out.
func (s *LoopbackServer) Wait(ctx context.Context, wantState string, timeout time.Duration) (string, error) {
	select {
	case res := <-s.resultCh:
		if res.err != nil {
			return "", res.err
		}
		if res.state != wantState {
			return "", output.Errorf(output.ExitError, "auth_failed", "", "state mismatch — aborting for safety")
		}
		return res.code, nil
	case <-time.After(timeout):
		return "", output.Errorf(output.ExitError, "auth_timeout", "re-run `loradex login`", "timed out waiting for browser authorization")
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// Shutdown stops the loopback server.
func (s *LoopbackServer) Shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = s.server.Shutdown(ctx)
}

// AuthorizeURL builds the web authorize URL for the loopback flow.
func (c *Client) AuthorizeURL(callback, state, challenge string) string {
	return fmt.Sprintf("%s/cli/authorize?callback=%s&state=%s&challenge=%s",
		c.Web, urlEnc(callback), urlEnc(state), urlEnc(challenge))
}

// Exchange swaps the one-time code for a CLI token (non-idempotent; no retry).
func (c *Client) Exchange(ctx context.Context, code, verifier string) (*ExchangeResp, error) {
	body := map[string]string{"code": code, "verifier": verifier}
	var out ExchangeResp
	if err := c.post(ctx, "/v1/auth/cli/exchange", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// OpenBrowser best-effort opens url in the default browser.
func OpenBrowser(url string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd, args = "open", []string{url}
	case "windows":
		cmd, args = "cmd", []string{"/c", "start", url}
	default:
		cmd, args = "xdg-open", []string{url}
	}
	return exec.Command(cmd, args...).Start()
}

func urlEnc(s string) string { return url.QueryEscape(s) }

const callbackHTML = `<!doctype html><html><head><meta charset="utf-8"><title>loradex</title>
<style>body{font-family:system-ui,sans-serif;background:#0e0f13;color:#f4f5f7;display:flex;height:100vh;margin:0;align-items:center;justify-content:center}
.c{text-align:center}.c h1{font-weight:700}.a{color:#8b6cff}</style></head>
<body><div class="c"><h1>%s</h1><p class="a">%s</p></div></body></html>`
