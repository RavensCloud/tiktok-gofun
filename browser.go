//go:build !unittest

package tiktok

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/go-rod/stealth"
)

// InitBrowser launches a headless Chrome instance with stealth mode.
// The browser stays open in the background for URL signing only.
func (s *Scraper) InitBrowser() error {
	return s.launchBrowser()
}

func (s *Scraper) launchBrowser() error {
	l := launcher.New().Headless(true)
	if s.proxy != "" {
		l = l.Proxy(s.proxy)
	}

	controlURL, err := l.Launch()
	if err != nil {
		return fmt.Errorf("launch browser: %w", err)
	}

	browser := rod.New().ControlURL(controlURL)
	if err := browser.Connect(); err != nil {
		return fmt.Errorf("connect browser: %w", err)
	}

	page, err := stealth.Page(browser)
	if err != nil {
		return fmt.Errorf("create stealth page: %w", err)
	}

	s.browser = browser
	s.page = page

	s.setupResourceBlocking()

	if err := s.page.Navigate(s.baseURL); err != nil {
		return fmt.Errorf("navigate to tiktok: %w", err)
	}
	if err := s.page.WaitStable(2 * time.Second); err != nil {
		return fmt.Errorf("wait for page stable: %w", err)
	}

	// Cache that signing is ready after initial page load.
	s.signingReady.Store(true)

	// Sync browser cookies (including fresh msToken) to the HTTP client.
	return s.syncCookiesFromBrowser()
}

func (s *Scraper) setupResourceBlocking() {
	router := s.browser.HijackRequests()
	blocked := []string{"*.css", "*.png", "*.jpg", "*.jpeg", "*.mp4", "*.woff*", "*.svg", "*analytics*"}
	for _, pattern := range blocked {
		router.MustAdd(pattern, func(ctx *rod.Hijack) {
			ctx.Response.Fail(proto.NetworkErrorReasonBlockedByClient)
		})
	}
	go router.Run()
}

// signURL calls TikTok's frontierSign JS to generate the X-Bogus signature.
// frontierSign returns an object like {"X-Bogus": "xxx"} — we append those
// params to the original URL.
// Caller must hold browserMu.
func (s *Scraper) signURL(rawURL string) (string, error) {
	if s.page == nil {
		return "", ErrBrowserNotReady
	}

	if err := s.ensureSigningReady(); err != nil {
		return "", fmt.Errorf("ensure signing ready: %w", err)
	}

	// Timeout the JS eval to avoid hanging forever.
	page := s.page.Timeout(5 * time.Second)

	// Returns the signed URL directly by appending params from frontierSign.
	result, err := page.Eval(`(url) => {
		if (typeof window.byted_acrawler === 'undefined') {
			throw new Error('signing function not available');
		}
		const params = window.byted_acrawler.frontierSign(url);
		if (typeof params === 'string') {
			return params;
		}
		// frontierSign returns an object {X-Bogus: "xxx", ...}
		const u = new URL(url);
		for (const [k, v] of Object.entries(params)) {
			u.searchParams.set(k, v);
		}
		return u.toString();
	}`, rawURL)
	if err != nil {
		// Mark signing as not ready so next call will reload.
		s.signingReady.Store(false)
		return "", fmt.Errorf("%w: %v", ErrSigningFailed, err)
	}

	return result.Value.String(), nil
}

// browserFetch signs a URL and fetches it inside the browser via JS fetch().
// This ensures the request uses the browser's TLS fingerprint, cookies, and
// session — avoiding detection from fingerprint mismatches between Go's
// net/http client and the browser that signed the URL.
// Caller must hold browserMu.
func (s *Scraper) browserFetch(rawURL string) ([]byte, error) {
	totalStart := time.Now()

	if s.page == nil {
		return nil, ErrBrowserNotReady
	}

	signingStart := time.Now()
	if err := s.ensureSigningReady(); err != nil {
		return nil, fmt.Errorf("ensure signing ready: %w", err)
	}
	perfLog("browserFetch: ensureSigningReady=%v", time.Since(signingStart))

	page := s.page.Timeout(15 * time.Second)

	// Sign the URL and fetch it in one JS call to keep everything consistent.
	evalStart := time.Now()
	result, err := page.Eval(`async (url) => {
		if (typeof window.byted_acrawler === 'undefined') {
			throw new Error('signing function not available');
		}
		const t0 = Date.now();
		const params = window.byted_acrawler.frontierSign(url);
		const signMs = Date.now() - t0;
		let signedUrl;
		if (typeof params === 'string') {
			signedUrl = params;
		} else {
			const u = new URL(url);
			for (const [k, v] of Object.entries(params)) {
				u.searchParams.set(k, v);
			}
			signedUrl = u.toString();
		}
		const t1 = Date.now();
		const resp = await fetch(signedUrl, {
			method: 'GET',
			credentials: 'include',
			headers: {
				'Accept': 'application/json, text/plain, */*',
			},
		});
		const fetchMs = Date.now() - t1;
		const t2 = Date.now();
		const text = await resp.text();
		const readMs = Date.now() - t2;
		return JSON.stringify({body: text, signMs, fetchMs, readMs});
	}`, rawURL)
	evalDur := time.Since(evalStart)
	if err != nil {
		s.signingReady.Store(false)
		perfLog("browserFetch: eval FAILED after %v", evalDur)
		return nil, fmt.Errorf("%w: %v", ErrSigningFailed, err)
	}

	// Parse timing from JS result.
	raw := result.Value.Str()
	var jsResult struct {
		Body    string `json:"body"`
		SignMs  int    `json:"signMs"`
		FetchMs int    `json:"fetchMs"`
		ReadMs  int    `json:"readMs"`
	}
	if err := json.Unmarshal([]byte(raw), &jsResult); err != nil {
		// Fallback: old format (plain text).
		perfLog("browserFetch: eval=%v total=%v (timing parse failed)", evalDur, time.Since(totalStart))
		if raw == "" {
			return nil, nil
		}
		return []byte(raw), nil
	}

	perfLog("browserFetch: js_sign=%dms js_fetch=%dms js_read=%dms eval=%v total=%v body=%d bytes",
		jsResult.SignMs, jsResult.FetchMs, jsResult.ReadMs, evalDur, time.Since(totalStart), len(jsResult.Body))

	if jsResult.Body == "" {
		return nil, nil
	}
	return []byte(jsResult.Body), nil
}

// ensureSigningReady checks if the signing JS is available, reloading only if
// a previous call failed (cached via atomic bool to avoid overhead per call).
func (s *Scraper) ensureSigningReady() error {
	if s.signingReady.Load() {
		return nil
	}

	result, err := s.page.Timeout(3 * time.Second).Eval(`() => typeof window.byted_acrawler !== 'undefined'`)
	if err != nil || !result.Value.Bool() {
		if err := s.page.Navigate(s.baseURL); err != nil {
			return fmt.Errorf("reload for signing: %w", err)
		}
		if err := s.page.WaitStable(2 * time.Second); err != nil {
			return fmt.Errorf("wait after reload: %w", err)
		}
	}

	s.signingReady.Store(true)
	return nil
}

func (s *Scraper) closeBrowser() error {
	if s.page != nil {
		if err := s.page.Close(); err != nil {
			return fmt.Errorf("close page: %w", err)
		}
		s.page = nil
	}
	if s.browser != nil {
		if err := s.browser.Close(); err != nil {
			return fmt.Errorf("close browser: %w", err)
		}
		s.browser = nil
	}
	return nil
}
