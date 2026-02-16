//go:build !unittest

package tiktok

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-rod/rod/lib/proto"
)

// Login automates TikTok login via the headless browser. After login,
// cookies are synced to the HTTP client for subsequent API requests.
func (s *Scraper) Login(username, password string) error {
	if s.browser == nil {
		if err := s.launchBrowser(); err != nil {
			return fmt.Errorf("login: %w", err)
		}
	}

	if err := s.page.Navigate("https://www.tiktok.com/login/phone-or-email/email"); err != nil {
		return fmt.Errorf("navigate to login: %w", err)
	}
	if err := s.page.WaitStable(2 * time.Second); err != nil {
		return fmt.Errorf("wait for login page: %w", err)
	}

	usernameInput, err := s.page.Element(`input[name="username"]`)
	if err != nil {
		return fmt.Errorf("find username input: %w", err)
	}
	if err := usernameInput.Input(username); err != nil {
		return fmt.Errorf("type username: %w", err)
	}

	passwordInput, err := s.page.Element(`input[type="password"]`)
	if err != nil {
		return fmt.Errorf("find password input: %w", err)
	}
	if err := passwordInput.Input(password); err != nil {
		return fmt.Errorf("type password: %w", err)
	}

	loginBtn, err := s.page.Element(`button[type="submit"]`)
	if err != nil {
		return fmt.Errorf("find login button: %w", err)
	}
	if err := loginBtn.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return fmt.Errorf("click login: %w", err)
	}

	if err := s.page.WaitStable(5 * time.Second); err != nil {
		return fmt.Errorf("wait after login: %w", err)
	}

	return s.syncCookiesFromBrowser()
}

// syncCookiesFromBrowser copies browser cookies to the HTTP client's cookie jar.
func (s *Scraper) syncCookiesFromBrowser() error {
	cookies, err := s.page.Cookies([]string{"https://www.tiktok.com"})
	if err != nil {
		return fmt.Errorf("get browser cookies: %w", err)
	}

	httpCookies := make([]*http.Cookie, 0, len(cookies))
	for _, c := range cookies {
		httpCookies = append(httpCookies, &http.Cookie{
			Name:    c.Name,
			Value:   c.Value,
			Domain:  c.Domain,
			Path:    c.Path,
			Expires: time.Unix(int64(c.Expires), 0),
		})
	}

	s.SetCookies(httpCookies)
	s.isLogged = true
	return nil
}

// LoginWithCookies loads saved cookies and initializes the browser for signing.
func (s *Scraper) LoginWithCookies(path string) error {
	if err := s.LoadCookies(path); err != nil {
		return fmt.Errorf("login with cookies: %w", err)
	}

	if s.browser == nil {
		if err := s.launchBrowser(); err != nil {
			return fmt.Errorf("init browser for signing: %w", err)
		}
	}

	// Set cookies on the browser page too so signing works with auth context.
	for _, c := range s.GetCookies() {
		if err := s.page.SetCookies([]*proto.NetworkCookieParam{{
			Name:   c.Name,
			Value:  c.Value,
			Domain: ".tiktok.com",
			Path:   "/",
		}}); err != nil {
			return fmt.Errorf("set browser cookie %q: %w", c.Name, err)
		}
	}

	return nil
}
