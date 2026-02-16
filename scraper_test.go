package tiktok

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// ssrPage returns an HTML page with __UNIVERSAL_DATA_FOR_REHYDRATION__ embedded.
func ssrPage(username, id string, followers int) string {
	return `<html><head></head><body>` +
		`<script id="__UNIVERSAL_DATA_FOR_REHYDRATION__" type="application/json">` +
		`{"__DEFAULT_SCOPE__":{"webapp.user-detail":{"userInfo":{"user":{"id":"` + id + `","uniqueId":"` + username + `","nickname":"Test","avatarLarger":"https://img.tiktok.com/avatar.jpg","signature":"test bio","verified":true,"secUid":"sec123"},"stats":{"followerCount":` + fmt.Sprintf("%d", followers) + `,"followingCount":50,"heart":5000,"heartCount":5000,"videoCount":42,"diggCount":100}}}}}` +
		`</script></body></html>`
}

// searchJSON returns a valid search API response body.
func searchJSON(count int, hasMore bool, cursor int) string {
	items := make([]string, 0, count)
	for i := range count {
		items = append(items, fmt.Sprintf(`{
			"id": "%d",
			"desc": "video %d",
			"createTime": 1706000000,
			"author": {"uniqueId": "user%d", "id": "%d", "nickname": "User", "verified": false},
			"stats": {"playCount": %d, "diggCount": 50, "shareCount": 10, "commentCount": 5}
		}`, 1000+i, i, i, 200+i, (i+1)*1000))
	}
	return fmt.Sprintf(`{"item_list": [%s], "has_more": %v, "cursor": %d}`,
		strings.Join(items, ","), hasMore, cursor)
}

// challengeDetailJSON returns a valid challenge detail API response body.
func challengeDetailJSON(id, title string) string {
	return fmt.Sprintf(`{"challengeInfo":{"challenge":{"id":"%s","title":"%s","desc":"desc"},"stats":{"videoCount":50000,"viewCount":1000000000}}}`, id, title)
}

// challengeItemsJSON returns a valid challenge item_list API response body.
func challengeItemsJSON(count int, hasMore bool, cursor int) string {
	items := make([]string, 0, count)
	for i := range count {
		items = append(items, fmt.Sprintf(`{
			"id": "%d",
			"desc": "hashtag video %d",
			"createTime": 1706000000,
			"author": {"uniqueId": "huser%d", "id": "%d", "nickname": "HUser", "verified": false},
			"stats": {"playCount": %d, "diggCount": 30, "shareCount": 5, "commentCount": 2}
		}`, 3000+i, i, i, 400+i, (i+1)*500))
	}
	return fmt.Sprintf(`{"itemList": [%s], "hasMore": %v, "cursor": %d}`,
		strings.Join(items, ","), hasMore, cursor)
}

// newMockScraper creates a Scraper pointing at the given test server with zero
// delays and a no-op sign function (returns URL as-is).
func newMockScraper(serverURL string) *Scraper {
	s := New().WithSearchDelay(0).WithProfileDelay(0)
	s.baseURL = serverURL
	s.signFunc = func(rawURL string) (string, error) { return rawURL, nil }
	return s
}

func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0600)
}

// ---------------------------------------------------------------------------
// Scraper construction tests
// ---------------------------------------------------------------------------

func TestNew(t *testing.T) {
	t.Parallel()
	s := New()

	if s.client == nil {
		t.Fatal("expected http client to be initialized")
	}
	if s.client.Jar == nil {
		t.Fatal("expected cookie jar to be initialized")
	}
	if s.userAgent != defaultUserAgent {
		t.Errorf("expected default user agent, got %q", s.userAgent)
	}
	if s.searchDelay != 2*time.Second {
		t.Errorf("expected 2s search delay, got %v", s.searchDelay)
	}
	if s.profileDelay != 1*time.Second {
		t.Errorf("expected 1s profile delay, got %v", s.profileDelay)
	}
	if s.IsLoggedIn() {
		t.Error("expected not logged in")
	}
	if s.baseURL != "https://www.tiktok.com" {
		t.Errorf("expected default baseURL, got %q", s.baseURL)
	}
	if s.signFunc == nil {
		t.Fatal("expected signFunc to be initialized")
	}
}

func TestWithSearchDelay(t *testing.T) {
	t.Parallel()
	s := New().WithSearchDelay(5 * time.Second)
	if s.searchDelay != 5*time.Second {
		t.Errorf("expected 5s search delay, got %v", s.searchDelay)
	}
}

func TestWithProfileDelay(t *testing.T) {
	t.Parallel()
	s := New().WithProfileDelay(500 * time.Millisecond)
	if s.profileDelay != 500*time.Millisecond {
		t.Errorf("expected 500ms profile delay, got %v", s.profileDelay)
	}
}

func TestSetProxy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		addr    string
		wantErr bool
	}{
		{"empty resets", "", false},
		{"http proxy", "http://proxy.example.com:8080", false},
		{"https proxy", "https://proxy.example.com:8080", false},
		{"socks5 proxy", "socks5://user:pass@proxy.example.com:1080", false},
		{"unsupported scheme", "ftp://proxy.example.com", true},
		{"invalid url", "://bad", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := New()
			err := s.SetProxy(tt.addr)
			if (err != nil) != tt.wantErr {
				t.Errorf("SetProxy(%q) error = %v, wantErr %v", tt.addr, err, tt.wantErr)
			}
			if err == nil && tt.addr != "" {
				if s.proxy != tt.addr {
					t.Errorf("expected proxy %q, got %q", tt.addr, s.proxy)
				}
			}
		})
	}
}

func TestSetProxy_EmptyResetsTransport(t *testing.T) {
	t.Parallel()
	s := New()
	_ = s.SetProxy("http://proxy.example.com:8080")
	if s.proxy != "http://proxy.example.com:8080" {
		t.Fatal("proxy not set")
	}
	_ = s.SetProxy("")
	if s.proxy != "" {
		t.Errorf("expected empty proxy after reset, got %q", s.proxy)
	}
}

// ---------------------------------------------------------------------------
// doRequest tests (with httptest)
// ---------------------------------------------------------------------------

func TestDoRequest_Success(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != defaultUserAgent {
			t.Errorf("missing user-agent header")
		}
		if r.Header.Get("Referer") != "https://www.tiktok.com/" {
			t.Errorf("missing referer header")
		}
		if r.Header.Get("Accept-Language") != "en-US,en;q=0.9" {
			t.Errorf("missing accept-language header")
		}
		if r.Header.Get("Origin") != "https://www.tiktok.com" {
			t.Errorf("missing origin header")
		}
		if r.Header.Get("Accept") != "application/json, text/plain, */*" {
			t.Errorf("missing accept header")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	s := New().WithProfileDelay(0).WithSearchDelay(0)
	resp, err := s.doRequest(context.Background(), "GET", srv.URL+"/test", nil)
	if err != nil {
		t.Fatalf("doRequest: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestDoRequest_RateLimited(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	s := New().WithProfileDelay(0).WithSearchDelay(0)
	_, err := s.doRequest(context.Background(), "GET", srv.URL, nil)
	if !errors.Is(err, ErrRateLimited) {
		t.Errorf("expected ErrRateLimited, got %v", err)
	}
}

func TestDoRequest_NotFound(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	s := New().WithProfileDelay(0).WithSearchDelay(0)
	_, err := s.doRequest(context.Background(), "GET", srv.URL, nil)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestDoRequest_ContextCanceled(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer srv.Close()

	s := New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.doRequest(ctx, "GET", srv.URL, nil)
	if err == nil {
		t.Fatal("expected error from canceled context")
	}
}

func TestDoRequest_OtherStatusCodes(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	s := New().WithProfileDelay(0)
	resp, err := s.doRequest(context.Background(), "GET", srv.URL, nil)
	if err != nil {
		t.Fatalf("expected no error for 500, got %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Rate limiting tests
// ---------------------------------------------------------------------------

func TestThrottle_ZeroDelay(t *testing.T) {
	t.Parallel()
	s := New().WithSearchDelay(0).WithProfileDelay(0)

	start := time.Now()
	s.waitForSearch()
	s.waitForSearch()
	s.waitForProfile()
	elapsed := time.Since(start)

	if elapsed > 100*time.Millisecond {
		t.Errorf("zero delay should be instant, took %v", elapsed)
	}
}

func TestThrottle_EnforcesMinDelay(t *testing.T) {
	t.Parallel()
	s := New().WithSearchDelay(100 * time.Millisecond).WithProfileDelay(0)

	s.waitForSearch()
	start := time.Now()
	s.waitForSearch()
	elapsed := time.Since(start)

	if elapsed < 100*time.Millisecond {
		t.Errorf("expected at least 100ms wait, got %v", elapsed)
	}
}

func TestThrottle_ProfileIndependentFromSearch(t *testing.T) {
	t.Parallel()
	s := New().WithSearchDelay(200 * time.Millisecond).WithProfileDelay(0)

	s.waitForSearch()
	start := time.Now()
	s.waitForProfile()
	elapsed := time.Since(start)

	if elapsed > 50*time.Millisecond {
		t.Errorf("profile should not wait for search delay, took %v", elapsed)
	}
}

func TestThrottle_ProfileDelay(t *testing.T) {
	t.Parallel()
	s := New().WithSearchDelay(0).WithProfileDelay(100 * time.Millisecond)

	s.waitForProfile()
	start := time.Now()
	s.waitForProfile()
	elapsed := time.Since(start)

	if elapsed < 100*time.Millisecond {
		t.Errorf("expected at least 100ms profile wait, got %v", elapsed)
	}
}

// ---------------------------------------------------------------------------
// GetUser tests (full pipeline with mock server)
// ---------------------------------------------------------------------------

func TestGetUser_Success(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username := strings.TrimPrefix(r.URL.Path, "/@")
		w.Write([]byte(ssrPage(username, "123", 5000)))
	}))
	defer srv.Close()

	s := newMockScraper(srv.URL)

	author, err := s.GetUser(context.Background(), "testuser")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}

	if author.Username != "testuser" {
		t.Errorf("expected username testuser, got %q", author.Username)
	}
	if author.ID != "123" {
		t.Errorf("expected ID 123, got %q", author.ID)
	}
	if author.FollowerCount != 5000 {
		t.Errorf("expected 5000 followers, got %d", author.FollowerCount)
	}
	if author.FollowingCount != 50 {
		t.Errorf("expected 50 following, got %d", author.FollowingCount)
	}
	if author.VideoCount != 42 {
		t.Errorf("expected 42 videos, got %d", author.VideoCount)
	}
	if !author.Verified {
		t.Error("expected verified=true")
	}
	if author.Bio != "test bio" {
		t.Errorf("expected bio 'test bio', got %q", author.Bio)
	}
}

func TestGetUser_EmptyUsername(t *testing.T) {
	t.Parallel()
	s := New()
	_, err := s.GetUser(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty username")
	}
}

func TestGetUser_NotFound(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	s := newMockScraper(srv.URL)
	_, err := s.GetUser(context.Background(), "noone")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestGetUser_InvalidSSR(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`<html><body>no SSR data here</body></html>`))
	}))
	defer srv.Close()

	s := newMockScraper(srv.URL)
	_, err := s.GetUser(context.Background(), "testuser")
	if !errors.Is(err, ErrInvalidResponse) {
		t.Errorf("expected ErrInvalidResponse, got %v", err)
	}
}

func TestGetUser_MissingUserInSSR(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Valid SSR structure but empty user data.
		w.Write([]byte(`<html><body>` +
			`<script id="__UNIVERSAL_DATA_FOR_REHYDRATION__" type="application/json">` +
			`{"__DEFAULT_SCOPE__":{"webapp.user-detail":{"userInfo":{"user":{},"stats":{}}}}}` +
			`</script></body></html>`))
	}))
	defer srv.Close()

	s := newMockScraper(srv.URL)
	_, err := s.GetUser(context.Background(), "nobody")
	if err == nil {
		t.Fatal("expected error for empty user in SSR")
	}
}

func TestGetUser_ServerError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	s := newMockScraper(srv.URL)
	_, err := s.GetUser(context.Background(), "testuser")
	if !errors.Is(err, ErrRateLimited) {
		t.Errorf("expected ErrRateLimited, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// SearchVideos tests (full pipeline with mock server)
// ---------------------------------------------------------------------------

func TestSearchVideos_Success(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(searchJSON(5, false, 0)))
	}))
	defer srv.Close()

	s := newMockScraper(srv.URL)

	videos, err := s.SearchVideos(context.Background(), "bonk", 10)
	if err != nil {
		t.Fatalf("SearchVideos: %v", err)
	}
	if len(videos) != 5 {
		t.Fatalf("expected 5 videos, got %d", len(videos))
	}
	if videos[0].Views != 1000 {
		t.Errorf("expected 1000 views on first video, got %d", videos[0].Views)
	}
}

func TestSearchVideos_Pagination(t *testing.T) {
	t.Parallel()
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		cursor := r.URL.Query().Get("cursor")
		switch cursor {
		case "0":
			w.Write([]byte(searchJSON(10, true, 10)))
		case "10":
			w.Write([]byte(searchJSON(5, false, 0)))
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	s := newMockScraper(srv.URL)

	videos, err := s.SearchVideos(context.Background(), "crypto", 50)
	if err != nil {
		t.Fatalf("SearchVideos: %v", err)
	}
	if len(videos) != 15 {
		t.Fatalf("expected 15 videos (10+5), got %d", len(videos))
	}
	if callCount != 2 {
		t.Errorf("expected 2 API calls, got %d", callCount)
	}
}

func TestSearchVideos_LimitTruncation(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(searchJSON(20, false, 0)))
	}))
	defer srv.Close()

	s := newMockScraper(srv.URL)

	videos, err := s.SearchVideos(context.Background(), "bonk", 5)
	if err != nil {
		t.Fatalf("SearchVideos: %v", err)
	}
	if len(videos) != 5 {
		t.Fatalf("expected 5 videos after truncation, got %d", len(videos))
	}
}

func TestSearchVideos_EmptyKeyword(t *testing.T) {
	t.Parallel()
	s := New()
	_, err := s.SearchVideos(context.Background(), "", 10)
	if err == nil {
		t.Fatal("expected error for empty keyword")
	}
}

func TestSearchVideos_NoBrowser(t *testing.T) {
	t.Parallel()
	s := New()
	_, err := s.SearchVideos(context.Background(), "bonk", 10)
	if !errors.Is(err, ErrBrowserNotReady) {
		t.Errorf("expected ErrBrowserNotReady, got %v", err)
	}
}

func TestSearchVideos_SignError(t *testing.T) {
	t.Parallel()
	s := New().WithSearchDelay(0)
	s.signFunc = func(rawURL string) (string, error) {
		return "", fmt.Errorf("sign failed: %w", ErrSigningFailed)
	}

	_, err := s.SearchVideos(context.Background(), "bonk", 10)
	if !errors.Is(err, ErrSigningFailed) {
		t.Errorf("expected ErrSigningFailed, got %v", err)
	}
}

func TestSearchVideos_ServerError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	s := newMockScraper(srv.URL)
	_, err := s.SearchVideos(context.Background(), "bonk", 10)
	if !errors.Is(err, ErrRateLimited) {
		t.Errorf("expected ErrRateLimited, got %v", err)
	}
}

func TestSearchVideos_InvalidJSON(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	s := newMockScraper(srv.URL)
	_, err := s.SearchVideos(context.Background(), "bonk", 10)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// ---------------------------------------------------------------------------
// SearchByHashtag tests (full pipeline with mock server)
// ---------------------------------------------------------------------------

func TestSearchByHashtag_Success(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/api/challenge/detail"):
			w.Write([]byte(challengeDetailJSON("789", "bonk")))
		case strings.Contains(r.URL.Path, "/api/challenge/item_list"):
			w.Write([]byte(challengeItemsJSON(5, false, 0)))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	s := newMockScraper(srv.URL)

	videos, err := s.SearchByHashtag(context.Background(), "bonk", 10)
	if err != nil {
		t.Fatalf("SearchByHashtag: %v", err)
	}
	if len(videos) != 5 {
		t.Fatalf("expected 5 videos, got %d", len(videos))
	}
	if videos[0].Views != 500 {
		t.Errorf("expected 500 views on first video, got %d", videos[0].Views)
	}
}

func TestSearchByHashtag_Pagination(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/api/challenge/detail"):
			w.Write([]byte(challengeDetailJSON("789", "bonk")))
		case strings.Contains(r.URL.Path, "/api/challenge/item_list"):
			cursor := r.URL.Query().Get("cursor")
			switch cursor {
			case "0":
				w.Write([]byte(challengeItemsJSON(10, true, 10)))
			case "10":
				w.Write([]byte(challengeItemsJSON(3, false, 0)))
			}
		}
	}))
	defer srv.Close()

	s := newMockScraper(srv.URL)

	videos, err := s.SearchByHashtag(context.Background(), "bonk", 50)
	if err != nil {
		t.Fatalf("SearchByHashtag: %v", err)
	}
	if len(videos) != 13 {
		t.Fatalf("expected 13 videos (10+3), got %d", len(videos))
	}
}

func TestSearchByHashtag_LimitTruncation(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/api/challenge/detail"):
			w.Write([]byte(challengeDetailJSON("789", "bonk")))
		case strings.Contains(r.URL.Path, "/api/challenge/item_list"):
			w.Write([]byte(challengeItemsJSON(20, false, 0)))
		}
	}))
	defer srv.Close()

	s := newMockScraper(srv.URL)

	videos, err := s.SearchByHashtag(context.Background(), "bonk", 5)
	if err != nil {
		t.Fatalf("SearchByHashtag: %v", err)
	}
	if len(videos) != 5 {
		t.Fatalf("expected 5 videos after truncation, got %d", len(videos))
	}
}

func TestSearchByHashtag_EmptyHashtag(t *testing.T) {
	t.Parallel()
	s := New()
	_, err := s.SearchByHashtag(context.Background(), "", 10)
	if err == nil {
		t.Fatal("expected error for empty hashtag")
	}
}

func TestSearchByHashtag_NoBrowser(t *testing.T) {
	t.Parallel()
	s := New()
	_, err := s.SearchByHashtag(context.Background(), "bonk", 10)
	if !errors.Is(err, ErrBrowserNotReady) {
		t.Errorf("expected ErrBrowserNotReady, got %v", err)
	}
}

func TestSearchByHashtag_ChallengeNotFound(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return empty challenge.
		w.Write([]byte(`{"challengeInfo":{"challenge":{"id":"","title":""},"stats":{}}}`))
	}))
	defer srv.Close()

	s := newMockScraper(srv.URL)
	_, err := s.SearchByHashtag(context.Background(), "nonexistent", 10)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestSearchByHashtag_ChallengeDetailServerError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	s := newMockScraper(srv.URL)
	_, err := s.SearchByHashtag(context.Background(), "bonk", 10)
	if !errors.Is(err, ErrRateLimited) {
		t.Errorf("expected ErrRateLimited, got %v", err)
	}
}

func TestSearchByHashtag_ItemListServerError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/api/challenge/detail"):
			w.Write([]byte(challengeDetailJSON("789", "bonk")))
		case strings.Contains(r.URL.Path, "/api/challenge/item_list"):
			w.WriteHeader(http.StatusTooManyRequests)
		}
	}))
	defer srv.Close()

	s := newMockScraper(srv.URL)
	_, err := s.SearchByHashtag(context.Background(), "bonk", 10)
	if !errors.Is(err, ErrRateLimited) {
		t.Errorf("expected ErrRateLimited, got %v", err)
	}
}

func TestSearchByHashtag_InvalidChallengeJSON(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	s := newMockScraper(srv.URL)
	_, err := s.SearchByHashtag(context.Background(), "bonk", 10)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// ---------------------------------------------------------------------------
// Cookie management tests
// ---------------------------------------------------------------------------

func TestGetSetCookies(t *testing.T) {
	t.Parallel()
	s := New()

	cookies := []*http.Cookie{
		{Name: "sessionid", Value: "abc123"},
		{Name: "msToken", Value: "token456"},
	}
	s.SetCookies(cookies)

	if s.msToken != "token456" {
		t.Errorf("expected msToken=token456, got %q", s.msToken)
	}

	got := s.GetCookies()
	if len(got) < 2 {
		t.Fatalf("expected at least 2 cookies, got %d", len(got))
	}
}

func TestSetCookies_NoMsToken(t *testing.T) {
	t.Parallel()
	s := New()
	s.SetCookies([]*http.Cookie{
		{Name: "sessionid", Value: "abc123"},
	})
	if s.msToken != "" {
		t.Errorf("expected empty msToken, got %q", s.msToken)
	}
}

func TestSaveLoadCookies(t *testing.T) {
	t.Parallel()
	s := New()
	s.SetCookies([]*http.Cookie{
		{Name: "sessionid", Value: "abc123"},
		{Name: "msToken", Value: "token456"},
	})

	path := filepath.Join(t.TempDir(), "cookies.json")

	if err := s.SaveCookies(path); err != nil {
		t.Fatalf("SaveCookies: %v", err)
	}

	s2 := New()
	if err := s2.LoadCookies(path); err != nil {
		t.Fatalf("LoadCookies: %v", err)
	}

	if s2.msToken != "token456" {
		t.Errorf("expected msToken=token456 after load, got %q", s2.msToken)
	}
	if !s2.IsLoggedIn() {
		t.Error("expected IsLoggedIn after LoadCookies")
	}
}

func TestSaveCookies_InvalidPath(t *testing.T) {
	t.Parallel()
	s := New()
	err := s.SaveCookies("/nonexistent/dir/cookies.json")
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
}

func TestLoadCookies_FileNotFound(t *testing.T) {
	t.Parallel()
	s := New()
	err := s.LoadCookies("/nonexistent/path/cookies.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadCookies_InvalidJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := writeFile(path, []byte(`not json`)); err != nil {
		t.Fatalf("write: %v", err)
	}

	s := New()
	err := s.LoadCookies(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// ---------------------------------------------------------------------------
// Close / cleanup tests
// ---------------------------------------------------------------------------

func TestClose_NilBrowser(t *testing.T) {
	t.Parallel()
	s := New()
	if err := s.Close(); err != nil {
		t.Errorf("Close on nil browser should not error, got %v", err)
	}
}

func TestClose_CalledTwice(t *testing.T) {
	t.Parallel()
	s := New()
	if err := s.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Sentinel errors tests
// ---------------------------------------------------------------------------

func TestSentinelErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
	}{
		{"ErrRateLimited", ErrRateLimited},
		{"ErrNotFound", ErrNotFound},
		{"ErrAuthRequired", ErrAuthRequired},
		{"ErrCaptcha", ErrCaptcha},
		{"ErrSigningFailed", ErrSigningFailed},
		{"ErrBrowserNotReady", ErrBrowserNotReady},
		{"ErrInvalidResponse", ErrInvalidResponse},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			wrapped := fmt.Errorf("outer: %w", tt.err)
			if !errors.Is(wrapped, tt.err) {
				t.Errorf("errors.Is failed for %v", tt.err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// SSR parsing tests
// ---------------------------------------------------------------------------

func TestExtractUniversalData(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		html     string
		wantUser string
		wantErr  bool
	}{
		{
			name:     "valid ssr data",
			html:     ssrPage("testuser", "123", 1000),
			wantUser: "testuser",
		},
		{
			name:    "missing script tag",
			html:    `<html><body>no data here</body></html>`,
			wantErr: true,
		},
		{
			name:    "malformed json",
			html:    `<script id="__UNIVERSAL_DATA_FOR_REHYDRATION__" type="application/json">{bad json}</script>`,
			wantErr: true,
		},
		{
			name:    "empty body",
			html:    "",
			wantErr: true,
		},
		{
			name:    "missing closing script tag",
			html:    `<script id="__UNIVERSAL_DATA_FOR_REHYDRATION__" type="application/json">{"data": true}`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data, err := extractUniversalData([]byte(tt.html))
			if (err != nil) != tt.wantErr {
				t.Errorf("extractUniversalData() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && data.DefaultScope.UserDetail.UserInfo.User.UniqueID != tt.wantUser {
				t.Errorf("expected user %q, got %q",
					tt.wantUser, data.DefaultScope.UserDetail.UserInfo.User.UniqueID)
			}
		})
	}
}

func TestExtractUserFromSSR(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		data    universalData
		wantID  string
		wantErr bool
	}{
		{
			name: "valid user data",
			data: universalData{
				DefaultScope: defaultScope{
					UserDetail: userDetailWrapper{
						UserInfo: rawUserInfo{
							User: rawUserDetail{
								ID:           "123",
								UniqueID:     "testuser",
								Nickname:     "Test User",
								AvatarLarger: "https://img.tiktok.com/avatar.jpg",
								Signature:    "my bio",
								Verified:     true,
							},
							Stats: rawUserStats{
								FollowerCount:  1000,
								FollowingCount: 50,
								VideoCount:     42,
							},
						},
					},
				},
			},
			wantID: "123",
		},
		{
			name:    "missing user data",
			data:    universalData{},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			author, err := extractUserFromSSR(tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractUserFromSSR() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && author.ID != tt.wantID {
				t.Errorf("expected ID %q, got %q", tt.wantID, author.ID)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Conversion function tests
// ---------------------------------------------------------------------------

func TestParseVideo(t *testing.T) {
	t.Parallel()
	raw := rawVideo{
		ID:         "7340000000000",
		Desc:       "Test video #bonk",
		CreateTime: 1706000000,
		Author: rawAuthor{
			UniqueID: "testuser",
			ID:       "700000000",
		},
		Stats: rawStats{
			PlayCount:    150000,
			DiggCount:    8500,
			ShareCount:   1200,
			CommentCount: 340,
		},
	}

	v := parseVideo(raw)

	if v.ID != "7340000000000" {
		t.Errorf("expected ID 7340000000000, got %s", v.ID)
	}
	if v.Username != "testuser" {
		t.Errorf("expected username testuser, got %s", v.Username)
	}
	if v.Views != 150000 {
		t.Errorf("expected 150000 views, got %d", v.Views)
	}
	if v.Likes != 8500 {
		t.Errorf("expected 8500 likes, got %d", v.Likes)
	}
	if v.Comments != 340 {
		t.Errorf("expected 340 comments, got %d", v.Comments)
	}
	if v.Shares != 1200 {
		t.Errorf("expected 1200 shares, got %d", v.Shares)
	}
	if v.AuthorID != "700000000" {
		t.Errorf("expected authorID 700000000, got %s", v.AuthorID)
	}
	if v.Description != "Test video #bonk" {
		t.Errorf("expected description, got %q", v.Description)
	}
	expected := time.Unix(1706000000, 0)
	if !v.CreatedAt.Equal(expected) {
		t.Errorf("expected CreatedAt %v, got %v", expected, v.CreatedAt)
	}
}

func TestParseAuthor(t *testing.T) {
	t.Parallel()
	raw := rawUserInfo{
		User: rawUserDetail{
			ID:           "700000000",
			UniqueID:     "testuser",
			AvatarLarger: "https://img.tiktok.com/avatar.jpg",
			Signature:    "my bio text",
			Verified:     true,
		},
		Stats: rawUserStats{
			FollowerCount:  15000,
			FollowingCount: 200,
			VideoCount:     120,
		},
	}

	a := parseAuthor(raw)

	if a.ID != "700000000" {
		t.Errorf("expected ID 700000000, got %s", a.ID)
	}
	if a.Username != "testuser" {
		t.Errorf("expected username testuser, got %s", a.Username)
	}
	if a.FollowerCount != 15000 {
		t.Errorf("expected 15000 followers, got %d", a.FollowerCount)
	}
	if a.FollowingCount != 200 {
		t.Errorf("expected 200 following, got %d", a.FollowingCount)
	}
	if a.VideoCount != 120 {
		t.Errorf("expected 120 videos, got %d", a.VideoCount)
	}
	if a.Bio != "my bio text" {
		t.Errorf("expected bio %q, got %q", "my bio text", a.Bio)
	}
	if !a.Verified {
		t.Error("expected verified=true")
	}
	if a.AvatarURL != "https://img.tiktok.com/avatar.jpg" {
		t.Errorf("expected avatar url, got %q", a.AvatarURL)
	}
}

// ---------------------------------------------------------------------------
// JSON deserialization tests
// ---------------------------------------------------------------------------

func TestSearchResponseDeserialization(t *testing.T) {
	t.Parallel()
	raw := searchJSON(2, true, 20)

	var resp searchResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(resp.ItemList) != 2 {
		t.Fatalf("expected 2 items, got %d", len(resp.ItemList))
	}
	if !resp.HasMore {
		t.Error("expected has_more=true")
	}
	if resp.Cursor != 20 {
		t.Errorf("expected cursor=20, got %d", resp.Cursor)
	}
}

func TestChallengeDetailDeserialization(t *testing.T) {
	t.Parallel()
	raw := challengeDetailJSON("456", "bonk")

	var resp challengeDetailResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.ChallengeInfo.Challenge.ID != "456" {
		t.Errorf("expected challenge ID 456, got %s", resp.ChallengeInfo.Challenge.ID)
	}
	if resp.ChallengeInfo.Challenge.Title != "bonk" {
		t.Errorf("expected challenge title bonk, got %s", resp.ChallengeInfo.Challenge.Title)
	}
	if resp.ChallengeInfo.Stats.ViewCount != 1000000000 {
		t.Errorf("expected viewCount=1000000000, got %d", resp.ChallengeInfo.Stats.ViewCount)
	}
	if resp.ChallengeInfo.Stats.VideoCount != 50000 {
		t.Errorf("expected videoCount=50000, got %d", resp.ChallengeInfo.Stats.VideoCount)
	}
}

func TestChallengeItemListDeserialization(t *testing.T) {
	t.Parallel()
	raw := challengeItemsJSON(3, true, 35)

	var resp challengeItemListResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(resp.ItemList) != 3 {
		t.Fatalf("expected 3 items, got %d", len(resp.ItemList))
	}
	if !resp.HasMore {
		t.Error("expected hasMore=true")
	}
	if resp.Cursor != 35 {
		t.Errorf("expected cursor=35, got %d", resp.Cursor)
	}
}

// ---------------------------------------------------------------------------
// signURL / browser edge cases (without actual browser)
// ---------------------------------------------------------------------------

func TestSignURL_NilPage(t *testing.T) {
	t.Parallel()
	s := New()
	// signFunc defaults to signURL which checks page == nil.
	_, err := s.signFunc("https://example.com")
	if !errors.Is(err, ErrBrowserNotReady) {
		t.Errorf("expected ErrBrowserNotReady, got %v", err)
	}
}

func TestEnsureSigningReady_AlreadyReady(t *testing.T) {
	t.Parallel()
	s := New()
	s.signingReady.Store(true)
	// Should return nil immediately without touching page.
	if err := s.ensureSigningReady(); err != nil {
		t.Errorf("expected nil for already-ready, got %v", err)
	}
}

func TestDoRequest_InvalidURL(t *testing.T) {
	t.Parallel()
	s := New()
	_, err := s.doRequest(context.Background(), "GET", "://invalid", nil)
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestSearchByHashtag_SignError(t *testing.T) {
	t.Parallel()
	s := New().WithSearchDelay(0)
	s.signFunc = func(rawURL string) (string, error) {
		return "", fmt.Errorf("sign failed: %w", ErrSigningFailed)
	}
	_, err := s.SearchByHashtag(context.Background(), "bonk", 10)
	if !errors.Is(err, ErrSigningFailed) {
		t.Errorf("expected ErrSigningFailed, got %v", err)
	}
}

func TestSearchByHashtag_ItemListSignError(t *testing.T) {
	t.Parallel()
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/api/challenge/detail") {
			w.Write([]byte(challengeDetailJSON("789", "bonk")))
		}
	}))
	defer srv.Close()

	s := New().WithSearchDelay(0).WithProfileDelay(0)
	s.baseURL = srv.URL
	s.signFunc = func(rawURL string) (string, error) {
		callCount++
		if callCount <= 1 {
			return rawURL, nil
		}
		return "", fmt.Errorf("sign hashtag: %w", ErrSigningFailed)
	}

	_, err := s.SearchByHashtag(context.Background(), "bonk", 10)
	if !errors.Is(err, ErrSigningFailed) {
		t.Errorf("expected ErrSigningFailed, got %v", err)
	}
}

func TestFetchHashtagVideos_InvalidJSON(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/api/challenge/detail"):
			w.Write([]byte(challengeDetailJSON("789", "bonk")))
		case strings.Contains(r.URL.Path, "/api/challenge/item_list"):
			w.Write([]byte(`not json`))
		}
	}))
	defer srv.Close()

	s := newMockScraper(srv.URL)
	_, err := s.SearchByHashtag(context.Background(), "bonk", 10)
	if err == nil {
		t.Fatal("expected error for invalid hashtag items JSON")
	}
}

// ---------------------------------------------------------------------------
// Integration tests (require network, skip with -short)
// ---------------------------------------------------------------------------

func TestGetUser_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	s := New().WithProfileDelay(0)
	defer s.Close()

	author, err := s.GetUser(t.Context(), "tiktok")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}

	if author.Username == "" {
		t.Error("expected non-empty username")
	}
	if author.FollowerCount == 0 {
		t.Error("expected non-zero follower count for @tiktok")
	}
	t.Logf("@%s: %d followers, %d videos", author.Username, author.FollowerCount, author.VideoCount)
}
