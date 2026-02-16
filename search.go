package tiktok

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// SearchVideos searches TikTok for videos matching the keyword.
// Requires an initialized browser (InitBrowser) and authentication.
func (s *Scraper) SearchVideos(ctx context.Context, keyword string, limit int) ([]Video, error) {
	if keyword == "" {
		return nil, fmt.Errorf("search videos: keyword is required")
	}

	var allVideos []Video
	cursor := 0

	for len(allVideos) < limit {
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
	rawURL := fmt.Sprintf(
		"%s/api/search/item/full/?keyword=%s&count=20&cursor=%d&from_page=search",
		s.baseURL, url.QueryEscape(keyword), cursor,
	)

	// Sign URL via browser JS (~50ms). Mutex protects single-threaded browser page.
	s.browserMu.Lock()
	signedURL, err := s.signFunc(rawURL)
	s.browserMu.Unlock()
	if err != nil {
		return nil, 0, fmt.Errorf("sign search url: %w", err)
	}

	// Rate limit before the HTTP call, not the signing.
	s.waitForSearch()

	resp, err := s.doRequest(ctx, "GET", signedURL, nil)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	var result searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, 0, fmt.Errorf("decode search response: %w", err)
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
	rawURL := fmt.Sprintf(
		"%s/api/challenge/detail/?challengeName=%s",
		s.baseURL, url.QueryEscape(hashtag),
	)

	s.browserMu.Lock()
	signedURL, err := s.signFunc(rawURL)
	s.browserMu.Unlock()
	if err != nil {
		return "", fmt.Errorf("sign challenge url: %w", err)
	}

	s.waitForSearch()

	resp, err := s.doRequest(ctx, "GET", signedURL, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result challengeDetailResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode challenge detail: %w", err)
	}

	if result.ChallengeInfo.Challenge.ID == "" {
		return "", fmt.Errorf("%w: challenge %q", ErrNotFound, hashtag)
	}
	return result.ChallengeInfo.Challenge.ID, nil
}

func (s *Scraper) fetchHashtagVideos(ctx context.Context, challengeID string, cursor int) ([]Video, int, error) {
	rawURL := fmt.Sprintf(
		"%s/api/challenge/item_list/?challengeID=%s&count=35&cursor=%s",
		s.baseURL, url.QueryEscape(challengeID), strconv.Itoa(cursor),
	)

	s.browserMu.Lock()
	signedURL, err := s.signFunc(rawURL)
	s.browserMu.Unlock()
	if err != nil {
		return nil, 0, fmt.Errorf("sign hashtag url: %w", err)
	}

	s.waitForSearch()

	resp, err := s.doRequest(ctx, "GET", signedURL, nil)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	var result challengeItemListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
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
