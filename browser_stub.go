//go:build unittest

package tiktok

import "fmt"

func (s *Scraper) InitBrowser() error {
	return fmt.Errorf("browser: %w (build tag: unittest)", ErrBrowserNotReady)
}

func (s *Scraper) launchBrowser() error {
	return fmt.Errorf("browser: %w (build tag: unittest)", ErrBrowserNotReady)
}

func (s *Scraper) setupResourceBlocking() {}

func (s *Scraper) signURL(rawURL string) (string, error) {
	if s.page == nil {
		return "", ErrBrowserNotReady
	}
	return "", ErrBrowserNotReady
}

func (s *Scraper) ensureSigningReady() error {
	if s.signingReady.Load() {
		return nil
	}
	return ErrBrowserNotReady
}

func (s *Scraper) closeBrowser() error {
	s.page = nil
	s.browser = nil
	return nil
}
