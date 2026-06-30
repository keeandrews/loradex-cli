// Package api is the typed HTTP client for the loradex control-plane API.
// File bytes never pass through here — they move directly to/from presigned URLs.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/keeandrews/loradex-cli/internal/buildinfo"
	"github.com/keeandrews/loradex-cli/internal/output"
)

const (
	defaultTimeout = 30 * time.Second
	maxAttempts    = 4
)

// Client talks to the loradex API.
type Client struct {
	Endpoint string // e.g. https://api.loradex.ai
	Web      string // e.g. https://loradex.ai
	Token    string // bearer token for the endpoint host (may be empty)
	Insecure bool   // allow http:// for loopback only
	HTTP     *http.Client
	Log      *output.Printer
}

// New builds a Client with sane transport timeouts.
func New(endpoint, web, token string, insecure bool, log *output.Printer) *Client {
	tr := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: defaultTimeout,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
	}
	return &Client{
		Endpoint: strings.TrimRight(endpoint, "/"),
		Web:      strings.TrimRight(web, "/"),
		Token:    token,
		Insecure: insecure,
		HTTP:     &http.Client{Transport: tr, Timeout: defaultTimeout},
		Log:      log,
	}
}

// CheckEndpoint enforces HTTPS unless the endpoint is a loopback host with --insecure.
func (c *Client) CheckEndpoint() error {
	u, err := url.Parse(c.Endpoint)
	if err != nil || u.Host == "" {
		return output.Usage("invalid endpoint %q", c.Endpoint)
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme == "http" && c.Insecure && isLoopback(u.Hostname()) {
		return nil
	}
	return output.Errorf(output.ExitUsage, "insecure_endpoint",
		"use https, or --insecure with a loopback endpoint for local dev",
		"endpoint %q must use https", c.Endpoint)
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// get issues an idempotent GET and decodes JSON into out.
func (c *Client) get(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, true, nil, out)
}

// post issues a non-idempotent POST (never auto-retried) and decodes into out.
func (c *Client) post(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, http.MethodPost, path, false, body, out)
}

// do performs the request with retry for idempotent calls only.
func (c *Client) do(ctx context.Context, method, path string, idempotent bool, body, out any) error {
	var bodyBytes []byte
	if body != nil {
		var err error
		if bodyBytes, err = json.Marshal(body); err != nil {
			return err
		}
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.Endpoint+path, bytes.NewReader(bodyBytes))
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", fmt.Sprintf("loradex-cli/%s (%s)", buildinfo.Version, buildinfo.Platform()))
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if c.Token != "" {
			req.Header.Set("Authorization", "Bearer "+c.Token)
		}
		c.Log.Debug("%s %s", method, c.Endpoint+path)

		resp, err := c.HTTP.Do(req)
		if err != nil {
			lastErr = err
			if idempotent && attempt < maxAttempts {
				c.sleepBackoff(ctx, attempt, "")
				continue
			}
			return networkError(err)
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			defer resp.Body.Close()
			if out == nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				return nil
			}
			return json.NewDecoder(resp.Body).Decode(out)
		}

		// Retry idempotent calls on 429/5xx.
		if idempotent && attempt < maxAttempts && (resp.StatusCode == 429 || resp.StatusCode >= 500) {
			ra := resp.Header.Get("Retry-After")
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			c.sleepBackoff(ctx, attempt, ra)
			continue
		}

		// Terminal error: parse the envelope.
		var ae apiErrorBody
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		_ = json.Unmarshal(data, &ae)
		return toCLIError(resp.StatusCode, ae)
	}
	if lastErr != nil {
		return networkError(lastErr)
	}
	return output.Errorf(output.ExitError, "error", "", "request failed after %d attempts", maxAttempts)
}

// sleepBackoff waits with exponential backoff + jitter, honoring Retry-After.
func (c *Client) sleepBackoff(ctx context.Context, attempt int, retryAfter string) {
	d := time.Duration(1<<uint(attempt-1)) * 500 * time.Millisecond
	if d > 8*time.Second {
		d = 8 * time.Second
	}
	d += time.Duration(rand.Int64N(int64(300 * time.Millisecond)))
	if retryAfter != "" {
		if secs, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil {
			d = time.Duration(secs) * time.Second
		}
	}
	c.Log.Debug("retrying in %s (attempt %d)", d, attempt)
	select {
	case <-time.After(d):
	case <-ctx.Done():
	}
}
