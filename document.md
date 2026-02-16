# TikTok Scraper for Go - Implementation Context

## 1. Why Scraping (Not Official API)

TikTok has 3 official APIs. None work for your use case:

| API | Can Search Hashtags? | Available To You? |
|-----|---------------------|-------------------|
| **Research API** | Yes | No - requires academic affiliation, no commercial use |
| **Display API** | No - only embed specific videos | Yes but useless |
| **Content Posting API** | No - only for uploading | Yes but useless |

**Conclusion:** You must scrape TikTok's internal web API (the same endpoints their website calls).

---

## 2. How TikTok's Web Works Internally

When you visit `tiktok.com/tag/bonk`, the browser does this:

```
1. Browser loads tiktok.com
2. TikTok's JavaScript runs
3. JS calls internal API: GET /api/challenge/item_list/?challengeID=123&count=35
4. Server validates: cookies + msToken + X-Bogus signature
5. Server returns JSON with video data
```

Your scraper needs to replicate steps 3-5.

---

## 3. TikTok's Anti-Bot System

TikTok uses a multi-layer protection system. This is the hardest part.

### Layer 1: msToken (Cookie)

- A token stored as a cookie after visiting TikTok
- Generated server-side when a real browser session exists
- Required for most API calls
- **How to get it:** Login with an account, extract from cookies

### Layer 2: X-Bogus (URL Signature)

- A cryptographic signature appended to every API request URL
- Generated client-side by TikTok's JavaScript VM
- The algorithm: takes your URL + User-Agent + timestamp, runs it through MD5 hashes and custom ciphers
- **How to get it:** Run TikTok's JS in a headless browser (go-rod) and call `window.byted_acrawler.frontierSign(url)`

### Layer 3: X-Gnarly (Header Signature)

- An additional header signature
- Combines the signed URL + msToken + User-Agent
- Required for some endpoints

### Layer 4: Browser Fingerprinting

- TikTok checks User-Agent, screen size, timezone, language
- Must look like a real browser
- Client-side only (doesn't talk to server), so can be faked

### What This Means For You

We use a **hybrid approach**:

**User Profiles → SSR Parsing (no browser, no signatures)**
```
Go code -> HTTP GET tiktok.com/@user -> parse embedded JSON from HTML
```
- No X-Bogus needed. TikTok server-renders profile data in the HTML.
- ~200ms per request.

**Search → Browser Signs URL + Pure HTTP Request**
```
Go code -> browser JS signs URL (~50ms) -> HTTP GET with signed URL (~200ms)
```
- Browser stays open in background ONLY for calling `frontierSign()`.
- All data fetching is pure `net/http`. ~300ms per search.
- If signing fails, just skip this cycle (Viraly polls every 60s).

---

## 4. TikTok Internal API Endpoints

These are the endpoints TikTok's website calls. Use browser DevTools (Network tab) on tiktok.com to verify these are current.

### 4.1 Search Videos by Keyword

```
GET https://www.tiktok.com/api/search/item/full/
```

| Parameter | Type | Description |
|-----------|------|-------------|
| `keyword` | string | Search term (e.g., "bonk solana") |
| `cursor` | int | Pagination offset (0, 10, 20...) |
| `count` | int | Results per page (default ~10) |
| `from_page` | string | "search" |
| `web_search_code` | string | JSON search config |

**Response:**
```json
{
  "item_list": [
    {
      "id": "7340000000000",
      "desc": "Video description with #hashtags",
      "createTime": 1706000000,
      "author": {
        "uniqueId": "username",
        "id": "700000000",
        "nickname": "Display Name",
        "avatarThumb": "https://...",
        "verified": false
      },
      "stats": {
        "playCount": 150000,
        "diggCount": 8500,
        "shareCount": 1200,
        "commentCount": 340
      },
      "video": {
        "duration": 15,
        "cover": "https://..."
      }
    }
  ],
  "has_more": true,
  "cursor": 10
}
```

### 4.2 Get Hashtag/Challenge Detail

```
GET https://www.tiktok.com/api/challenge/detail/
```

| Parameter | Type | Description |
|-----------|------|-------------|
| `challengeName` | string | Hashtag without # (e.g., "bonk") |

**Response:**
```json
{
  "challengeInfo": {
    "challenge": {
      "id": "123456",
      "title": "bonk",
      "desc": "Challenge description"
    },
    "stats": {
      "videoCount": 50000,
      "viewCount": 1000000000
    }
  }
}
```

### 4.3 Get Videos by Hashtag

```
GET https://www.tiktok.com/api/challenge/item_list/
```

| Parameter | Type | Description |
|-----------|------|-------------|
| `challengeID` | string | From challenge/detail response |
| `count` | int | Videos per page (default 35) |
| `cursor` | int | Pagination offset |

**Response:** Same format as search, returns `itemList` array.

### 4.4 Get User Profile

```
GET https://www.tiktok.com/api/user/detail/
```

| Parameter | Type | Description |
|-----------|------|-------------|
| `uniqueId` | string | Username |
| `secUid` | string | Security UID (optional) |

**Response:**
```json
{
  "userInfo": {
    "user": {
      "id": "700000000",
      "uniqueId": "username",
      "nickname": "Display Name",
      "avatarLarger": "https://...",
      "signature": "Bio text here",
      "verified": false,
      "secUid": "MS4w..."
    },
    "stats": {
      "followerCount": 15000,
      "followingCount": 200,
      "heart": 500000,
      "heartCount": 500000,
      "videoCount": 120,
      "diggCount": 3000
    }
  }
}
```

> **IMPORTANT:** These endpoints change. Always verify with browser DevTools before implementing. Open tiktok.com, search something, and check the Network tab for actual URLs and params.

---

## 5. Required HTTP Headers

Every request must include realistic browser headers:

```
User-Agent: Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36
Accept: application/json, text/plain, */*
Accept-Language: en-US,en;q=0.9
Referer: https://www.tiktok.com/
Origin: https://www.tiktok.com
Cookie: msToken=...; tt_webid_v2=...; sessionid=...
```

---

## 6. Implementation Approach

Two techniques combined for best results:

```
┌──────────────────────────────────────────────────────────────┐
│  SEARCH (hashtags/keywords)                                   │
│  Browser signs URL via JS (~50ms), pure HTTP fetches data    │
│  ~300ms per search                                           │
└──────────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────────┐
│  USER PROFILES (bot detection)                                │
│  Pure HTTP GET tiktok.com/@username                          │
│  Parse __UNIVERSAL_DATA_FOR_REHYDRATION__ JSON               │
│  ~200ms per profile (no browser needed)                      │
└──────────────────────────────────────────────────────────────┘
```

For Viraly's 60-second polling cycle with parallel Twitter/Telegram sources, if signing
fails you just retry next cycle — speed matters, not reliability.

---

### 6.1 SSR HTML Parsing for User Profiles (Pure HTTP, No Browser)

TikTok server-renders pages with ALL data embedded as JSON in the HTML:

```html
<script id="__UNIVERSAL_DATA_FOR_REHYDRATION__" type="application/json">
  {"__DEFAULT_SCOPE__": {"webapp.user-detail": {"userInfo": { ... all user data ... }}}}
</script>
```

**How to extract it:**
```go
// 1. GET the page (pure HTTP, no browser)
resp, err := s.client.Get("https://www.tiktok.com/@username")

// 2. Read HTML body
body, _ := io.ReadAll(resp.Body)

// 3. Find the JSON script tag
// Look for: <script id="__UNIVERSAL_DATA_FOR_REHYDRATION__" ...>
// Extract the JSON between the script tags

// 4. Parse the JSON
var data universalData
json.Unmarshal(jsonBytes, &data)

// 5. Map to your types
author := parseAuthorFromSSR(data)
```

**Works for:**
- User profiles: `GET https://www.tiktok.com/@username`
- Individual videos: `GET https://www.tiktok.com/@user/video/123`
- Hashtag pages: `GET https://www.tiktok.com/tag/bonk` (may have limited data)

**Does NOT work for:**
- Search by keyword (search page requires JS to load results)
- Pagination (only first page of results)

**Why it's fast:** No browser, no JS, no signatures. Just HTTP GET + HTML parsing.

---

### 6.2 Browser Signing + HTTP for Search

Keep a browser page open with TikTok loaded. Use it ONLY to call the signing JS function,
then make the actual HTTP request with pure `net/http`.

#### How It Works Step by Step

```
STARTUP (once):
1. Launch browser with go-rod + stealth
2. Navigate to tiktok.com (loads signing JS)
3. Login (or load cookies) → browser has valid session
4. Extract cookies from browser → set on HTTP client
5. Browser stays open in background (only for signing)

EACH SEARCH (~300ms):
1. Build API URL with params                          (~0ms)
2. Call browser JS: frontierSign(url) → signed URL    (~50ms)
3. Pure HTTP GET with signed URL + cookies             (~200ms)
4. Parse JSON response                                 (~10ms)
```

#### Browser Setup

```go
// browser.go - Launch browser ONCE, keep open for signing only
func (s *Scraper) launchBrowser() error {
    // Launch with stealth
    u := launcher.New().Headless(true).MustLaunch()
    s.browser = rod.New().ControlURL(u).MustConnect()
    s.page = stealth.MustPage(s.browser)

    // Block ALL resources - we only need JS execution context
    router := s.browser.HijackRequests()
    router.MustAdd("*.css", func(ctx *rod.Hijack) { ctx.Response.Fail(proto.NetworkErrorReasonBlockedByClient) })
    router.MustAdd("*.png", func(ctx *rod.Hijack) { ctx.Response.Fail(proto.NetworkErrorReasonBlockedByClient) })
    router.MustAdd("*.jpg", func(ctx *rod.Hijack) { ctx.Response.Fail(proto.NetworkErrorReasonBlockedByClient) })
    router.MustAdd("*.mp4", func(ctx *rod.Hijack) { ctx.Response.Fail(proto.NetworkErrorReasonBlockedByClient) })
    router.MustAdd("*.woff*", func(ctx *rod.Hijack) { ctx.Response.Fail(proto.NetworkErrorReasonBlockedByClient) })
    router.MustAdd("*.svg", func(ctx *rod.Hijack) { ctx.Response.Fail(proto.NetworkErrorReasonBlockedByClient) })
    router.MustAdd("*analytics*", func(ctx *rod.Hijack) { ctx.Response.Fail(proto.NetworkErrorReasonBlockedByClient) })
    go router.Run()

    // Navigate to TikTok (loads signing JS)
    s.page.MustNavigate("https://www.tiktok.com").MustWaitStable()

    return nil
}
```

#### Signing + HTTP Request Flow

```go
// search.go - Sign URL via browser, request via HTTP
func (s *Scraper) SearchVideos(ctx context.Context, keyword string, limit int) ([]Video, error) {
    rawURL := fmt.Sprintf(
        "https://www.tiktok.com/api/search/item/full/?keyword=%s&count=20&cursor=0",
        url.QueryEscape(keyword),
    )

    // Step 1: Sign the URL (~50ms)
    s.browserMu.Lock()
    signedURL, err := s.signURL(rawURL)
    s.browserMu.Unlock()
    if err != nil {
        return nil, fmt.Errorf("sign url: %w", err)
    }

    // Step 2: Pure HTTP request (~200ms)
    resp, err := s.doRequest(ctx, "GET", signedURL, nil)
    if err != nil {
        return nil, fmt.Errorf("search request: %w", err)
    }
    defer resp.Body.Close()

    // Step 3: Parse response
    var result searchResponse
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return nil, fmt.Errorf("decode response: %w", err)
    }

    videos := make([]Video, 0, len(result.ItemList))
    for _, raw := range result.ItemList {
        videos = append(videos, parseVideo(raw))
    }
    return videos, nil
}

func (s *Scraper) signURL(rawURL string) (string, error) {
    result, err := s.page.Evaluate(rod.Eval(`(url) => {
        const signed = window.byted_acrawler.frontierSign(url);
        return signed;
    }`, rawURL))
    if err != nil {
        return "", fmt.Errorf("sign url: %w", err)
    }
    return result.Value.String(), nil
}
```

#### Cookie Sync: Browser → HTTP Client

After login (or loading cookies), sync cookies from browser to the HTTP client:

```go
// auth.go - After browser login, sync cookies to HTTP client
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
        if c.Name == "msToken" {
            s.msToken = c.Value
        }
    }
    s.SetCookies(httpCookies)
    s.isLogged = true
    return nil
}
```

#### Handling Signing Failures

```go
// If frontierSign is not available, refresh the page
func (s *Scraper) ensureSigningReady() error {
    // Check if signing function exists
    result, err := s.page.Evaluate(rod.Eval(`() => typeof window.byted_acrawler !== 'undefined'`))
    if err != nil || !result.Value.Bool() {
        // Reload page to get signing JS back
        s.page.MustNavigate("https://www.tiktok.com").MustWaitStable()
    }
    return nil
}
```

---

### go-rod Key Packages

```
github.com/go-rod/rod              # Core browser automation
github.com/go-rod/rod/lib/launcher # Chrome launcher
github.com/go-rod/rod/lib/proto    # Chrome DevTools Protocol types
github.com/go-rod/stealth          # Anti-bot detection evasion
```

### go-rod Stealth Setup

```go
import "github.com/go-rod/stealth"

// Use stealth to avoid TikTok's bot detection
browser := rod.New().MustConnect()
page := stealth.MustPage(browser) // Patches navigator.webdriver, etc.

// Verify stealth works:
// page.Navigate("https://bot.sannysoft.com")
```

---

## 7. Login Flow

TikTok requires an account for search. The login flow:

```
POST https://www.tiktok.com/passport/web/user/login/
```

But it's heavily protected. With go-rod, you automate the real login:

```
1. Browser is already open (launchBrowser called at startup)
2. Navigate to tiktok.com/login
3. Enter username/password in form fields
4. Handle CAPTCHA (if triggered)
5. Wait for redirect
6. Sync cookies from browser → HTTP client (syncCookiesFromBrowser)
7. Save cookies to file for future use
8. Don't close browser — it stays open for URL signing
```

### Cookie Persistence

After first login, save cookies to a file. On next startup, load cookies instead of logging in again. Re-login only when cookies expire.

---

## 8. Rate Limiting

TikTok rate limits aggressively. Known limits:

| Action | Approximate Limit | Consequence |
|--------|-------------------|-------------|
| Search requests | ~30/minute | 429 or CAPTCHA |
| User profile lookups | ~60/minute | 429 or CAPTCHA |
| Hashtag video lists | ~30/minute | 429 or CAPTCHA |
| From same IP | ~100/minute total | IP block (temp) |

### Mitigation

- Minimum 2-3 seconds between requests
- Add random jitter (1-3 seconds)
- Use residential proxies (datacenter IPs are blocked fast)
- Rotate User-Agent strings
- Cooldown period on 429 (15-30 minutes)
- Proxy rotation per request or per session

---

## 9. Data Mapping: TikTok JSON -> Go Structs

### Video

| TikTok JSON Field | Go Struct Field | Type |
|-------------------|----------------|------|
| `id` | ID | string |
| `desc` | Description | string |
| `createTime` | CreatedAt | time.Time (unix) |
| `author.uniqueId` | Username | string |
| `author.id` | AuthorID | string |
| `stats.playCount` | Views | int |
| `stats.diggCount` | Likes | int |
| `stats.commentCount` | Comments | int |
| `stats.shareCount` | Shares | int |

### Author

| TikTok JSON Field | Go Struct Field | Type |
|-------------------|----------------|------|
| `user.id` | ID | string |
| `user.uniqueId` | Username | string |
| `stats.followerCount` | FollowerCount | int |
| `stats.followingCount` | FollowingCount | int |
| `stats.videoCount` | VideoCount | int |
| `user.verified` | Verified | bool |
| `user.signature` | Bio (len -> BioLength) | string/int |
| `user.avatarLarger` | HasAvatar (non-empty) | bool |

---

## 10. Architecture Patterns (From imperatrona/twitter-scraper)

The twitter-scraper library (`github.com/imperatrona/twitter-scraper`) is the model to follow.
Here are the key patterns to replicate for your TikTok scraper.

### 10.1 Scraper Struct Pattern

The twitter-scraper uses a single `Scraper` struct. We extend it with a browser for signing:

```go
// twitter-scraper pattern:
type Scraper struct {
    client    *http.Client   // HTTP client with cookie jar
    proxy     string         // Current proxy URL
    userAgent string         // Browser User-Agent
    // ... auth state, config, etc.
}
```

**Apply to TikTok (HTTP for everything + browser only for URL signing):**

```go
type Scraper struct {
    // HTTP client for ALL data requests (profiles via SSR, search via signed URLs).
    client    *http.Client
    proxy     string
    userAgent string
    isLogged  bool

    // Browser for URL signing only.
    // Stays open in background, calls frontierSign() to sign search URLs.
    // Never used for fetching data — only for generating X-Bogus signatures.
    browser   *rod.Browser
    page      *rod.Page     // Reusable page with TikTok JS loaded (for signing).
    browserMu sync.Mutex    // Protect concurrent signURL() calls.

    // Rate limiting.
    delay     time.Duration
    lastReq   time.Time
    mu        sync.Mutex    // Protect lastReq.

    // Cookies/session.
    msToken   string
}
```

**Key design decisions:**
- `client` (net/http) is used for user profiles (SSR parsing) AND for search data requests (signed URLs)
- `browser` + `page` (go-rod) is used ONLY for URL signing (`frontierSign()` calls)
- Browser launches ONCE at startup, stays open for the lifetime of the scraper
- `browserMu` protects concurrent access to the signing page
- All actual HTTP data fetching goes through `client` — browser never fetches search results

### 10.2 Constructor with Builder Pattern

twitter-scraper uses `New()` + builder methods for optional config:

```go
// twitter-scraper pattern:
func New() *Scraper {
    jar, _ := cookiejar.New(nil)
    return &Scraper{
        client: &http.Client{Jar: jar, Timeout: 10 * time.Second},
        userAgent: "Mozilla/5.0 ...",
    }
}

// Builder methods return *Scraper for chaining:
func (s *Scraper) WithDelay(d time.Duration) *Scraper { s.delay = d; return s }
func (s *Scraper) WithProxy(p string) *Scraper { ... ; return s }
```

**Apply to TikTok:**

```go
func New() *Scraper {
    jar, _ := cookiejar.New(nil)
    return &Scraper{
        client: &http.Client{
            Jar:     jar,
            Timeout: 15 * time.Second,
        },
        userAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) ...",
        delay:     3 * time.Second, // TikTok needs slower rate than Twitter
    }
}

func (s *Scraper) WithDelay(d time.Duration) *Scraper {
    s.delay = d
    return s
}

func (s *Scraper) WithProxy(proxyURL string) *Scraper {
    // Set up proxy transport (HTTP/HTTPS/SOCKS5)
    return s
}
```

### 10.3 Proxy Support (HTTP/HTTPS + SOCKS5)

twitter-scraper supports both proxy protocols:

```go
// twitter-scraper pattern:
func (s *Scraper) SetProxy(proxyAddr string) error {
    if proxyAddr == "" {
        s.client.Transport = http.DefaultTransport
        return nil
    }

    u, err := url.Parse(proxyAddr)
    if err != nil {
        return err
    }

    switch u.Scheme {
    case "http", "https":
        s.client.Transport = &http.Transport{
            Proxy: http.ProxyURL(u),
        }
    case "socks5":
        // Extract user:pass from URL if present
        dialer, err := proxy.SOCKS5("tcp", u.Host, auth, proxy.Direct)
        if err != nil {
            return err
        }
        s.client.Transport = &http.Transport{
            DialContext: dialer.(proxy.ContextDialer).DialContext,
        }
    }
    s.proxy = proxyAddr
    return nil
}
```

**Apply to TikTok (same pattern):**

```go
func (s *Scraper) SetProxy(proxyAddr string) error {
    if proxyAddr == "" {
        s.client.Transport = http.DefaultTransport
        s.proxy = ""
        return nil
    }

    u, err := url.Parse(proxyAddr)
    if err != nil {
        return fmt.Errorf("parse proxy url: %w", err)
    }

    switch u.Scheme {
    case "http", "https":
        s.client.Transport = &http.Transport{
            Proxy: http.ProxyURL(u),
        }
    case "socks5":
        var auth *proxy.Auth
        if u.User != nil {
            pass, _ := u.User.Password()
            auth = &proxy.Auth{User: u.User.Username(), Password: pass}
        }
        dialer, err := proxy.SOCKS5("tcp", u.Host, auth, proxy.Direct)
        if err != nil {
            return fmt.Errorf("socks5 proxy: %w", err)
        }
        dc, ok := dialer.(proxy.ContextDialer)
        if !ok {
            return errors.New("socks5: context dialer not supported")
        }
        s.client.Transport = &http.Transport{DialContext: dc.DialContext}
    default:
        return fmt.Errorf("unsupported proxy scheme: %s", u.Scheme)
    }

    s.proxy = proxyAddr
    return nil
}
```

**Required import for SOCKS5:**
```go
"golang.org/x/net/proxy"
```

### 10.4 Cookie Management (Save/Load Sessions)

twitter-scraper uses `GetCookies()` / `SetCookies()` to persist sessions:

```go
// twitter-scraper pattern:
func (s *Scraper) GetCookies() []*http.Cookie {
    url, _ := url.Parse("https://twitter.com")
    return s.client.Jar.Cookies(url)
}

func (s *Scraper) SetCookies(cookies []*http.Cookie) {
    url, _ := url.Parse("https://twitter.com")
    s.client.Jar.SetCookies(url, cookies)
}

func (s *Scraper) IsLoggedIn() bool {
    // Check if auth cookies exist
}
```

**Apply to TikTok:**

```go
var tiktokURL, _ = url.Parse("https://www.tiktok.com")

func (s *Scraper) GetCookies() []*http.Cookie {
    return s.client.Jar.Cookies(tiktokURL)
}

func (s *Scraper) SetCookies(cookies []*http.Cookie) {
    s.client.Jar.SetCookies(tiktokURL, cookies)
    // Extract msToken from cookies.
    for _, c := range cookies {
        if c.Name == "msToken" {
            s.msToken = c.Value
        }
    }
}

// SaveCookies writes cookies to a JSON file for reuse.
func (s *Scraper) SaveCookies(path string) error {
    cookies := s.GetCookies()
    data, err := json.Marshal(cookies)
    if err != nil {
        return fmt.Errorf("marshal cookies: %w", err)
    }
    return os.WriteFile(path, data, 0600)
}

// LoadCookies reads cookies from a JSON file.
func (s *Scraper) LoadCookies(path string) error {
    data, err := os.ReadFile(path)
    if err != nil {
        return fmt.Errorf("read cookies: %w", err)
    }
    var cookies []*http.Cookie
    if err := json.Unmarshal(data, &cookies); err != nil {
        return fmt.Errorf("unmarshal cookies: %w", err)
    }
    s.SetCookies(cookies)
    s.isLogged = true
    return nil
}

func (s *Scraper) IsLoggedIn() bool {
    return s.isLogged
}
```

### 10.5 Auth Flow (Login)

twitter-scraper has a multi-step login. TikTok is similar but uses go-rod since
TikTok's login has CAPTCHAs and complex JS:

```go
// With go-rod for TikTok:
func (s *Scraper) Login(username, password string) error {
    // 1. Browser is already open (launchBrowser called at startup)
    // 2. Navigate to tiktok.com/login
    // 3. Fill username/password fields
    // 4. Submit and wait for redirect
    // 5. Sync cookies from browser → HTTP client (syncCookiesFromBrowser)
    // 6. Save cookies to file for future use
    // 7. s.isLogged = true
    // NOTE: Don't close browser - it stays open for URL signing
    return nil
}
```

This is the most complex part. Login is only needed once, then you reuse cookies.
The browser is NOT closed after login — it stays open for signing.

### 10.6 Search with Cursor Pagination

twitter-scraper uses cursor-based pagination with channel returns:

```go
// twitter-scraper pattern:
// Returns a channel for streaming results
func (s *Scraper) SearchTweets(ctx context.Context, query string, max int) <-chan *TweetResult {
    // Uses cursor pagination internally
}

// Or fetch function returning slice + cursor
func (s *Scraper) FetchSearchTweets(query string, max int, cursor string) ([]*Tweet, string, error) {
    // Returns (results, nextCursor, error)
}
```

**Apply to TikTok:**

```go
// Simple version: returns all results up to limit.
func (s *Scraper) SearchVideos(ctx context.Context, keyword string, limit int) ([]Video, error) {
    var allVideos []Video
    cursor := 0

    for len(allVideos) < limit {
        videos, nextCursor, err := s.fetchSearch(ctx, keyword, cursor)
        if err != nil {
            return allVideos, err
        }
        allVideos = append(allVideos, videos...)
        if nextCursor == 0 {
            break // No more results.
        }
        cursor = nextCursor
        s.waitBeforeRequest() // Rate limiting.
    }

    if len(allVideos) > limit {
        allVideos = allVideos[:limit]
    }
    return allVideos, nil
}

// fetchSearch makes a single API request (sign URL via browser, fetch via HTTP).
func (s *Scraper) fetchSearch(ctx context.Context, keyword string, cursor int) ([]Video, int, error) {
    // 1. Build URL with params.
    // 2. Sign URL via browser JS (frontierSign) (~50ms).
    // 3. Pure HTTP GET with signed URL (~200ms).
    // 4. Parse JSON response.
    // 5. Return (videos, nextCursor, error).
}
```

### 10.7 HTTP Request Helper

twitter-scraper has a centralized request method:

```go
// Apply to TikTok:
func (s *Scraper) doRequest(ctx context.Context, method, urlStr string, body io.Reader) (*http.Response, error) {
    req, err := http.NewRequestWithContext(ctx, method, urlStr, body)
    if err != nil {
        return nil, fmt.Errorf("create request: %w", err)
    }

    // Set standard headers.
    req.Header.Set("User-Agent", s.userAgent)
    req.Header.Set("Accept", "application/json, text/plain, */*")
    req.Header.Set("Accept-Language", "en-US,en;q=0.9")
    req.Header.Set("Referer", "https://www.tiktok.com/")

    resp, err := s.client.Do(req)
    if err != nil {
        return nil, fmt.Errorf("do request: %w", err)
    }

    // Check for rate limiting.
    if resp.StatusCode == 429 {
        resp.Body.Close()
        return nil, ErrRateLimited
    }

    return resp, nil
}
```

### 10.8 JSON Response Parsing

twitter-scraper parses nested JSON into flat Go structs:

```go
// Raw TikTok JSON structs (match API response exactly).
type searchResponse struct {
    ItemList []rawVideo `json:"item_list"`
    HasMore  bool       `json:"has_more"`
    Cursor   int        `json:"cursor"`
}

type rawVideo struct {
    ID         string    `json:"id"`
    Desc       string    `json:"desc"`
    CreateTime int64     `json:"createTime"`
    Author     rawAuthor `json:"author"`
    Stats      rawStats  `json:"stats"`
}

type rawAuthor struct {
    UniqueID    string `json:"uniqueId"`
    ID          string `json:"id"`
    Nickname    string `json:"nickname"`
    AvatarThumb string `json:"avatarThumb"`
    Verified    bool   `json:"verified"`
}

type rawStats struct {
    PlayCount    int `json:"playCount"`
    DiggCount    int `json:"diggCount"`
    ShareCount   int `json:"shareCount"`
    CommentCount int `json:"commentCount"`
}

// Parse into your clean public types.
func parseVideo(raw rawVideo) Video {
    return Video{
        ID:          raw.ID,
        Description: raw.Desc,
        AuthorID:    raw.Author.ID,
        Username:    raw.Author.UniqueID,
        CreatedAt:   time.Unix(raw.CreateTime, 0),
        Views:       raw.Stats.PlayCount,
        Likes:       raw.Stats.DiggCount,
        Comments:    raw.Stats.CommentCount,
        Shares:      raw.Stats.ShareCount,
    }
}
```

---

## 11. Project Structure

```
tiktok-gofun/
├── go.mod               # module github.com/youruser/tiktok-gofun
├── main.go              # CLI for manual testing
│
├── scraper.go           # Core: Scraper struct, New(), Close(), proxy, cookies, HTTP helper
├── scraper_test.go      # Unit tests
│
├── browser.go           # go-rod browser lifecycle: launch, stealth, resource blocking, signURL()
├── auth.go              # Login (go-rod), cookie sync browser→HTTP, save/load, session refresh
├── search.go            # SearchVideos(), SearchByHashtag() (sign URL + HTTP request)
├── user.go              # GetUser() profile (pure HTTP + SSR parsing)
├── ssr.go               # SSR HTML parsing: extract __UNIVERSAL_DATA_FOR_REHYDRATION__
│
├── types.go             # Video, Author (public types)
├── types_raw.go         # Raw JSON response structs (private, for API + SSR parsing)
├── errors.go            # Sentinel errors
│
└── document.md          # This file
```

**File responsibilities:**

| File | What It Does | Browser? | HTTP? |
|------|-------------|----------|-------|
| `search.go` | Sign URL via browser JS, fetch data via HTTP | ✅ Signing only | ✅ Data fetch |
| `user.go` | Get user profiles via SSR parsing | ❌ No | ✅ Yes |
| `ssr.go` | Parse `__UNIVERSAL_DATA_FOR_REHYDRATION__` from HTML | ❌ No | ✅ Yes |
| `browser.go` | Browser lifecycle, stealth, resource blocking, `signURL()` | ✅ Yes | ❌ No |
| `auth.go` | Login (browser), cookie sync + persistence | ✅ Yes | ✅ Yes |
| `scraper.go` | Core struct, ties everything together | Both | Both |

**Why this structure (not subdirectories):**

The twitter-scraper uses flat package layout. Since this is a library (not an app),
a single package is simpler. Users import it as:

```go
import "github.com/youruser/tiktok-gofun"
```

No `internal/` needed for an MVP. Add it later if the package grows.

---

## 12. How This Connects to Viraly

Once `tiktok-gofun` works standalone, Viraly imports it.

### Current State of Viraly (what you already changed)

**`app/domain/social/types.go`** - Already has:
```go
type TikTokVideo struct { ID, AuthorID, Username, Description, CreatedAt, Views, Likes, Comments, Shares }
type TikTokAuthor struct { ID, Username, FollowerCount, FollowingCount, VideoCount, Verified, BioLength, HasAvatar }
```

**`app/domain/social/social.go`** - Already has placeholder:
```go
// TikTokClient defines TikTok monitoring.
// (empty - needs interface methods)
```

**`app/domain/social/adapters.go`** - Already has:
```go
type TiktokAdapter struct {
    scraper *tiktok.LibraryScraper
}
```

**`foundation/tiktok/`** - Empty files (config.go, scraper.go, scraper_test.go)

### Integration flow

```
tiktok-gofun (this project)
    │
    │  import
    ▼
viraly/foundation/tiktok/scraper.go
    │  LibraryScraper wraps tiktok-gofun.Scraper
    │  Adds: Viraly-specific rate limiting, retries, health tracking
    │  Same pattern as foundation/twitter/scraper.go wraps imperatrona/twitter-scraper
    ▼
viraly/app/domain/social/adapters.go
    │  TiktokAdapter converts tiktok types -> social types
    │  (TikTokVideo, TikTokAuthor)
    ▼
viraly/app/domain/social/social.go
    │  Orchestrator polls TikTok alongside Twitter/Telegram
    ▼
Pipeline continues: Hype Engine -> Signals -> WebSocket
```

### What Viraly's foundation/tiktok/scraper.go will look like

Same pattern as `foundation/twitter/scraper.go`:

```go
// foundation/tiktok/scraper.go
package tiktok

import tiktokgofun "github.com/youruser/tiktok-gofun"

// LibraryScraper wraps tiktok-gofun with health tracking.
type LibraryScraper struct {
    cfg     Config
    log     *slog.Logger
    scraper *tiktokgofun.Scraper
    // ... rate limiting, cooldown, health state
}

func NewLibraryScraper(cfg Config, log *slog.Logger) (*LibraryScraper, error) {
    s := tiktokgofun.New()
    if cfg.ProxyURL != "" {
        if err := s.SetProxy(cfg.ProxyURL); err != nil {
            return nil, fmt.Errorf("set proxy: %w", err)
        }
    }
    // Login or load cookies
    // ...
    return &LibraryScraper{scraper: s, cfg: cfg, log: log}, nil
}

func (s *LibraryScraper) Search(ctx context.Context, query string, limit int) ([]Video, error) {
    // Delegate to s.scraper.SearchVideos with health tracking
}

func (s *LibraryScraper) GetAuthor(ctx context.Context, username string) (Author, error) {
    // Delegate to s.scraper.GetUser with health tracking
}
```

---

## 13. Implementation Order

### Phase 1: Pure HTTP foundation (no browser yet)

Get the basics working first. User profiles work without a browser.

| Step | File | What to Build |
|------|------|---------------|
| 1 | `go.mod` | Fix module name: `github.com/youruser/tiktok-gofun` |
| 2 | `errors.go` | Sentinel errors: `ErrRateLimited`, `ErrNotFound`, `ErrAuthRequired`, `ErrCaptcha` |
| 3 | `types.go` | Public types: `Video`, `Author` |
| 4 | `types_raw.go` | Raw JSON structs for API responses + SSR data |
| 5 | `scraper.go` | `Scraper` struct (HTTP only first), `New()`, `SetProxy()`, `doRequest()`, cookies |
| 6 | `ssr.go` | Parse `__UNIVERSAL_DATA_FOR_REHYDRATION__` from HTML |
| 7 | `user.go` | `GetUser()` via SSR parsing (pure HTTP) |
| 8 | `main.go` | Test: `go run . --user "tiktok"` (no browser, no login) |

### Phase 2: Add browser signing + search

| Step | File | What to Build |
|------|------|---------------|
| 9 | `browser.go` | `launchBrowser()`, stealth, resource blocking, `signURL()` |
| 10 | `scraper.go` | Add browser fields to Scraper, `Close()` for cleanup |
| 11 | `auth.go` | `Login()` with go-rod, `syncCookiesFromBrowser()`, `SaveCookies()`, `LoadCookies()` |
| 12 | `search.go` | `SearchVideos()`, `SearchByHashtag()` — sign URL via browser, fetch via HTTP |
| 13 | `main.go` | Full test: `go run . --search "bonk" --limit 5` |
| 14 | `scraper_test.go` | Unit tests |

### Phase 3: Connect to Viraly

| Step | File | What to Build |
|------|------|---------------|
| 15 | `foundation/tiktok/config.go` | Config struct |
| 16 | `foundation/tiktok/scraper.go` | LibraryScraper wrapping tiktok-gofun |
| 17 | `app/domain/social/social.go` | TikTokClient interface with methods |
| 18 | `app/domain/social/adapters.go` | Complete TiktokAdapter |
| 19 | `app/domain/social/social.go` | Add TikTok polling in orchestrator |
| 20 | `app/config/config.go` | Add TikTokConfig |
| 21 | `cmd/viraly/main.go` | Wire TikTok scraper |
| 22 | `config.toml` | Add [tiktok] section |

---

## 14. Key Risks and Gotchas

### TikTok Updates Frequently
- Endpoints may change paths or parameters
- Signature algorithms get updated
- Solution: Browser signing absorbs JS changes automatically

### CAPTCHAs
- TikTok may trigger CAPTCHA on login or after too many requests
- go-rod can detect CAPTCHA presence, but solving it requires either:
  - Manual intervention
  - CAPTCHA solving service (2captcha, capsolver)
- For MVP: detect CAPTCHA, log warning, wait and retry

### Headless Browser Detection
- TikTok checks `navigator.webdriver` and other browser properties
- `go-rod/stealth` patches most of these
- Test with `https://bot.sannysoft.com` to verify stealth works

### Cookie Expiration
- Session cookies expire (typically 24-48h)
- msToken refreshes periodically
- Need automatic re-login when cookies expire

### Proxy Requirements
- Datacenter IPs get blocked within minutes
- Residential proxies work (Bright Data, Oxylabs, IPRoyal)
- Viraly already has a proxy manager you can reuse later

### go-rod Requires Chrome Installed
- go-rod needs Chrome or Chromium on the machine
- For Docker/production: use a base image with Chromium
- For local dev: your system Chrome works

---

## 15. Useful References

### TikTok Scrapers (study their approach)
- [Q-Bukold/TikTok-Content-Scraper](https://github.com/Q-Bukold/TikTok-Content-Scraper) - **Pure HTTP + SSR approach.** Parses `__UNIVERSAL_DATA_FOR_REHYDRATION__` JSON from HTML. Our SSR user profile approach is based on this.
- [TikTok-Api (Python)](https://github.com/davidteather/TikTok-Api) - Best reference for TikTok endpoints. Uses Playwright. Read: `TikTokApi/api/search.py`, `user.py`, `hashtag.py`

### Architecture patterns
- [imperatrona/twitter-scraper](https://github.com/imperatrona/twitter-scraper) - **Main reference for Go scraper architecture.** Study: `scraper.go`, `auth.go`, `search.go`, `profile.go`

### Go browser automation
- [go-rod documentation](https://go-rod.github.io/) - Headless browser automation in Go
- [go-rod/stealth](https://github.com/go-rod/stealth) - Anti-detection plugin
- [go-rod examples](https://github.com/go-rod/rod/tree/main/lib/examples) - Code examples

### TikTok internals
- [TikTok Web Reverse Engineering](https://github.com/justbeluga/tiktok-web-reverse-engineering) - X-Bogus and X-Gnarly algorithm details
- [Reverse Engineering TikTok's VM](https://nullpt.rs/reverse-engineering-tiktok-vm-1) - Deep technical analysis of signature generation
- [TikTok Developers Portal](https://developers.tiktok.com/) - Official API docs (for reference only)

---

## 16. Quick Glossary

| Term | Meaning |
|------|---------|
| **msToken** | Session cookie required for API authentication |
| **X-Bogus** | URL signature generated by TikTok's client-side JS |
| **X-Gnarly** | Additional header signature (combines URL sig + msToken) |
| **frontierSign** | TikTok's JS function (`window.byted_acrawler.frontierSign`) that signs URLs with X-Bogus |
| **secUid** | TikTok's internal security user ID |
| **uniqueId** | Public username (@handle) |
| **diggCount** | TikTok's internal name for "likes" |
| **playCount** | TikTok's internal name for "views" |
| **challengeID** | Internal ID for a hashtag/challenge |
| **go-rod** | Go library for controlling Chrome headlessly |
| **stealth** | go-rod plugin that hides automation signals |
| **CAPTCHA** | Human verification that blocks bots |
| **cookie jar** | `net/http/cookiejar` - stores cookies per domain automatically |
| **SOCKS5** | Proxy protocol, more reliable than HTTP proxy for scraping |
| **SSR** | Server-Side Rendering - TikTok embeds JSON data in HTML on first load |
| **`__UNIVERSAL_DATA_FOR_REHYDRATION__`** | Script tag in TikTok HTML containing all page data as JSON |
| **HijackRequests** | go-rod method to block/modify network requests (block CSS, images, etc.) |
| **CDP** | Chrome DevTools Protocol - how go-rod communicates with Chrome |
