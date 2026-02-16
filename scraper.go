package tiktok

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-rod/rod"
	"golang.org/x/net/proxy"
)

const defaultUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// debugPerf enables performance timing output to stderr.
var debugPerf bool

// perfLog prints a performance timing message to stderr when debug is enabled.
func perfLog(format string, args ...any) {
	if debugPerf {
		fmt.Fprintf(os.Stderr, "[perf] "+format+"\n", args...)
	}
}

var tiktokURL, _ = url.Parse("https://www.tiktok.com")

// Scraper is the main TikTok scraper. It uses pure HTTP for user profiles
// (SSR parsing) and a headless browser only for signing search URLs.
type Scraper struct {
	client    *http.Client
	proxy     string
	userAgent string
	isLogged  bool
	baseURL   string // defaults to "https://www.tiktok.com"

	// Browser for URL signing only.
	browser      *rod.Browser
	page         *rod.Page
	browserMu    sync.Mutex
	signingReady atomic.Bool

	// signFunc signs a raw URL via browser JS. Replaceable for testing.
	signFunc func(rawURL string) (string, error)

	// fetchFunc signs a URL and fetches it inside the browser via JS fetch().
	// Uses the browser's TLS fingerprint and cookies. Replaceable for testing.
	fetchFunc func(rawURL string) ([]byte, error)

	// Per-operation rate limiting.
	// Search: ~30/min → 2s min. Profile: ~60/min → 1s min.
	searchDelay  time.Duration
	profileDelay time.Duration
	lastSearch   time.Time
	lastProfile  time.Time
	searchMu     sync.Mutex
	profileMu    sync.Mutex

	// Session token.
	msToken string

	// Device fingerprint (generated once per Scraper instance).
	deviceID string
}

// defaultTransport returns an http.Transport optimized for scraping:
// connection pooling, keep-alive, and TLS handshake caching.
func defaultTransport() *http.Transport {
	return &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}
}

// New creates a Scraper with sensible defaults. The browser is not launched
// until InitBrowser or Login is called.
func New() *Scraper {
	jar, _ := cookiejar.New(nil)
	s := &Scraper{
		client: &http.Client{
			Jar:       jar,
			Timeout:   15 * time.Second,
			Transport: defaultTransport(),
		},
		baseURL:      "https://www.tiktok.com",
		userAgent:    defaultUserAgent,
		searchDelay:  2 * time.Second,
		profileDelay: 1 * time.Second,
		deviceID:     generateDeviceID(),
	}
	s.signFunc = s.signURL
	s.fetchFunc = s.browserFetch
	return s
}

// generateDeviceID creates a random 19-digit device ID (mimics TikTok web).
func generateDeviceID() string {
	// 19-digit random number starting with 7 (matches TikTok pattern).
	id := int64(7e18) + rand.Int64N(int64(1e18))
	return strconv.FormatInt(id, 10)
}

// SetDebug enables performance timing output to stderr.
func (s *Scraper) SetDebug(enabled bool) *Scraper {
	debugPerf = enabled
	return s
}

// WithSearchDelay sets the minimum delay between search/hashtag API requests.
func (s *Scraper) WithSearchDelay(d time.Duration) *Scraper {
	s.searchDelay = d
	return s
}

// WithProfileDelay sets the minimum delay between user profile requests.
func (s *Scraper) WithProfileDelay(d time.Duration) *Scraper {
	s.profileDelay = d
	return s
}

// SetProxy configures an HTTP/HTTPS or SOCKS5 proxy for the HTTP client.
// Connection pooling and keep-alive settings are preserved.
func (s *Scraper) SetProxy(proxyAddr string) error {
	if proxyAddr == "" {
		s.client.Transport = defaultTransport()
		s.proxy = ""
		return nil
	}

	u, err := url.Parse(proxyAddr)
	if err != nil {
		return fmt.Errorf("parse proxy url: %w", err)
	}

	base := defaultTransport()

	switch u.Scheme {
	case "http", "https":
		base.Proxy = http.ProxyURL(u)
		s.client.Transport = base
	case "socks5":
		var auth *proxy.Auth
		if u.User != nil {
			pass, _ := u.User.Password()
			auth = &proxy.Auth{User: u.User.Username(), Password: pass}
		}
		dialer, err := proxy.SOCKS5("tcp", u.Host, auth, proxy.Direct)
		if err != nil {
			return fmt.Errorf("socks5 proxy: %w", err)
		}
		dc, ok := dialer.(proxy.ContextDialer)
		if !ok {
			return fmt.Errorf("socks5: context dialer not supported")
		}
		base.DialContext = dc.DialContext
		s.client.Transport = base
	default:
		return fmt.Errorf("unsupported proxy scheme: %s", u.Scheme)
	}

	s.proxy = proxyAddr
	return nil
}

// buildAPIParams returns the base query parameters required by TikTok's web API.
// These mimic a real Chrome browser session and are appended to every API request.
func (s *Scraper) buildAPIParams() url.Values {
	p := url.Values{}
	p.Set("aid", "1988")
	p.Set("app_language", "en")
	p.Set("app_name", "tiktok_web")
	p.Set("browser_language", "en-US")
	p.Set("browser_name", "Mozilla")
	p.Set("browser_online", "true")
	p.Set("browser_platform", "MacIntel")
	p.Set("browser_version", s.userAgent)
	p.Set("channel", "tiktok_web")
	p.Set("cookie_enabled", "true")
	p.Set("device_id", s.deviceID)
	p.Set("device_platform", "web_pc")
	p.Set("focus_state", "true")
	p.Set("history_len", strconv.Itoa(2+rand.IntN(8)))
	p.Set("is_fullscreen", "false")
	p.Set("is_page_visible", "true")
	p.Set("language", "en")
	p.Set("os", "mac")
	p.Set("priority_region", "")
	p.Set("referer", "")
	p.Set("region", "US")
	p.Set("screen_height", "1080")
	p.Set("screen_width", "1920")
	p.Set("tz_name", "America/New_York")
	p.Set("webcast_language", "en")
	if s.msToken != "" {
		p.Set("msToken", s.msToken)
	}
	return p
}

// doRequest builds and executes an HTTP request with standard TikTok headers.
// No built-in rate limiting — callers use waitForSearch or waitForProfile.
func (s *Scraper) doRequest(ctx context.Context, method, urlStr string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, urlStr, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", s.userAgent)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Referer", "https://www.tiktok.com/")
	req.Header.Set("Origin", "https://www.tiktok.com")

	// Chrome client hints — anti-bot systems check for these.
	req.Header.Set("Sec-Ch-Ua", `"Google Chrome";v="131", "Chromium";v="131", "Not_A Brand";v="24"`)
	req.Header.Set("Sec-Ch-Ua-Mobile", "?0")
	req.Header.Set("Sec-Ch-Ua-Platform", `"macOS"`)
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}

	// Capture fresh msToken from response — TikTok rotates it per request.
	s.extractMsToken(resp)

	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		resp.Body.Close()
		return nil, ErrRateLimited
	case http.StatusNotFound:
		resp.Body.Close()
		return nil, ErrNotFound
	}

	return resp, nil
}

// extractMsToken updates the cached msToken from response headers or cookies.
// TikTok sends a fresh token via X-Ms-Token header and Set-Cookie on every response.
func (s *Scraper) extractMsToken(resp *http.Response) {
	// Prefer X-Ms-Token header (always present when token rotates).
	if token := resp.Header.Get("X-Ms-Token"); token != "" {
		s.msToken = token
		return
	}

	// Fallback: extract from Set-Cookie.
	for _, c := range resp.Cookies() {
		if c.Name == "msToken" {
			s.msToken = c.Value
			return
		}
	}
}

// waitForSearch enforces rate limiting for search/hashtag API calls.
func (s *Scraper) waitForSearch() {
	s.searchMu.Lock()
	defer s.searchMu.Unlock()
	s.throttle(&s.lastSearch, s.searchDelay)
}

// waitForProfile enforces rate limiting for user profile lookups.
func (s *Scraper) waitForProfile() {
	s.profileMu.Lock()
	defer s.profileMu.Unlock()
	s.throttle(&s.lastProfile, s.profileDelay)
}

// throttle sleeps if needed to enforce min delay + jitter between requests.
func (s *Scraper) throttle(lastReq *time.Time, delay time.Duration) {
	if delay == 0 {
		return
	}
	start := time.Now()

	// First call — no previous request, skip the wait.
	if lastReq.IsZero() {
		*lastReq = start
		perfLog("throttle: first_call delay=%v skipped=true", delay)
		return
	}

	elapsed := start.Sub(*lastReq)
	jitter := time.Duration(rand.Int64N(int64(500 * time.Millisecond)))
	wait := delay + jitter - elapsed
	if wait > 0 {
		time.Sleep(wait)
	}
	*lastReq = time.Now()
	perfLog("throttle: delay=%v jitter=%v elapsed=%v slept=%v", delay, jitter, elapsed, time.Since(start))
}

// GetCookies returns the current session cookies for tiktok.com.
func (s *Scraper) GetCookies() []*http.Cookie {
	return s.client.Jar.Cookies(tiktokURL)
}

// SetCookies sets session cookies and extracts the msToken.
func (s *Scraper) SetCookies(cookies []*http.Cookie) {
	s.client.Jar.SetCookies(tiktokURL, cookies)
	for _, c := range cookies {
		if c.Name == "msToken" {
			s.msToken = c.Value
		}
	}
}

// SaveCookies writes session cookies to a JSON file.
func (s *Scraper) SaveCookies(path string) error {
	data, err := json.Marshal(s.GetCookies())
	if err != nil {
		return fmt.Errorf("marshal cookies: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}

// LoadCookies reads cookies from a JSON file and sets them on the client.
func (s *Scraper) LoadCookies(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read cookies file: %w", err)
	}
	var cookies []*http.Cookie
	if err := json.Unmarshal(data, &cookies); err != nil {
		return fmt.Errorf("unmarshal cookies: %w", err)
	}
	s.SetCookies(cookies)
	s.isLogged = true
	return nil
}

// IsLoggedIn reports whether the scraper has an active session.
func (s *Scraper) IsLoggedIn() bool {
	return s.isLogged
}

// Close releases all resources including the headless browser if running.
func (s *Scraper) Close() error {
	return s.closeBrowser()
}
