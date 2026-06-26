package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/sardanioss/httpcloak"
	"github.com/teacat/chaturbate-dvr/entity"
	"github.com/teacat/chaturbate-dvr/server"
)

// defaultProxyRefreshURLs are built-in sources for free SOCKS5 proxies.
// Used when no explicit PROXY_REFRESH_URL is configured.
//
// Region selection avoids countries where Chaturbate enforces age/face ID
// verification. As of 2026, these include 26 US states (TX, LA, UT, VA, FL,
// NC, etc.), France, Germany, Italy, the UK, and Australia. Safe regions
// include Netherlands (NL), Canada (CA), India (IN), and most of Asia/SA.
//
// Priority:
//  1. NL + CA + IN — proven safe, no age verification required
//  2. All-region fallback — broader pool; age verification is detected at
//     the HTTP response level (ErrAgeVerification) regardless of proxy region
//
// Sources: ProxyScrape free proxy lists.
var defaultProxyRefreshURLs = []string{
	"https://api.proxyscrape.com/v4/free-proxy-list/get?request=display_proxies&format=text&protocol=socks5&country=nl,ca,in",
	"https://cdn.jsdelivr.net/gh/proxyscrape/free-proxy-list@main/proxies/socks5/data.txt",
}

// httpcloakTransport wraps httpcloak.Client as an http.RoundTripper.
// It emulates a Chrome 146 TLS/HTTP2 fingerprint to bypass Cloudflare WAF
// TCP RST that Go's default crypto/tls triggers.
// ECH (Encrypted Client Hello) hides the SNI from network observers for
// better Cloudflare bot scores.
//
// When the SOCKS5 proxy is unreachable (i/o timeout, connection refused),
// automatically rotates to the next proxy URL in the list. This handles
// the case where free proxy servers are intermittently available.
type httpcloakTransport struct {
	mu        sync.Mutex
	client    *httpcloak.Client
	proxyURLs []string
	proxyIdx  int
	renewing  bool // prevents re-entrant refreshProxies calls from RoundTrip recursion

	lastRefreshTime time.Time
	lastRefreshURLs []string
}

// GetProxyStatus returns a snapshot of the proxy pool state for the admin page.
func GetProxyStatus() entity.ProxyStatus {
	t, ok := getSharedTransport().(*httpcloakTransport)
	if !ok {
		return entity.ProxyStatus{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	refreshURLs := defaultProxyRefreshURLs
	if server.Config != nil && server.Config.ProxyRefreshURL != "" {
		refreshURLs = []string{server.Config.ProxyRefreshURL}
	}
	return entity.ProxyStatus{
		PoolSize:        len(t.proxyURLs),
		CurrentIndex:    t.proxyIdx,
		RefreshURLs:     refreshURLs,
		LastRefreshTime: t.lastRefreshTime.Format(time.RFC3339),
		ConfigURL:       server.Config.ProxyURL,
	}
}

// sharedTransportSingleton is a singleton http.RoundTripper for the shared transport.
var sharedTransportSingleton http.RoundTripper
var sharedTransportOnce sync.Once

func getSharedTransport() http.RoundTripper {
	sharedTransportOnce.Do(func() {
		proxyURLs := configuredProxyURLs()
		client := newCloakClient(proxyURLAt(proxyURLs, 0))
		sharedTransportSingleton = &httpcloakTransport{
			client:    client,
			proxyURLs: proxyURLs,
		}
	})
	return sharedTransportSingleton
}

func proxyURLAt(urls []string, idx int) string {
	if len(urls) == 0 {
		return ""
	}
	return urls[idx%len(urls)]
}

// newCloakClient creates a new httpcloak client with the given proxy URL.
func newCloakClient(proxyURL string) *httpcloak.Client {
	opts := []httpcloak.Option{
		httpcloak.WithTimeout(120 * time.Second),
	}
	if proxyURL != "" {
		opts = append(opts, httpcloak.WithProxy(proxyURL))
	}
	return httpcloak.New("chrome-146-windows", opts...)
}

// configuredProxyURLs returns all proxy URLs (supports comma-separated for failover).
func configuredProxyURLs() []string {
	if server.Config == nil {
		return nil
	}
	raw := strings.TrimSpace(server.Config.ProxyURL)
	if raw == "" {
		return nil
	}

	username := strings.TrimSpace(server.Config.ProxyUsername)
	password := strings.TrimSpace(server.Config.ProxyPassword)

	var urls []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		part = applyProxyAuth(part, username, password)
		urls = append(urls, part)
	}
	return urls
}

func applyProxyAuth(proxyURL, username, password string) string {
	if username == "" && password == "" {
		return proxyURL
	}
	u, err := url.Parse(proxyURL)
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil {
		return proxyURL
	}
	if password != "" {
		u.User = url.UserPassword(username, password)
	} else {
		u.User = url.User(username)
	}
	return u.String()
}

// rotateProxy recreates the httpcloak client with the next proxy in the list.
// Returns true if a different proxy was selected.
func (t *httpcloakTransport) rotateProxy() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(t.proxyURLs) <= 1 {
		return false
	}

	t.proxyIdx++
	proxyURL := proxyURLAt(t.proxyURLs, t.proxyIdx)

	// Close old client if it exposes a Close method
	if c, ok := interface{}(t.client).(interface{ Close() error }); ok {
		c.Close()
	}

	t.client = newCloakClient(proxyURL)
	return true
}

// WarmupChaturbate makes an initial request to chaturbate.com to establish
// TLS session tickets with Cloudflare before any API calls are made.
// This gives subsequent requests TLS session resumption, making them look
// more like a returning browser visitor.
// Uses a single-attempt round trip — warmup is best-effort and should not
// retry through multiple proxies (that can delay startup by 30s per domain).
func WarmupChaturbate(ctx context.Context) {
	req, err := http.NewRequestWithContext(ctx, "HEAD", "https://chaturbate.com/", nil)
	if err != nil {
		return
	}
	SetRequestHeaders(req)
	t, ok := getSharedTransport().(*httpcloakTransport)
	if !ok {
		return
	}
	resp, err := t.roundTripOnce(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

// WarmupStripchat makes an initial request to stripchat.com to establish TLS
// session tickets before any API calls are made. This is the same idea as
// WarmupChaturbate but for Stripchat's domain.
func WarmupStripchat(ctx context.Context) {
	req, err := http.NewRequestWithContext(ctx, "HEAD", "https://stripchat.com/", nil)
	if err != nil {
		return
	}
	SetRequestHeaders(req)
	t, ok := getSharedTransport().(*httpcloakTransport)
	if !ok {
		return
	}
	resp, err := t.roundTripOnce(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

// isProxyError checks if an error is a proxy connection failure (SOCKS5 unreachable).
// These errors trigger automatic proxy rotation.
func isProxyError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "SOCKS5 CONNECT failed") ||
		strings.Contains(msg, "connect to SOCKS5 proxy") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no reachable proxy")
}

// cdnHostSuffixes lists CDN hostname suffixes that serve HLS segments
// with signed URLs (pkey/token). These hosts are directly reachable from
// any region — the proxy is only needed for geo-unblocking API requests
// (chaturbate.com, stripchat.com). Bypassing the proxy for CDN eliminates
// the slow-proxy → timeout → pkey-expiry failure chain.
var cdnHostSuffixes = []string{
	".doppiocdn.net",
	".doppiocdn.com",
	".live.mmcdn.com",
}

// proxyBypassHosts lists hosts that should never use the proxy.
// Stripchat doesn't need a Netherlands proxy — it has no age verification.
var proxyBypassHosts = []string{
	"stripchat.com",
	".stripchat.com",
}

func isCDNHost(host string) bool {
	host = strings.ToLower(host)
	for _, suffix := range cdnHostSuffixes {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

func isProxyBypassHost(host string) bool {
	host = strings.ToLower(host)
	for _, h := range proxyBypassHosts {
		if host == h || strings.HasSuffix(host, h) {
			return true
		}
	}
	return false
}

// roundTripOnce executes a single request attempt using the current httpcloak
// client. No proxy rotation — used by warmup functions (best-effort).
func (t *httpcloakTransport) roundTripOnce(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme == "http" || isCDNHost(req.URL.Host) || isProxyBypassHost(req.URL.Host) {
		return http.DefaultTransport.RoundTrip(req)
	}

	ctx := req.Context()
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}

	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		req.Body.Close()
		if err != nil {
			return nil, err
		}
	}

	t.mu.Lock()
	client := t.client
	t.mu.Unlock()

	cloakReq := &httpcloak.Request{
		Method:  req.Method,
		URL:     req.URL.String(),
		Headers: req.Header,
	}
	if len(bodyBytes) > 0 {
		cloakReq.Body = bytes.NewReader(bodyBytes)
	}

	cloakResp, err := client.Do(ctx, cloakReq)
	if err != nil {
		return nil, err
	}

	body, bodyErr := cloakResp.Bytes()
	if bodyErr != nil {
		cloakResp.Close()
		return nil, bodyErr
	}

	resp := &http.Response{
		StatusCode: cloakResp.StatusCode,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    req,
	}
	if cloakResp.Headers != nil {
		for k, vs := range cloakResp.Headers {
			for _, v := range vs {
				resp.Header.Add(k, v)
			}
		}
	}
	return resp, nil
}

// RoundTrip implements http.RoundTripper. CDN requests bypass the proxy
// entirely. API requests use httpcloak with the SOCKS5 proxy, and
// automatically rotate to the next proxy on connection failure.
func (t *httpcloakTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme == "http" || isCDNHost(req.URL.Host) || isProxyBypassHost(req.URL.Host) {
		return http.DefaultTransport.RoundTrip(req)
	}

	ctx := req.Context()
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}

	// Prepare request body once, reuse across retries
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		req.Body.Close()
		if err != nil {
			return nil, err
		}
	}

	// Try up to len(proxyURLs) attempts, rotating proxy on connection failures.
	t.mu.Lock()
	proxyCount := len(t.proxyURLs)
	t.mu.Unlock()

	for attempt := 0; attempt < max(1, proxyCount); attempt++ {
		t.mu.Lock()
		client := t.client
		t.mu.Unlock()

		cloakReq := &httpcloak.Request{
			Method:  req.Method,
			URL:     req.URL.String(),
			Headers: req.Header,
		}
		if len(bodyBytes) > 0 {
			cloakReq.Body = bytes.NewReader(bodyBytes)
		}

		cloakResp, err := client.Do(ctx, cloakReq)

		if err == nil {
			body, bodyErr := cloakResp.Bytes()
			if bodyErr != nil {
				cloakResp.Close()
				return nil, bodyErr
			}

			resp := &http.Response{
				StatusCode: cloakResp.StatusCode,
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewReader(body)),
				Request:    req,
			}
			if cloakResp.Headers != nil {
				for k, vs := range cloakResp.Headers {
					for _, v := range vs {
						resp.Header.Add(k, v)
					}
				}
			}
			return resp, nil
		}

		// Proxy connection failure — rotate to next proxy in the list
		if isProxyError(err) {
			if t.rotateProxy() {
				continue
			}
		}
		return nil, err
	}

	// All SOCKS5 proxies failed — try to refresh the proxy list before giving up.
	// The renewing flag prevents re-entrant calls that would cause infinite recursion
	// when the refreshed proxy list contains the same (still-failing) proxies.
	t.mu.Lock()
	alreadyRenewing := t.renewing
	if !alreadyRenewing {
		t.renewing = true
	}
	t.mu.Unlock()

	if !alreadyRenewing {
		if refreshed := t.refreshProxies(); refreshed {
			t.mu.Lock()
			t.renewing = false
			t.mu.Unlock()
			// Retry with the new proxy list
			return t.RoundTrip(req)
		}
		t.mu.Lock()
		t.renewing = false
		t.mu.Unlock()
	}
	return nil, fmt.Errorf("all proxies failed")
}

// refreshProxies fetches fresh proxy URLs from the configured refresh URL
// (or built-in defaults) and updates the transport's proxy list.
// Uses direct connection (no proxy) to avoid circular dependency.
// Returns true if new proxies were loaded.
func (t *httpcloakTransport) refreshProxies() bool {
	if server.Config == nil {
		return false
	}

	refreshURLs := defaultProxyRefreshURLs
	if server.Config.ProxyRefreshURL != "" {
		refreshURLs = []string{server.Config.ProxyRefreshURL}
	}

	for _, url := range refreshURLs {
		if t.fetchProxiesFrom(url) {
			t.mu.Lock()
			t.lastRefreshTime = time.Now()
			t.lastRefreshURLs = refreshURLs
			t.mu.Unlock()
			fmt.Printf("[proxy] loaded %d proxies from %s\n", len(t.proxyURLs), url)
			return true
		}
	}
	return false
}

// fetchProxiesFrom fetches proxy URLs from a single refresh endpoint and
// updates the transport if any are found. Returns true on success.
func (t *httpcloakTransport) fetchProxiesFrom(refreshURL string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", refreshURL, nil)
	if err != nil {
		return false
	}

	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil || len(body) == 0 {
		return false
	}

	var newProxies []string

	// Try JSON array first: ["socks5://ip:port", ...]
	if bytes.HasPrefix(bytes.TrimSpace(body), []byte("[")) {
		var parsed []string
		if err := json.Unmarshal(body, &parsed); err == nil && len(parsed) > 0 {
			newProxies = parsed
		}
	}

	// Fall back to newline-separated URLs (also handles comma-separated)
	if len(newProxies) == 0 {
		text := strings.TrimSpace(string(body))
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			// Support comma-separated on a single line
			for _, part := range strings.Split(line, ",") {
				part = strings.TrimSpace(part)
				if part != "" {
					newProxies = append(newProxies, part)
				}
			}
		}
	}

	if len(newProxies) == 0 {
		return false
	}

	// Normalize: prepend socks5:// to bare ip:port entries
	for i, p := range newProxies {
		if !strings.Contains(p, "://") {
			newProxies[i] = "socks5://" + p
		}
	}

	// Apply proxy auth credentials if configured
	username := strings.TrimSpace(server.Config.ProxyUsername)
	password := strings.TrimSpace(server.Config.ProxyPassword)
	for i, p := range newProxies {
		newProxies[i] = applyProxyAuth(p, username, password)
	}

	// Update the transport with the new proxy list
	t.mu.Lock()
	defer t.mu.Unlock()

	t.proxyURLs = newProxies
	t.proxyIdx = 0
	if len(newProxies) > 0 {
		if c, ok := interface{}(t.client).(interface{ Close() error }); ok {
			c.Close()
		}
		t.client = newCloakClient(newProxies[0])
	}

	return true
}

// StartProxyRefresher periodically fetches fresh proxies in the background.
// Runs every refreshInterval (default 10 minutes) until ctx is cancelled.
// When no explicit PROXY_REFRESH_URL is configured, uses built-in default
// sources (ProxyScrape free SOCKS5 lists for NL + IN).
func StartProxyRefresher(ctx context.Context) {
	interval := 10 * time.Minute
	if server.Config != nil && server.Config.ProxyRefreshInterval > 0 {
		interval = server.Config.ProxyRefreshInterval
	}

	t, ok := getSharedTransport().(*httpcloakTransport)
	if !ok {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Do an initial refresh on startup
	if t.refreshProxies() {
		fmt.Println("[proxy] refreshed proxy list on startup")
		publishProxyRefreshEvent(len(t.proxyURLs))
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if t.refreshProxies() {
				fmt.Println("[proxy] refreshed proxy list")
				t.mu.Lock()
				n := len(t.proxyURLs)
				t.mu.Unlock()
				publishProxyRefreshEvent(n)
			}
		}
	}
}

func publishProxyRefreshEvent(count int) {
	go func() {
		if server.Manager == nil {
			return
		}
		data, _ := json.Marshal(map[string]interface{}{
			"type":    "proxy_refreshed",
			"message": fmt.Sprintf("Proxy list refreshed: %d proxies available", count),
			"time":    time.Now().Format(time.RFC3339),
		})
		server.Manager.PublishAdminEvent("proxy", data)
	}()
}

// max returns the larger of a and b.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
