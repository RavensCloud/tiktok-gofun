package tiktok

import (
	"context"
	"fmt"
	"io"
	"time"
)

// GetUser fetches a TikTok user profile via SSR HTML parsing.
// This is pure HTTP — no browser or login required.
func (s *Scraper) GetUser(ctx context.Context, username string) (Author, error) {
	if username == "" {
		return Author{}, fmt.Errorf("get user: username is required")
	}

	totalStart := time.Now()
	profileURL := s.baseURL + "/@" + username

	delayStart := time.Now()
	s.waitForProfile()
	delayDur := time.Since(delayStart)

	httpStart := time.Now()
	resp, err := s.doRequest(ctx, "GET", profileURL, nil)
	if err != nil {
		return Author{}, fmt.Errorf("get user %q: %w", username, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Author{}, fmt.Errorf("read user page %q: %w", username, err)
	}
	httpDur := time.Since(httpStart)

	parseStart := time.Now()
	data, err := extractUniversalData(body)
	if err != nil {
		return Author{}, fmt.Errorf("parse user page %q: %w", username, err)
	}

	author, err := extractUserFromSSR(data)
	if err != nil {
		return Author{}, fmt.Errorf("extract user %q: %w", username, err)
	}
	parseDur := time.Since(parseStart)

	perfLog("GetUser: user=%s delay=%v http=%v parse=%v total=%v body=%d bytes",
		username, delayDur, httpDur, parseDur, time.Since(totalStart), len(body))

	return author, nil
}
