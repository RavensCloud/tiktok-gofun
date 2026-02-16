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
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-rod/rod"
	"golang.org/x/net/proxy"
)

const defaultUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

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
	}
	s.signFunc = s.signURL
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

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}

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
	elapsed := time.Since(*lastReq)
	jitter := time.Duration(rand.Int64N(int64(500 * time.Millisecond)))
	wait := delay + jitter - elapsed
	if wait > 0 {
		time.Sleep(wait)
	}
	*lastReq = time.Now()
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
