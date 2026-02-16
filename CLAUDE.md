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
| Search/hashtag | Browser signs URL via `frontierSign()` JS + pure HTTP fetch | ~300ms | Signing only |

The browser stays open in the background exclusively for URL signing (~50ms per call). All data fetching uses `net/http` with connection pooling.

## Project Structure

```
tiktok-gofun/
├── go.mod                  # github.com/RavensCloud/tiktok-gofun
├── errors.go               # Sentinel errors
├── types.go                # Video, Author (public types)
├── types_raw.go            # Raw JSON structs + parseVideo/parseAuthor
├── scraper.go              # Scraper struct, New(), proxy, cookies, HTTP, rate limiting
├── ssr.go                  # __UNIVERSAL_DATA_FOR_REHYDRATION__ extraction
├── user.go                 # GetUser() via SSR parsing (pure HTTP)
├── browser.go              # go-rod lifecycle, stealth, signURL() [build tag: !unittest]
├── browser_stub.go         # No-op stubs for unit testing [build tag: unittest]
├── auth.go                 # Login, cookie sync browser→HTTP [build tag: !unittest]
├── auth_stub.go            # No-op stubs for unit testing [build tag: unittest]
├── search.go               # SearchVideos(), SearchByHashtag()
├── scraper_test.go         # Unit + integration tests
├── cmd/tiktok/main.go      # CLI for testing
└── document.md             # Design reference document
```

### File Responsibilities

| File | Purpose | Browser | HTTP |
|------|---------|---------|------|
| `scraper.go` | Core struct, constructor, proxy, cookies, HTTP client, rate limiting | Fields only | Yes |
| `search.go` | SearchVideos, SearchByHashtag with cursor pagination | Sign only | Yes |
| `user.go` | GetUser via SSR HTML parsing | No | Yes |
| `ssr.go` | Parse `__UNIVERSAL_DATA_FOR_REHYDRATION__` from HTML | No | No |
| `browser.go` | Browser lifecycle, stealth mode, signURL, resource blocking | Yes | No |
| `auth.go` | Login automation, cookie sync browser→HTTP | Yes | Yes |
| `types.go` | Public Video and Author structs | - | - |
| `types_raw.go` | Internal JSON structs matching TikTok API, conversion functions | - | - |
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

    browser      *rod.Browser      // Headless Chrome (signing only)
    page         *rod.Page          // Reusable page with TikTok JS loaded
    browserMu    sync.Mutex         // Protects concurrent signURL calls
    signingReady atomic.Bool        // Cached signing readiness

    signFunc     func(string) (string, error)  // Replaceable for testing

    searchDelay  time.Duration      // 2s default (~30 req/min)
    profileDelay time.Duration      // 1s default (~60 req/min)
    // + per-type mutexes and timestamps
}
```

### Rate Limiting

Per-operation-type rate limiting (not global):
- **Search/hashtag**: 2s minimum delay + 0-500ms jitter
- **User profiles**: 1s minimum delay + 0-500ms jitter
- Independent mutexes — profile requests don't wait for search cooldown

### HTTP Transport

```go
MaxIdleConns:        100
MaxIdleConnsPerHost: 10
IdleConnTimeout:     90s
TLSHandshakeTimeout: 10s
KeepAlive:           30s
```

### URL Signing

Browser JS eval with 5s timeout. Uses `atomic.Bool` to cache signing readiness — avoids redundant JS checks per call. On failure, marks `signingReady=false` so next call reloads the page.

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
# Unit tests (no Chrome required, 94%+ coverage)
go test -tags unittest -short -race -cover ./...

# All tests including integration (requires network)
go test -race -cover ./...

# Full coverage report
go test -tags unittest -short -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

### Coverage

With `unittest` tag: **94.3%** statement coverage. All non-browser code is 100% covered.

Without tag: **69.1%** (browser/auth code at 0% — requires Chrome for integration tests).

### Test Strategy

- **Mock-based**: `httptest.NewServer` for all HTTP endpoints
- **`baseURL` override**: Scraper's `baseURL` field points to test server
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
| `GET /api/search/item/full/` | Search videos by keyword | X-Bogus |
| `GET /api/challenge/detail/` | Get hashtag/challenge ID | X-Bogus |
| `GET /api/challenge/item_list/` | Videos by hashtag | X-Bogus |

## Viraly Integration

```
tiktok-gofun (this library)
    │  import
    ▼
viraly/foundation/tiktok/scraper.go
    │  LibraryScraper wraps Scraper + health tracking
    ▼
viraly/app/domain/social/adapters.go
    │  TiktokAdapter converts types
    ▼
viraly/app/domain/social/social.go
    │  Orchestrator polls alongside Twitter/Telegram
    ▼
Hype Engine → Signals → WebSocket
```

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
