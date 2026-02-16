# tiktok-gofun

Production-grade TikTok scraper library for Go. Tracks crypto virality/hype on TikTok as part of the Viraly platform, polling every ~60s alongside Twitter/Telegram sources.

## Module

```
github.com/RavensCloud/tiktok-gofun
```

Go 1.25 | Dependencies: `go-rod/rod`, `go-rod/stealth`, `golang.org/x/net`

## Architecture

**Hybrid approach** — two data paths optimized for speed:

| Path | Method | Speed | Browser? |
|------|--------|-------|----------|
| User profiles | SSR HTML parsing (`__UNIVERSAL_DATA_FOR_REHYDRATION__`) | ~200ms | No |
| Search/hashtag | Browser signs URL + fetches via JS `fetch()` in one atomic call | ~300ms | Sign + fetch |

Search uses `browserFetch()` which signs the URL via `frontierSign()` and fetches using the browser's `fetch()` API in a single JS eval. This ensures the browser's TLS fingerprint and cookies are used consistently, avoiding anti-bot detection from fingerprint mismatches.

User profiles use pure `net/http` with SSR parsing (no browser needed).

## Project Structure

```
tiktok-gofun/
├── go.mod                  # github.com/RavensCloud/tiktok-gofun
├── errors.go               # Sentinel errors
├── types.go                # Video, Author (public types)
├── types_raw.go            # Raw JSON structs (flat format) + parseVideo/parseAuthor
├── scraper.go              # Scraper struct, New(), proxy, cookies, HTTP, rate limiting
├── ssr.go                  # __UNIVERSAL_DATA_FOR_REHYDRATION__ extraction
├── user.go                 # GetUser() via SSR parsing (pure HTTP)
├── browser.go              # go-rod lifecycle, stealth, browserFetch(), signURL() [build tag: !unittest]
├── browser_stub.go         # No-op stubs for unit testing [build tag: unittest]
├── auth.go                 # Login, cookie sync browser→HTTP [build tag: !unittest]
├── auth_stub.go            # No-op stubs for unit testing [build tag: unittest]
├── search.go               # SearchVideos(), SearchByHashtag() via browserAPIRequest()
├── scraper_test.go         # Unit + integration tests
├── cmd/tiktok/main.go      # CLI for testing
└── document.md             # Design reference document
```

### File Responsibilities

| File | Purpose | Browser | HTTP |
|------|---------|---------|------|
| `scraper.go` | Core struct, constructor, proxy, cookies, HTTP client, rate limiting | Fields only | Yes |
| `search.go` | SearchVideos, SearchByHashtag via `browserAPIRequest()` using `fetchFunc` | Via fetchFunc | No |
| `user.go` | GetUser via SSR HTML parsing | No | Yes |
| `ssr.go` | Parse `__UNIVERSAL_DATA_FOR_REHYDRATION__` from HTML | No | No |
| `browser.go` | Browser lifecycle, stealth mode, `browserFetch()`, `signURL()`, resource blocking | Yes | No |
| `auth.go` | Login automation, cookie sync browser→HTTP | Yes | Yes |
| `types.go` | Public Video and Author structs | - | - |
| `types_raw.go` | Internal JSON structs matching TikTok API (flat format), conversion functions | - | - |
| `errors.go` | Sentinel errors (ErrRateLimited, ErrNotFound, etc.) | - | - |

## Core Design

### Scraper Struct

```go
type Scraper struct {
    client       *http.Client      // HTTP client with cookie jar + connection pooling
    baseURL      string            // "https://www.tiktok.com" (overridable for tests)
    proxy        string
    userAgent    string
    isLogged     bool

    browser      *rod.Browser      // Headless Chrome
    page         *rod.Page          // Reusable page with TikTok JS loaded
    browserMu    sync.Mutex         // Protects concurrent browser calls
    signingReady atomic.Bool        // Cached signing readiness

    signFunc     func(string) (string, error)  // Signs URL via browser JS (replaceable for testing)
    fetchFunc    func(string) ([]byte, error)   // Signs + fetches via browser JS fetch() (replaceable for testing)

    searchDelay  time.Duration      // 2s default (~30 req/min)
    profileDelay time.Duration      // 1s default (~60 req/min)
    // + per-type mutexes and timestamps
}
```

### Browser Fetch (Sign + Fetch Atomically)

Search/hashtag requests use `browserFetch()` which executes a single JS eval that:
1. Signs the URL via `window.byted_acrawler.frontierSign(url)`
2. Fetches the signed URL via `fetch()` with `credentials: 'include'`
3. Returns the response body as text

This avoids TLS fingerprint mismatches between Go's `net/http` and the browser. On failure, marks `signingReady=false` so the next call reloads the page.

### Rate Limiting

Per-operation-type rate limiting (not global):
- **Search/hashtag**: 2s minimum delay + 0-500ms jitter
- **User profiles**: 1s minimum delay + 0-500ms jitter
- Independent mutexes — profile requests don't wait for search cooldown

### HTTP Transport (used for user profiles only)

```go
MaxIdleConns:        100
MaxIdleConnsPerHost: 10
IdleConnTimeout:     90s
TLSHandshakeTimeout: 10s
KeepAlive:           30s
```

### TikTok API Response Format

TikTok returns **flat JSON** (no `data` envelope):

```go
// Search response
{"status_code": 0, "item_list": [...], "has_more": 1, "cursor": 20}

// Hashtag item list (camelCase variant)
{"itemList": [...], "hasMore": true, "cursor": 20}
```

Note: field naming is inconsistent between endpoints (snake_case vs camelCase, int vs bool for `has_more`).

## Public API

```go
// Constructor
s := tiktok.New()                           // Sensible defaults, no browser
s.WithSearchDelay(2 * time.Second)          // Builder pattern
s.WithProfileDelay(1 * time.Second)

// Proxy
s.SetProxy("http://proxy:8080")             // HTTP/HTTPS
s.SetProxy("socks5://user:pass@proxy:1080") // SOCKS5
s.SetProxy("")                              // Reset

// User profiles (pure HTTP, no browser)
author, err := s.GetUser(ctx, "tiktok")

// Browser initialization (required for search)
s.InitBrowser()

// Authentication
s.Login("user", "pass")                     // Browser automation
s.LoginWithCookies("cookies.json")          // Load saved session
s.SaveCookies("cookies.json")               // Persist session
s.LoadCookies("cookies.json")

// Search (requires browser + auth)
videos, err := s.SearchVideos(ctx, "bonk solana", 50)
videos, err := s.SearchByHashtag(ctx, "bonk", 50)

// Cookie management
s.GetCookies()
s.SetCookies(cookies)
s.IsLoggedIn()

// Cleanup
s.Close()
```

## Types

```go
type Video struct {
    ID, Description, AuthorID, Username string
    CreatedAt time.Time
    Views, Likes, Comments, Shares int
}

type Author struct {
    ID, Username string
    FollowerCount, FollowingCount, VideoCount int
    Verified bool
    Bio, AvatarURL string
}
```

## Sentinel Errors

```go
ErrRateLimited     // HTTP 429
ErrNotFound        // HTTP 404
ErrAuthRequired    // Authentication needed
ErrCaptcha         // CAPTCHA triggered
ErrSigningFailed   // Browser JS signing failed
ErrBrowserNotReady // Browser not initialized
ErrInvalidResponse // Unexpected response format
```

## Testing

### Build Tags

Browser/auth code uses build tags to separate unit-testable and integration code:

- **`browser.go`** / **`auth.go`**: `//go:build !unittest` — real implementation requiring Chrome
- **`browser_stub.go`** / **`auth_stub.go`**: `//go:build unittest` — no-op stubs

### Running Tests

```bash
# Unit tests (no Chrome required, 93%+ coverage)
go test -tags unittest -short -race -cover ./...

# All tests including integration (requires network)
go test -race -cover ./...

# Full coverage report
go test -tags unittest -short -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

### Coverage

With `unittest` tag: **93.1%** statement coverage. All non-browser code is fully covered.

### Test Strategy

- **Mock-based**: `httptest.NewServer` for all HTTP endpoints
- **`baseURL` override**: Scraper's `baseURL` field points to test server
- **`fetchFunc` override**: Injected HTTP GET function bypasses browser signing + fetching
- **`signFunc` override**: Injected identity function bypasses browser signing
- **`newMockScraper()`**: Helper creates scraper wired to test server with zero delays

## CLI

```bash
# User profile (no browser needed)
go run ./cmd/tiktok --user "tiktok"

# Search (requires browser + cookies)
go run ./cmd/tiktok --search "bonk solana" --limit 5 --cookies cookies.json

# Hashtag search
go run ./cmd/tiktok --hashtag "crypto" --limit 10 --cookies cookies.json --proxy socks5://proxy:1080
```

## TikTok API Endpoints

| Endpoint | Purpose | Signing |
|----------|---------|---------|
| `GET /@{username}` (HTML) | User profile via SSR | No |
| `GET /api/search/item/full/` | Search videos by keyword | X-Bogus (via browserFetch) |
| `GET /api/challenge/detail/` | Get hashtag/challenge ID | X-Bogus (via browserFetch) |
| `GET /api/challenge/item_list/` | Videos by hashtag | X-Bogus (via browserFetch) |

## Development

### Prerequisites
- Go 1.25+
- Chrome/Chromium (for browser features)

### Commands

```bash
go build ./...                              # Build all
go vet ./...                                # Static analysis
go test -tags unittest -short -race ./...   # Unit tests
go test -race ./...                         # All tests
```

### Code Standards

Follow `.agent/rules/code.md`:
- Max 40 lines per function, max 4 parameters
- Early returns, error wrapping with `fmt.Errorf("context: %w", err)`
- Sentinel errors for expected conditions
- Table-driven tests, 80%+ coverage on business logic
- Interfaces defined where used, not where implemented
