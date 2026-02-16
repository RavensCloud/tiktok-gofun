package tiktok

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// browserAPIRequest builds the full API URL and fetches it via the browser.
// The browser signs the URL (X-Bogus) and makes the HTTP request itself,
// ensuring the TLS fingerprint, cookies, and session are all consistent.
func (s *Scraper) browserAPIRequest(
	_ context.Context,
	path string,
	setParams func(p map[string]string),
) ([]byte, error) {
	totalStart := time.Now()

	params := s.buildAPIParams()

	// Apply caller-specific params.
	extra := make(map[string]string)
	setParams(extra)
	for k, v := range extra {
		params.Set(k, v)
	}

	rawURL := s.baseURL + path + "?" + params.Encode()
	buildDur := time.Since(totalStart)

	fetchStart := time.Now()
	s.browserMu.Lock()
	body, err := s.fetchFunc(rawURL)
	s.browserMu.Unlock()
	fetchDur := time.Since(fetchStart)

	perfLog("browserAPIRequest: path=%s build=%v fetch=%v total=%v", path, buildDur, fetchDur, time.Since(totalStart))

	if err != nil {
		return nil, fmt.Errorf("browser fetch: %w", err)
	}

	if len(body) == 0 {
		return nil, fmt.Errorf("%w: empty response", ErrInvalidResponse)
	}
	return body, nil
}

// SearchVideos searches TikTok for videos matching the keyword.
// Requires an initialized browser (InitBrowser) and authentication.
func (s *Scraper) SearchVideos(ctx context.Context, keyword string, limit int) ([]Video, error) {
	if keyword == "" {
		return nil, fmt.Errorf("search videos: keyword is required")
	}

	var allVideos []Video
	cursor := 0

	for len(allVideos) < limit {
		s.waitForSearch()

		videos, nextCursor, err := s.fetchSearch(ctx, keyword, cursor)
		if err != nil {
			return allVideos, fmt.Errorf("search videos %q: %w", keyword, err)
		}
		allVideos = append(allVideos, videos...)
		if nextCursor == 0 {
			break
		}
		cursor = nextCursor
	}

	if len(allVideos) > limit {
		allVideos = allVideos[:limit]
	}
	return allVideos, nil
}

func (s *Scraper) fetchSearch(ctx context.Context, keyword string, cursor int) ([]Video, int, error) {
	body, err := s.browserAPIRequest(ctx, "/api/search/item/full/", func(p map[string]string) {
		p["keyword"] = keyword
		p["count"] = "20"
		p["cursor"] = strconv.Itoa(cursor)
		p["from_page"] = "search"
	})
	if err != nil {
		return nil, 0, fmt.Errorf("search request: %w", err)
	}

	var result searchResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, 0, fmt.Errorf("decode search response (len %d): %w", len(body), err)
	}

	videos := make([]Video, 0, len(result.ItemList))
	for _, raw := range result.ItemList {
		videos = append(videos, parseVideo(raw))
	}

	nextCursor := 0
	if result.HasMore == 1 {
		nextCursor = result.Cursor
	}
	return videos, nextCursor, nil
}

// SearchByHashtag searches TikTok for videos under a specific hashtag.
// Requires an initialized browser and authentication.
func (s *Scraper) SearchByHashtag(ctx context.Context, hashtag string, limit int) ([]Video, error) {
	if hashtag == "" {
		return nil, fmt.Errorf("search by hashtag: hashtag is required")
	}

	challengeID, err := s.getChallengeID(ctx, hashtag)
	if err != nil {
		return nil, fmt.Errorf("search by hashtag %q: %w", hashtag, err)
	}

	var allVideos []Video
	cursor := 0

	for len(allVideos) < limit {
		s.waitForSearch()

		videos, nextCursor, err := s.fetchHashtagVideos(ctx, challengeID, cursor)
		if err != nil {
			return allVideos, fmt.Errorf("fetch hashtag videos %q: %w", hashtag, err)
		}
		allVideos = append(allVideos, videos...)
		if nextCursor == 0 {
			break
		}
		cursor = nextCursor
	}

	if len(allVideos) > limit {
		allVideos = allVideos[:limit]
	}
	return allVideos, nil
}

func (s *Scraper) getChallengeID(ctx context.Context, hashtag string) (string, error) {
	body, err := s.browserAPIRequest(ctx, "/api/challenge/detail/", func(p map[string]string) {
		p["challengeName"] = hashtag
	})
	if err != nil {
		return "", fmt.Errorf("challenge detail: %w", err)
	}

	var result challengeDetailResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("decode challenge detail: %w", err)
	}

	if result.ChallengeInfo.Challenge.ID == "" {
		return "", fmt.Errorf("%w: challenge %q", ErrNotFound, hashtag)
	}
	return result.ChallengeInfo.Challenge.ID, nil
}

func (s *Scraper) fetchHashtagVideos(ctx context.Context, challengeID string, cursor int) ([]Video, int, error) {
	body, err := s.browserAPIRequest(ctx, "/api/challenge/item_list/", func(p map[string]string) {
		p["challengeID"] = challengeID
		p["count"] = "35"
		p["cursor"] = strconv.Itoa(cursor)
	})
	if err != nil {
		return nil, 0, fmt.Errorf("hashtag videos: %w", err)
	}

	var result challengeItemListResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, 0, fmt.Errorf("decode hashtag videos: %w", err)
	}

	videos := make([]Video, 0, len(result.ItemList))
	for _, raw := range result.ItemList {
		videos = append(videos, parseVideo(raw))
	}

	nextCursor := 0
	if result.HasMore {
		nextCursor = result.Cursor
	}
	return videos, nextCursor, nil
}
