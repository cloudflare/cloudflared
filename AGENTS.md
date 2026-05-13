# Cloudflared

Cloudflare's command-line tool and networking daemon written in Go.
Production-grade tunneling and network connectivity services used by millions of
developers and organizations worldwide.

## Essential Commands

### Build & Test (Always run before commits)

```bash
# Full development check (run before any commit)
make test lint

# Build for current platform
make cloudflared

# Run all unit tests with coverage
make test
make cover

# Run specific test
go test -run TestFunctionName ./path/to/package

# Run tests with race detection
go test -race ./...
```

### Platform-Specific Builds

```bash
# Linux
TARGET_OS=linux TARGET_ARCH=amd64 make cloudflared

# Windows
TARGET_OS=windows TARGET_ARCH=amd64 make cloudflared

# macOS ARM64
TARGET_OS=darwin TARGET_ARCH=arm64 make cloudflared

# FIPS compliant build
FIPS=true make cloudflared
```

### Code Quality & Formatting

```bash
# Run linter (38+ enabled linters)
make lint

# Auto-fix formatting
make fmt
gofmt -w .
goimports -w .

# Security scanning
make vet

# Component tests (Python integration tests)
cd component-tests && python -m pytest test_file.py::test_function_name
```

Notes on linting:

- `.golangci.yaml` is configured with `new-from-rev` and `whole-files: true`.
  Touching a file triggers linting of the ENTIRE file, not just the changed
  hunks. Expect to fix pre-existing issues in files you modify, or add
  targeted `// nolint: <linter>` comments with a short justification.
- Prefer `defer func() { _ = resource.Close() }()` over `defer resource.Close()`
  for `io.Closer` values whose error truly does not matter — this satisfies
  `errcheck` without hiding real failures elsewhere.

## Project Knowledge

### Package Structure

- Use meaningful package names that reflect functionality
- Package names should be lowercase, single words when possible
- Avoid generic names like `util`, `common`, `helper`

#### Well-known shared packages

- `crypto/`: Single source of truth for TLS curve preferences and other
  cryptographic primitives shared by every edge-facing transport. Import as
  `cfdcrypto "github.com/cloudflare/cloudflared/crypto"` to avoid colliding
  with the standard library's `crypto` package. Do NOT duplicate TLS curve
  or cipher selection logic in other packages.
- `tlsconfig/`: Builds the base `*tls.Config` used for edge connections
  (`CreateTunnelConfig`) and loads origin/CA pools. Curve selection is
  intentionally NOT set here; it is applied per-connection from the
  `crypto/` package so the same config can be cloned and reused across
  protocols.
- `features/`: Runtime feature flags including `PostQuantumMode`
  (`PostQuantumPrefer` = default, `PostQuantumStrict` = `--post-quantum`).
- `fips/`: Build-tag driven FIPS detection. Only `fips.IsFipsEnabled()` is
  exposed; never branch on `fipsEnabled` inside a function if the two
  branches return the same value.

### Function and Method Guidelines

```go
// Good: Clear purpose, proper error handling
func (c *Connection) HandleRequest(ctx context.Context, req *http.Request) error {
    if req == nil {
        return errors.New("request cannot be nil")
    }
    // Implementation...
    return nil
}
```

### Error Handling

- Always handle errors explicitly, never ignore them
- Use `fmt.Errorf` for error wrapping
- Create meaningful error messages with context
- Use error variables for common errors

```go
// Good error handling patterns
if err != nil {
    return fmt.Errorf("failed to process connection: %w", err)
}
```

### Logging Standards

- Use `github.com/rs/zerolog` for structured logging
- Include relevant context fields
- Use appropriate log levels (Debug, Info, Warn, Error)

```go
logger.Info().
    Str("tunnelID", tunnel.ID).
    Int("connIndex", connIndex).
    Msg("Connection established")
```

### Testing Patterns

- Use `github.com/stretchr/testify` for assertions
- Test files end with `_test.go`
- Use table-driven tests for multiple scenarios
- Always use `t.Parallel()` for parallel-safe tests
- Use meaningful test names that describe behavior

```go
func TestMetricsListenerCreation(t *testing.T) {
    t.Parallel()
    // Test implementation
    assert.Equal(t, expected, actual)
    require.NoError(t, err)
}
```

### Constants and Variables

```go
const (
    MaxGracePeriod       = time.Minute * 3
    MaxConcurrentStreams = math.MaxUint32
    LogFieldConnIndex    = "connIndex"
)

var (
    // Group related variables
    switchingProtocolText = fmt.Sprintf("%d %s", http.StatusSwitchingProtocols, http.StatusText(http.StatusSwitchingProtocols))
    flushableContentTypes = []string{sseContentType, grpcContentType, sseJsonContentType}
)
```

### Type Definitions

- Define interfaces close to their usage
- Keep interfaces small and focused
- Use descriptive names for complex types

```go
type TunnelConnection interface {
    Serve(ctx context.Context) error
}

type TunnelProperties struct {
    Credentials    Credentials
    QuickTunnelUrl string
}
```

## Key Architectural Patterns

### Context Usage

- Always accept `context.Context` as first parameter for long-running operations
- Respect context cancellation in loops and blocking operations
- Pass context through call chains

### Concurrency

- Use channels for goroutine communication
- Protect shared state with mutexes
- Prefer `sync.RWMutex` for read-heavy workloads
- `*tls.Config` values stored in shared maps (e.g.
  `TunnelConfig.EdgeTLSConfigs`) must be `Clone()`d before mutating
  per-connection fields like `CurvePreferences` or `NextProtos`. Writing
  through the shared pointer races with concurrent connection attempts.

### TLS & Post-Quantum key exchange

- Per-connection TLS configuration for edge connections is built via
  `cfdcrypto.TLSConfigWithCurvePreferences(tlsConfig, pqMode)`. It clones
  the provided `*tls.Config` and sets `CurvePreferences` based on `pqMode`,
  so callers never need to clone or mutate `CurvePreferences` themselves.
  Do NOT reach for the package-private `getCurvePreferences` helper; the
  exported `TLSConfigWithCurvePreferences` is the only supported entry
  point.
- Two PQ modes are supported and apply identically to QUIC and HTTP/2:
  - `PostQuantumPrefer` (default): `[X25519MLKEM768, P256Kyber768Draft00, CurveP256]`
  - `PostQuantumStrict` (`--post-quantum`): `[X25519MLKEM768, P256Kyber768Draft00]`
- FIPS and non-FIPS builds use the same curve list. Do NOT reintroduce a
  `fipsEnabled` branch in curve-selection code; if the two modes ever
  diverge, express the divergence inside `crypto/` so call sites remain
  untouched.
- HTTP/2 supports post-quantum handshakes. Never re-add a
  `PostQuantumStrict`-based rejection to H2 code paths, and never force
  `--post-quantum` to select QUIC-only in protocol selection.

### Configuration

- Use structured configuration with validation
- Support both file-based and CLI flag configuration
- Provide sensible defaults

### Metrics and Observability

- Instrument code with Prometheus metrics
- Use OpenTelemetry for distributed tracing
- Include structured logging with relevant context

## Boundaries

### ✅ Always Do

- Run `make test lint` before any commit
- Handle all errors explicitly with proper context
- Use `github.com/rs/zerolog` for all logging
- Add `t.Parallel()` to all parallel-safe tests
- Follow the import grouping conventions
- Use meaningful variable and function names
- Include context.Context for long-running operations
- Close resources in defer statements

### ⚠️ Ask First Before

- Adding new dependencies to go.mod
- Modifying CI/CD configuration files
- Changing build system or Makefile
- Modifying component test infrastructure
- Adding new linter rules or changing golangci-lint config
- Making breaking changes to public APIs
- Changing logging levels or structured logging fields

### 🚫 Never Do

- Ignore errors without explicit handling (`_ = err`)
- Use generic package names (`util`, `helper`, `common`)
- Commit code that fails `make test lint`
- Use `fmt.Print*` instead of structured logging
- Modify vendor dependencies directly
- Commit secrets, credentials, or sensitive data
- Use deprecated or unsafe Go patterns
- Skip testing for new functionality
- Remove existing tests unless they're genuinely invalid

## Dependencies Management

- Use Go modules (`go.mod`) exclusively
- Vendor dependencies for reproducible builds
- Keep dependencies up-to-date and secure
- Prefer standard library when possible
- Cloudflared uses a fork of quic-go always check release notes before bumping
  this dependency.

## Security Considerations

- FIPS compliance support available
- Vulnerability scanning integrated in CI
- Credential handling follows security best practices
- Network security with TLS/QUIC protocols
- Regular security audits and updates
- Post quantum encryption

## Common Patterns to Follow

1. **Graceful shutdown**: Always implement proper cleanup
2. **Resource management**: Close resources in defer statements
3. **Error propagation**: Wrap errors with meaningful context
4. **Configuration validation**: Validate inputs early
5. **Logging consistency**: Use structured logging throughout
6. **Testing coverage**: Aim for comprehensive test coverage
7. **Documentation**: Comment exported functions and types

Remember: This is a mission-critical networking tool used in production by many
organizations. Code quality, security, and reliability are paramount.
