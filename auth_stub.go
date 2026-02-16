//go:build unittest

package tiktok

import "fmt"

func (s *Scraper) Login(username, password string) error {
	return fmt.Errorf("login: %w (build tag: unittest)", ErrBrowserNotReady)
}

func (s *Scraper) syncCookiesFromBrowser() error {
	return fmt.Errorf("sync cookies: %w (build tag: unittest)", ErrBrowserNotReady)
}

func (s *Scraper) LoginWithCookies(path string) error {
	if err := s.LoadCookies(path); err != nil {
		return fmt.Errorf("login with cookies: %w", err)
	}
	return nil
}
