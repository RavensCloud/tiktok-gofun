---
trigger: always_on
---

# Go Development Standards

You are a senior Go engineer. Produce **production-grade, idiomatic Go code** that is simple, correct, and maintainable.

---

## 1. Design Philosophy (Priority Order)

All design decisions follow this hierarchy:

1. **Integrity** — Code is correct, handles errors, protects data.
2. **Readability** — Code is clear, shows its true cost, never lies.
3. **Simplicity** — Code is minimal, focused, hides complexity properly.
4. **Performance** — Code is efficient, but only optimize what's measured.

### Core Principles
- **Write less code.** 15-50 bugs per 1000 lines. Fewer lines = fewer bugs.
- **Code must never lie.** If it looks like it does X, it must do X.
- **Explicit over implicit.** No magic. No hidden behavior.
- **Errors are values.** Handle them. Every. Single. Time.

### Definition of Done
Code is "done" when: compiles clean, passes `golangci-lint`, tests critical paths, handles all errors, no unlinked `TODO`/`FIXME`.

---

## 2. Project Structure

```
api/           # HTTP/gRPC handlers and transport layer
app/           # Application layer (use cases, orchestration)
business/      # Core business logic and domain types
foundation/    # Foundational packages (logger, web, database)
zarf/          # Infrastructure (docker, k8s, scripts)
```

### Package Rules
- **Packages must provide, not contain.** Name describes what it provides.
- **Avoid** `util`, `common`, `helpers` — these are dumping grounds.
- **Layer dependencies flow inward:** api → app → business ← foundation.
- **Business has zero external dependencies** (except stdlib).
- **Foundation packages are reusable** across projects.

---

## 3. Code Quality

### Functions
- **Max 40 lines.** Extract helpers if longer.
- **Max 4 parameters.** Use option structs beyond that.
- **Single responsibility.** One function, one job.
- **Early returns.** Handle errors first, then happy path.

```go
// ✅ Early return, clear flow
func ProcessOrder(ctx context.Context, id string) error {
    if id == "" {
        return errors.New("order id required")
    }
    order, err := repo.Get(ctx, id)
    if err != nil {
        return fmt.Errorf("get order: %w", err)
    }
    return process(order)
}
```

### Naming
- **Variables:** Short in tight scope (`u`), descriptive in wide scope (`currentUser`).
- **Functions:** Verb-first (`GetUser`, `ValidateInput`).
- **Interfaces:** `-er` suffix for single method (`Reader`, `Validator`).
- **Packages:** Short, lowercase, no underscores (`auth`, not `authentication_service`).

### Interfaces
- **Define where used**, not where implemented.
- **Keep small:** 1-3 methods. 5+ is a smell.
- **Accept interfaces, return structs.**
- **Don't use interfaces** for the sake of using an interface.

---

## 4. Error Handling

1. **Always check errors.** No `_` for error returns.
2. **Wrap with context:** `fmt.Errorf("operation: %w", err)`.
3. **Sentinel errors** for expected conditions: `var ErrNotFound = errors.New("not found")`.
4. **Never panic** in library code.

```go
var (
    ErrNotFound     = errors.New("not found")
    ErrUnauthorized = errors.New("unauthorized")
)

// Check with errors.Is (sentinel) and errors.As (typed)
if errors.Is(err, ErrNotFound) { /* handle */ }

var valErr *ValidationError
if errors.As(err, &valErr) { /* extract details */ }
```

---

## 5. Concurrency

### Golden Rules
1. **Every goroutine must have a way to exit.** Know when it terminates.
2. **All goroutines must terminate before main returns.**
3. **Use `context.WithoutCancel`** for background work. Only use `context.Background()` in `main`.
4. **Share by communicating**, not by sharing memory.

### Back Pressure & Rate Limiting
- **Monitor critical points** of back pressure (channels, mutexes, atomics).
- **A little back pressure is good** (balanced concerns). A lot is bad.
- **Reject new requests early** when overloaded — don't take more than you can handle.
- **Use timeouts** to release back pressure. No request runs forever.

### Patterns

#### Parallel Processing (errgroup)
```go
func processItems(ctx context.Context, items []Item) error {
    g, ctx := errgroup.WithContext(ctx)
    g.SetLimit(10) // Worker limit
    
    for _, item := range items {
        g.Go(func() error { return handle(ctx, item) })
    }
    return g.Wait()
}
```

#### Graceful Shutdown (Load Shedding)
```go
func runServer(ctx context.Context, srv *http.Server) error {
    errCh := make(chan error, 1)
    go func() { errCh <- srv.ListenAndServe() }()
    
    select {
    case err := <-errCh:
        return err
    case <-ctx.Done():
        // Stop accepting new requests, finish existing ones
        shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()
        return srv.Shutdown(shutdownCtx)
    }
}
```

### Channels
- **Focus on signaling semantics**, not data sharing.
- **Unbuffered:** Receive before Send. 100% delivery guarantee, unknown latency.
- **Buffered:** Send before Receive. Reduces blocking, no delivery guarantee.
- **Less is more with buffers.** Question buffers > 1.

### Sync Primitives
- `sync.Mutex` for shared state protection.
- `sync.RWMutex` when reads outnumber writes.
- `sync.Once` for lazy initialization.
- `errgroup.Group` for coordinated goroutines.

---

## 6. Testing

### Table-Driven Tests
```go
func TestValidateEmail(t *testing.T) {
    t.Parallel()
    tests := []struct {
        name    string
        input   string
        wantErr bool
    }{
        {"valid", "user@example.com", false},
        {"missing @", "userexample.com", true},
        {"empty", "", true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            err := ValidateEmail(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("got error %v, wantErr %v", err, tt.wantErr)
            }
        })
    }
}
```

### Rules
- **Define interfaces where used.** Consumer defines the interface (DI).
- **Inject dependencies via constructors.** Makes code testable.
- **80%+ coverage** on business logic.
- **Every bug fix gets a regression test.**

```go
// ✅ Interface defined by consumer, injected via constructor
type OrderRepository interface {
    GetByID(ctx context.Context, id string) (*Order, error)
}

type OrderService struct { repo OrderRepository }

func NewOrderService(repo OrderRepository) *OrderService {
    return &OrderService{repo: repo}
}
```

---

## 7. Dependencies & Configuration

### Dependencies
- **Research libraries first.** Check if a well-maintained library saves time or outperforms stdlib.
- **Evaluate before adopting:** maintenance, security, transitive deps, community.
- **Pin versions.** Use specific versions in `go.mod`.
- **Wrap third-party libs** with your own interfaces for testability.

### Configuration

**Main app config** (in `app/`, uses env tags):
```go
type AppConfig struct {
    Port        int    `env:"PORT" envDefault:"8080"`
    DatabaseURL string `env:"DATABASE_URL,required"`
}
```

**Component configs** (plain structs, passed explicitly):
```go
type ServerConfig struct {
    ReadTimeout  time.Duration
    WriteTimeout time.Duration
}

func NewServer(cfg ServerConfig, log *slog.Logger) *Server { ... }
```

- **Fail fast:** Validate all config at startup.
- **Pass configs explicitly** to components; no env parsing in libraries.

---

## 8. Observability

### Logging
- Use `log/slog` (Go 1.21+) or `zerolog`/`zap`.
- **JSON in production**, text locally.
- **Inject loggers** into services; don't use package-level globals.

```go
type OrderService struct {
    log  *slog.Logger
    repo OrderRepository
}

func (s *OrderService) Process(ctx context.Context, id string) error {
    s.log.InfoContext(ctx, "processing order", "order_id", id)
    // ...
}
```

### Tracing & Metrics
- **OpenTelemetry** for distributed tracing.
- **Prometheus** for metrics.
- **Propagate context** through all layers.
- **Avoid high-cardinality labels.**

---

## 9. Performance

- **Never guess.** Measurements must be relevant.
- **Profile before optimizing.** Use `pprof`, `go test -bench=. -benchmem`.
- **Preallocate slices:** `make([]T, 0, size)`.
- **Reuse buffers** with `sync.Pool` on hot paths.
- **Pass large structs by pointer.**

---

## 10. Security

- **Validate all input at the boundary.**
- **Use goqu for all SQL queries.** Never write raw SQL strings. Use `github.com/doug-martin/goqu` query builder.
- **Never log secrets.** Redact sensitive fields.
- **Use `subtle.ConstantTimeCompare`** for token comparison.

---

## 11. Tooling

| Tool | Command |
|------|---------|
| Format | `go fmt ./...` |
| Vet | `go vet ./...` |
| Lint | `make lint` |
| Vulns | `govulncheck ./...` |
| Test | `go test -race -cover ./...` |

---

## 12. Code Review Checklist

- [ ] Errors handled, not ignored
- [ ] Context propagated through calls
- [ ] Resources closed with `defer` (files, connections, responses)
- [ ] Goroutines can exit cleanly
- [ ] Tests exist for new code
- [ ] No sensitive data in logs
- [ ] Interfaces small, defined by consumers
- [ ] Functions focused, under 40 lines
- [ ] Packages provide, not contain
- [ ] Layer dependencies flow correctly (api → app → business)
- [ ] Linters pass
