# Changelog

## 0.43.0

### Breaking Changes 🛠

- Add support for go 1.26 by @giortzisg in [#1193](https://github.com/getsentry/sentry-go/pull/1193)
  - bump minimum supported go version to 1.24
- change type signature of attributes for Logs and Metrics. by @giortzisg in [#1205](https://github.com/getsentry/sentry-go/pull/1205)
  - users are not supposed to modify Attributes directly on the Log/Metric itself, but this is still is a breaking change on the type.
- Send uint64 overflowing attributes as numbers. by @giortzisg in [#1198](https://github.com/getsentry/sentry-go/pull/1198)
  - The SDK was converting overflowing uint64 attributes to strings for slog and logrus integrations. To eliminate double types for these attributes, the SDK now sends the overflowing attribute as is, and lets the server handle the overflow appropriately.
  - It is expected that overflowing unsigned integers would now get dropped, instead of converted to strings.

### New Features ✨

- Add zap logging integration by @giortzisg in [#1184](https://github.com/getsentry/sentry-go/pull/1184)
- Log specific message for RequestEntityTooLarge by @giortzisg in [#1185](https://github.com/getsentry/sentry-go/pull/1185)

### Bug Fixes 🐛

- Improve otel span map cleanup performance by @giortzisg in [#1200](https://github.com/getsentry/sentry-go/pull/1200)
- Ensure correct signal delivery on multi-client setups by @giortzisg in [#1190](https://github.com/getsentry/sentry-go/pull/1190)

### Internal Changes 🔧

#### Deps

- Bump golang.org/x/crypto to 0.48.0 by @giortzisg in [#1196](https://github.com/getsentry/sentry-go/pull/1196)
- Use go1.24.0 by @giortzisg in [#1195](https://github.com/getsentry/sentry-go/pull/1195)
- Bump github.com/gofiber/fiber/v2 from 2.52.9 to 2.52.11 in /fiber by @dependabot in [#1191](https://github.com/getsentry/sentry-go/pull/1191)
- Bump getsentry/craft from 2.19.0 to 2.20.1 by @dependabot in [#1187](https://github.com/getsentry/sentry-go/pull/1187)

#### Other

- Add omitzero and remove custom serialization by @giortzisg in [#1197](https://github.com/getsentry/sentry-go/pull/1197)
- Rename Telemetry Processor components by @giortzisg in [#1186](https://github.com/getsentry/sentry-go/pull/1186)

## 0.42.0

### Breaking Changes 🛠

- refactor Telemetry Processor to use TelemetryItem instead of ItemConvertible by @giortzisg in [#1180](https://github.com/getsentry/sentry-go/pull/1180)
  - remove ToEnvelopeItem from single log items
  - rename TelemetryBuffer to Telemetry Processor to adhere to spec
  - remove unsed ToEnvelopeItem(dsn) from Event.

### New Features ✨

- Add metric support by @aldy505 in [#1151](https://github.com/getsentry/sentry-go/pull/1151)
  - support for three metric methods (counter, gauge, distribution)
  - custom metric units
  - unexport batchlogger

### Internal Changes 🔧

#### Release

- Fix changelog-preview permissions by @BYK in [#1181](https://github.com/getsentry/sentry-go/pull/1181)
- Switch from action-prepare-release to Craft by @BYK in [#1167](https://github.com/getsentry/sentry-go/pull/1167)

#### Other

- (repo) Add Claude Code settings with basic permissions by @philipphofmann in [#1175](https://github.com/getsentry/sentry-go/pull/1175)
- Update release and changelog-preview workflows by @giortzisg in [#1177](https://github.com/getsentry/sentry-go/pull/1177)
- Bump echo to 4.10.1 by @giortzisg in [#1174](https://github.com/getsentry/sentry-go/pull/1174)

## 0.41.0

The Sentry SDK team is happy to announce the immediate availability of Sentry Go SDK v0.41.0.

### Features

- Add HTTP client integration for distributed tracing via `sentryhttpclient` package ([#876](https://github.com/getsentry/sentry-go/pull/876))
  - Provides an `http.RoundTripper` implementation that automatically creates spans for outgoing HTTP requests
  - Supports trace propagation targets configuration via `WithTracePropagationTargets` option
  - Example usage:
    ```go
    import sentryhttpclient "github.com/getsentry/sentry-go/httpclient"

    roundTripper := sentryhttpclient.NewSentryRoundTripper(nil)
    client := &http.Client{
        Transport: roundTripper,
    }
    ```
- Add `ClientOptions.PropagateTraceparent` option to control W3C `traceparent` header propagation in outgoing HTTP requests ([#1161](https://github.com/getsentry/sentry-go/pull/1161))
- Add `SpanID` field to structured logs ([#1169](https://github.com/getsentry/sentry-go/pull/1169))

## 0.40.0

The Sentry SDK team is happy to announce the immediate availability of Sentry Go SDK v0.40.0.

### Bug Fixes

- Disable `DisableTelemetryBuffer` flag and noop Telemetry Buffer, to prevent a panic at runtime ([#1149](https://github.com/getsentry/sentry-go/pull/1149)).

## 0.39.0

The Sentry SDK team is happy to announce the immediate availability of Sentry Go SDK v0.39.0.

### Features

- Drop events from the telemetry buffer when rate-limited or transport is full, allowing the buffer queue to empty itself under load ([#1138](https://github.com/getsentry/sentry-go/pull/1138)).

### Bug Fixes

- Fix scheduler's `hasWork()` method to check if buffers are ready to flush. The previous implementation was causing CPU spikes ([#1143](https://github.com/getsentry/sentry-go/pull/1143)).

## 0.38.0

### Breaking Changes

### Features

- Introduce a new async envelope transport and telemetry buffer to prioritize and batch events ([#1094](https://github.com/getsentry/sentry-go/pull/1094), [#1093](https://github.com/getsentry/sentry-go/pull/1093), [#1107](https://github.com/getsentry/sentry-go/pull/1107)).
  - Advantages:
    - Prioritized, per-category buffers (errors, transactions, logs, check-ins) reduce starvation and improve resilience under load
    - Batching for high-volume logs (up to 100 items or 5s) cuts network overhead
    - Bounded memory with eviction policies
    - Improved flush behavior with context-aware flushing
- Add `ClientOptions.DisableTelemetryBuffer` to opt out and fall back to the legacy transport layer (`HTTPTransport` / `HTTPSyncTransport`).
  
  ```go
  err := sentry.Init(sentry.ClientOptions{
    Dsn: "__DSN__",
    DisableTelemetryBuffer: true, // fallback to legacy transport
  })
  ```

### Notes

- If a custom `Transport` is provided, the SDK automatically disables the telemetry buffer and uses the legacy transport for compatibility.

## 0.37.0

The Sentry SDK team is happy to announce the immediate availability of Sentry Go SDK v0.37.0.

### Breaking Changes

- Behavioral change for the `TraceIgnoreStatusCodes` option. The option now defaults to ignoring 404 status codes ([#1122](https://github.com/getsentry/sentry-go/pull/1122)).

### Features

- Add `sentry.origin` attribute to structured logs to identify log origin for `slog` and `logrus` integrations (`auto.log.slog`, `auto.log.logrus`) ([#1121](https://github.com/getsentry/sentry-go/pull/1121)).

### Bug Fixes

- Fix `slog` event handler to use the initial context, ensuring events use the correct hub/span when the emission context lacks one ([#1133](https://github.com/getsentry/sentry-go/pull/1133)).
- Improve exception chain processing by checking pointer values when tracking visited errors, avoiding instability for certain wrapped errors ([#1132](https://github.com/getsentry/sentry-go/pull/1132)).

### Misc

- Bump `golang.org/x/net` to v0.38.0 ([#1126](https://github.com/getsentry/sentry-go/pull/1126)).

## 0.36.2

The Sentry SDK team is happy to announce the immediate availability of Sentry Go SDK v0.36.2.

### Bug Fixes

- Fix context propagation for logs to ensure logger instances correctly inherit span and hub information from their creation context ([#1118](https://github.com/getsentry/sentry-go/pull/1118))
  - Logs now properly propagate trace context from the logger's original context, even when emitted in a different context
  - The logger will first check the emission context, then fall back to its creation context, and finally to the current hub

## 0.36.1

The Sentry SDK team is happy to announce the immediate availability of Sentry Go SDK v0.36.1.

### Bug Fixes

- Prevent panic when converting error chains containing non-comparable error types by using a safe fallback for visited detection in exception conversion ([#1113](https://github.com/getsentry/sentry-go/pull/1113))

## 0.36.0

The Sentry SDK team is happy to announce the immediate availability of Sentry Go SDK v0.36.0.

### Breaking Changes

- Behavioral change for the `MaxBreadcrumbs` client option. Removed the hard limit of 100 breadcrumbs, allowing users to set a larger limit and also changed the default limit from 30 to 100 ([#1106](https://github.com/getsentry/sentry-go/pull/1106)))

- The changes to error handling ([#1075](https://github.com/getsentry/sentry-go/pull/1075)) will affect issue grouping. It is expected that any wrapped and complex errors will be grouped under a new issue group.

### Features

- Add support for improved issue grouping with enhanced error chain handling ([#1075](https://github.com/getsentry/sentry-go/pull/1075))

  The SDK now provides better handling of complex error scenarios, particularly when dealing with multiple related errors or error chains. This feature automatically detects and properly structures errors created with Go's `errors.Join()` function and other multi-error patterns.

  ```go
  // Multiple errors are now properly grouped and displayed in Sentry
  err1 := errors.New("err1")
  err2 := errors.New("err2") 
  combinedErr := errors.Join(err1, err2)
  
  // When captured, these will be shown as related exceptions in Sentry
  sentry.CaptureException(combinedErr)
  ```

- Add `TraceIgnoreStatusCodes` option to allow filtering of HTTP transactions based on status codes ([#1089](https://github.com/getsentry/sentry-go/pull/1089))
  - Configure which HTTP status codes should not be traced by providing single codes or ranges
  - Example: `TraceIgnoreStatusCodes: [][]int{{404}, {500, 599}}` ignores 404 and server errors 500-599

### Bug Fixes

- Fix logs being incorrectly filtered by `BeforeSend` callback ([#1109](https://github.com/getsentry/sentry-go/pull/1109))
  - Logs now bypass the `processEvent` method and are sent directly to the transport
  - This ensures logs are only filtered by `BeforeSendLog`, not by the error/message `BeforeSend` callback

### Misc

- Add support for Go 1.25 and drop support for Go 1.22 ([#1103](https://github.com/getsentry/sentry-go/pull/1103))

## 0.35.3

The Sentry SDK team is happy to announce the immediate availability of Sentry Go SDK v0.35.3.

### Bug Fixes

- Add missing rate limit categories ([#1082](https://github.com/getsentry/sentry-go/pull/1082))

## 0.35.2

The Sentry SDK team is happy to announce the immediate availability of Sentry Go SDK v0.35.2.

### Bug Fixes

- Fix OpenTelemetry spans being created as transactions instead of child spans ([#1073](https://github.com/getsentry/sentry-go/pull/1073))

### Misc

- Add `MockTransport` to test clients for improved testing ([#1071](https://github.com/getsentry/sentry-go/pull/1071))

## 0.35.1

The Sentry SDK team is happy to announce the immediate availability of Sentry Go SDK v0.35.1.

### Bug Fixes

- Fix race conditions when accessing the scope during logging operations ([#1050](https://github.com/getsentry/sentry-go/pull/1050))
- Fix nil pointer dereference with malformed URLs when tracing is enabled in `fasthttp` and `fiber` integrations ([#1055](https://github.com/getsentry/sentry-go/pull/1055))

### Misc

- Bump `github.com/gofiber/fiber/v2` from 2.52.5 to 2.52.9 in `/fiber` ([#1067](https://github.com/getsentry/sentry-go/pull/1067))

## 0.35.0

The Sentry SDK team is happy to announce the immediate availability of Sentry Go SDK v0.35.0.

### Breaking Changes

- Changes to the logging API ([#1046](https://github.com/getsentry/sentry-go/pull/1046))

The logging API now supports a fluent interface for structured logging with attributes:

```go
// usage before
logger := sentry.NewLogger(ctx)
// attributes weren't being set permanently
logger.SetAttributes(
    attribute.String("version", "1.0.0"),
)
logger.Infof(ctx, "Message with parameters %d and %d", 1, 2)

// new behavior
ctx := context.Background()
logger := sentry.NewLogger(ctx)

// Set permanent attributes on the logger
logger.SetAttributes(
    attribute.String("version", "1.0.0"),
)

// Chain attributes on individual log entries
logger.Info().
    String("key.string", "value").
    Int("key.int", 42).
    Bool("key.bool", true).
    Emitf("Message with parameters %d and %d", 1, 2)
```

### Bug Fixes

- Correctly serialize `FailureIssueThreshold` and `RecoveryThreshold` onto check-in payloads ([#1060](https://github.com/getsentry/sentry-go/pull/1060))

## 0.34.1

The Sentry SDK team is happy to announce the immediate availability of Sentry Go SDK v0.34.1.

### Bug Fixes

- Allow flush to be used multiple times without issues, particularly for the batch logger ([#1051](https://github.com/getsentry/sentry-go/pull/1051))
- Fix race condition in `Scope.GetSpan()` method by adding proper mutex locking ([#1044](https://github.com/getsentry/sentry-go/pull/1044))
- Guard transport on `Close()` to prevent panic when called multiple times ([#1044](https://github.com/getsentry/sentry-go/pull/1044))

## 0.34.0

The Sentry SDK team is happy to announce the immediate availability of Sentry Go SDK v0.34.0.

### Breaking Changes

- Logrus structured logging support replaces the `sentrylogrus.Hook` signature from a `*Hook` to an interface.

```go
var hook *sentrylogrus.Hook
hook = sentrylogrus.New(
    // ... your setup
)

// should change the definition to 
var hook sentrylogrus.Hook
hook = sentrylogrus.New(
    // ... your setup
)
```

### Features

- Structured logging support for [slog](https://pkg.go.dev/log/slog). ([#1033](https://github.com/getsentry/sentry-go/pull/1033))

```go
ctx := context.Background()
handler := sentryslog.Option{
    EventLevel: []slog.Level{slog.LevelError, sentryslog.LevelFatal}, // Only Error and Fatal as events
    LogLevel:   []slog.Level{slog.LevelWarn, slog.LevelInfo},         // Only Warn and Info as logs
}.NewSentryHandler(ctx)
logger := slog.New(handler)
logger.Info("hello"))
```

- Structured logging support for [logrus](https://github.com/sirupsen/logrus). ([#1036](https://github.com/getsentry/sentry-go/pull/1036))
```go
logHook, _ := sentrylogrus.NewLogHook(
    []logrus.Level{logrus.InfoLevel, logrus.WarnLevel}, 
    sentry.ClientOptions{
        Dsn: "your-dsn",
        EnableLogs: true, // Required for log entries    
    })
defer logHook.Flush(5 * time.Secod)
logrus.RegisterExitHandler(func() {
    logHook.Flush(5 * time.Second)
})

logger := logrus.New()
logger.AddHook(logHook)
logger.Infof("hello")
```

- Add support for flushing events with context using `FlushWithContext()`. ([#935](https://github.com/getsentry/sentry-go/pull/935))

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

if !sentry.FlushWithContext(ctx) {
    // Handle timeout or cancellation
}
```

- Add support for custom fingerprints in slog integration. ([#1039](https://github.com/getsentry/sentry-go/pull/1039))

### Deprecations 

- Slog structured logging support replaces `Level` option with `EventLevel` and `LogLevel` options, for specifying fine-grained levels for capturing events and logs.

```go 
handler := sentryslog.Option{
    EventLevel: []slog.Level{slog.LevelWarn, slog.LevelError, sentryslog.LevelFatal},
    LogLevel:   []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError, sentryslog.LevelFatal},
}.NewSentryHandler(ctx)
```

- Logrus structured logging support replaces `New` and `NewFromClient` functions to `NewEventHook`, `NewEventHookFromClient`, to match the newly added `NewLogHook` functions, and specify the hook type being created each time.

```go
logHook, err := sentrylogrus.NewLogHook(
    []logrus.Level{logrus.InfoLevel},
    sentry.ClientOptions{})
eventHook, err := sentrylogrus.NewEventHook([]logrus.Level{
    logrus.ErrorLevel,
    logrus.FatalLevel,
    logrus.PanicLevel,
}, sentry.ClientOptions{})
```

### Bug Fixes

- Fix issue where `ContinueTrace()` would panic when `sentry-trace` header does not exist. ([#1026](https://github.com/getsentry/sentry-go/pull/1026))
- Fix incorrect log level signature in structured logging. ([#1034](https://github.com/getsentry/sentry-go/pull/1034))
- Remove `sentry.origin` attribute from Sentry logger to prevent confusion in spans. ([#1038](https://github.com/getsentry/sentry-go/pull/1038))
- Don't gate user information behind `SendDefaultPII` flag for logs. ([#1032](https://github.com/getsentry/sentry-go/pull/1032))

### Misc

- Add more sensitive HTTP headers to the default list of headers that are scrubbed by default. ([#1008](https://github.com/getsentry/sentry-go/pull/1008))

## 0.33.0

The Sentry SDK team is happy to announce the immediate availability of Sentry Go SDK v0.33.0.


### Breaking Changes

- Rename the internal `Logger` to `DebugLogger`. This feature was only used when you set `Debug: True` in your `sentry.Init()` call. If you haven't used the Logger directly, no changes are necessary. ([#1012](https://github.com/getsentry/sentry-go/issues/1012))

### Features

- Add support for [Structured Logging](https://docs.sentry.io/product/explore/logs/). ([#1010](https://github.com/getsentry/sentry-go/issues/1010))

  ```go
  logger := sentry.NewLogger(ctx)
  logger.Info(ctx, "Hello, Logs!")
  ```

  You can learn more about Sentry Logs on our [docs](https://docs.sentry.io/product/explore/logs/) and the [examples](https://github.com/getsentry/sentry-go/blob/master/_examples/logs/main.go).

- Add new attributes APIs, which are currently only exposed on logs. ([#1007](https://github.com/getsentry/sentry-go/issues/1007))

### Bug Fixes

- Do not push a new scope on `StartSpan`. ([#1013](https://github.com/getsentry/sentry-go/issues/1013))
- Fix an issue where the propagated smapling decision wasn't used. ([#995](https://github.com/getsentry/sentry-go/issues/995))
- [Otel] Prefer `httpRoute` over `httpTarget` for span descriptions. ([#1002](https://github.com/getsentry/sentry-go/issues/1002))

### Misc

- Update `github.com/stretchr/testify` to v1.8.4. ([#988](https://github.com/getsentry/sentry-go/issues/988))  

## 0.32.0

The Sentry SDK team is happy to announce the immediate availability of Sentry Go SDK v0.32.0.

### Breaking Changes

- Bump the minimum Go version to 1.22. The supported versions are 1.22, 1.23 and 1.24. ([#967](https://github.com/getsentry/sentry-go/issues/967))
- Setting any values on `span.Extra` has no effect anymore. Use `SetData(name string, value interface{})` instead. ([#864](https://github.com/getsentry/sentry-go/pull/864))

### Features

- Add a `MockTransport` and `MockScope`. ([#972](https://github.com/getsentry/sentry-go/pull/972))

### Bug Fixes

- Fix writing `*http.Request` in the Logrus JSONFormatter. ([#955](https://github.com/getsentry/sentry-go/issues/955))

### Misc

- Transaction `data` attributes are now seralized as trace context data attributes, allowing you to query these attributes in the [Trace Explorer](https://docs.sentry.io/product/explore/traces/).

## 0.31.1

The Sentry SDK team is happy to announce the immediate availability of Sentry Go SDK v0.31.1.

### Bug Fixes

- Correct wrong module name for `sentry-go/logrus` ([#950](https://github.com/getsentry/sentry-go/pull/950))

## 0.31.0

The Sentry SDK team is happy to announce the immediate availability of Sentry Go SDK v0.31.0.

### Breaking Changes

- Remove support for metrics. Read more about the end of the Metrics beta [here](https://sentry.zendesk.com/hc/en-us/articles/26369339769883-Metrics-Beta-Ended-on-October-7th). ([#914](https://github.com/getsentry/sentry-go/pull/914))

- Remove support for profiling. ([#915](https://github.com/getsentry/sentry-go/pull/915))

- Remove `Segment` field from the `User` struct. This field is no longer used in the Sentry product. ([#928](https://github.com/getsentry/sentry-go/pull/928))

- Every integration is now a separate module, reducing the binary size and number of dependencies. Once you update `sentry-go` to latest version, you'll need to `go get` the integration you want to use. For example, if you want to use the `echo` integration, you'll need to run `go get github.com/getsentry/sentry-go/echo` ([#919](github.com/getsentry/sentry-go/pull/919)).

### Features

Add the ability to override `hub` in `context` for integrations that use custom context. ([#931](https://github.com/getsentry/sentry-go/pull/931))

- Add `HubProvider` Hook for `sentrylogrus`, enabling dynamic Sentry hub allocation for each log entry or goroutine. ([#936](https://github.com/getsentry/sentry-go/pull/936))

This change enhances compatibility with Sentry's recommendation of using separate hubs per goroutine. To ensure a separate Sentry hub for each goroutine, configure the `HubProvider` like this:

```go
hook, err := sentrylogrus.New(nil, sentry.ClientOptions{})
if err != nil {
    log.Fatalf("Failed to initialize Sentry hook: %v", err)
}

// Set a custom HubProvider to generate a new hub for each goroutine or log entry
hook.SetHubProvider(func() *sentry.Hub {
    client, _ := sentry.NewClient(sentry.ClientOptions{})
    return sentry.NewHub(client, sentry.NewScope())
})

logrus.AddHook(hook)
```

### Bug Fixes

- Add support for closing worker goroutines started by the `HTTPTranport` to prevent goroutine leaks. ([#894](https://github.com/getsentry/sentry-go/pull/894))

```go
client, _ := sentry.NewClient()
defer client.Close()
```

Worker can be also closed by calling `Close()` method on the `HTTPTransport` instance. `Close` should be called after `Flush` and before terminating the program otherwise some events may be lost.

```go
transport := sentry.NewHTTPTransport()
defer transport.Close()
```

### Misc

- Bump [gin-gonic/gin](https://github.com/gin-gonic/gin) to v1.9.1. ([#946](https://github.com/getsentry/sentry-go/pull/946))

## 0.30.0

The Sentry SDK team is happy to announce the immediate availability of Sentry Go SDK v0.30.0.

### Features

- Add `sentryzerolog` integration ([#857](https://github.com/getsentry/sentry-go/pull/857))
- Add `sentryslog` integration ([#865](https://github.com/getsentry/sentry-go/pull/865))
- Always set Mechanism Type to generic ([#896](https://github.com/getsentry/sentry-go/pull/897))

### Bug Fixes

- Prevent panic in `fasthttp` and `fiber` integration in case a malformed URL has to be parsed ([#912](https://github.com/getsentry/sentry-go/pull/912))

### Misc

Drop support for Go 1.18, 1.19 and 1.20. The currently supported Go versions are the last 3 stable releases: 1.23, 1.22 and 1.21.

## 0.29.1

The Sentry SDK team is happy to announce the immediate availability of Sentry Go SDK v0.29.1.

### Bug Fixes

- Correlate errors to the current trace ([#886](https://github.com/getsentry/sentry-go/pull/886))
- Set the trace context when the transaction finishes ([#888](https://github.com/getsentry/sentry-go/pull/888))

### Misc

- Update the `sentrynegroni` integration to use the latest (v3.1.1) version of Negroni ([#885](https://github.com/getsentry/sentry-go/pull/885))

## 0.29.0

The Sentry SDK team is happy to announce the immediate availability of Sentry Go SDK v0.29.0.

### Breaking Changes

- Remove the `sentrymartini` integration ([#861](https://github.com/getsentry/sentry-go/pull/861))
- The `WrapResponseWriter` has been moved from the `sentryhttp` package to the `internal/httputils` package. If you've imported it previosuly, you'll need to copy the implementation in your project. ([#871](https://github.com/getsentry/sentry-go/pull/871))

### Features

- Add new convenience methods to continue a trace and propagate tracing headers for error-only use cases. ([#862](https://github.com/getsentry/sentry-go/pull/862))

  If you are not using one of our integrations, you can manually continue an incoming trace by using `sentry.ContinueTrace()` by providing the `sentry-trace` and `baggage` header received from a downstream SDK.

  ```go
  hub := sentry.CurrentHub()
  sentry.ContinueTrace(hub, r.Header.Get(sentry.SentryTraceHeader), r.Header.Get(sentry.SentryBaggageHeader)),
  ```

  You can use `hub.GetTraceparent()` and `hub.GetBaggage()` to fetch the necessary header values for outgoing HTTP requests.

  ```go
  hub := sentry.GetHubFromContext(ctx)
  req, _ := http.NewRequest("GET", "http://localhost:3000", nil)
  req.Header.Add(sentry.SentryTraceHeader, hub.GetTraceparent())
  req.Header.Add(sentry.SentryBaggageHeader, hub.GetBaggage())
  ```

### Bug Fixes

- Initialize `HTTPTransport.limit` if `nil` ([#844](https://github.com/getsentry/sentry-go/pull/844))
- Fix `sentry.StartTransaction()` returning a transaction with an outdated context on existing transactions ([#854](https://github.com/getsentry/sentry-go/pull/854))
- Treat `Proxy-Authorization` as a sensitive header ([#859](https://github.com/getsentry/sentry-go/pull/859))
- Add support for the `http.Hijacker` interface to the `sentrynegroni` package ([#871](https://github.com/getsentry/sentry-go/pull/871))
- Go version >= 1.23: Use value from `http.Request.Pattern` for HTTP transaction names when using `sentryhttp` & `sentrynegroni` ([#875](https://github.com/getsentry/sentry-go/pull/875))
- Go version >= 1.21: Fix closure functions name grouping ([#877](https://github.com/getsentry/sentry-go/pull/877))

### Misc

- Collect `span` origins ([#849](https://github.com/getsentry/sentry-go/pull/849))

## 0.28.1

The Sentry SDK team is happy to announce the immediate availability of Sentry Go SDK v0.28.1.

### Bug Fixes

- Implement `http.ResponseWriter` to hook into various parts of the response process ([#837](https://github.com/getsentry/sentry-go/pull/837))

## 0.28.0

The Sentry SDK team is happy to announce the immediate availability of Sentry Go SDK v0.28.0.

### Features

- Add a `Fiber` performance tracing & error reporting integration ([#795](https://github.com/getsentry/sentry-go/pull/795))
- Add performance tracing to the `Echo` integration ([#722](https://github.com/getsentry/sentry-go/pull/722))
- Add performance tracing to the `FastHTTP` integration ([#732](https://github.com/getsentry/sentry-go/pull/723))
- Add performance tracing to the `Iris` integration ([#809](https://github.com/getsentry/sentry-go/pull/809))
- Add performance tracing to the `Negroni` integration ([#808](https://github.com/getsentry/sentry-go/pull/808))
- Add `FailureIssueThreshold` & `RecoveryThreshold` to `MonitorConfig` ([#775](https://github.com/getsentry/sentry-go/pull/775))
- Use `errors.Unwrap()` to create exception groups ([#792](https://github.com/getsentry/sentry-go/pull/792))
- Add support for matching on strings for `ClientOptions.IgnoreErrors` & `ClientOptions.IgnoreTransactions` ([#819](https://github.com/getsentry/sentry-go/pull/819))
- Add `http.request.method` attribute for performance span data ([#786](https://github.com/getsentry/sentry-go/pull/786))
- Accept `interface{}` for span data values ([#784](https://github.com/getsentry/sentry-go/pull/784))

### Bug Fixes

- Fix missing stack trace for parsing error in `logrusentry` ([#689](https://github.com/getsentry/sentry-go/pull/689))

## 0.27.0

The Sentry SDK team is happy to announce the immediate availability of Sentry Go SDK v0.27.0.

### Breaking Changes

- `Exception.ThreadId` is now typed as `uint64`. It was wrongly typed as `string` before. ([#770](https://github.com/getsentry/sentry-go/pull/770))

### Misc

- Export `Event.Attachments` ([#771](https://github.com/getsentry/sentry-go/pull/771))

## 0.26.0

The Sentry SDK team is happy to announce the immediate availability of Sentry Go SDK v0.26.0.

### Breaking Changes

As previously announced, this release removes some methods from the SDK.

- `sentry.TransactionName()` use `sentry.WithTransactionName()` instead.
- `sentry.OpName()` use `sentry.WithOpName()` instead.
- `sentry.TransctionSource()` use `sentry.WithTransactionSource()` instead.
- `sentry.SpanSampled()` use `sentry.WithSpanSampled()` instead.

### Features

- Add `WithDescription` span option ([#751](https://github.com/getsentry/sentry-go/pull/751))

  ```go
  span := sentry.StartSpan(ctx, "http.client", WithDescription("GET /api/users"))
  ```
- Add support for package name parsing in Go 1.20 and higher ([#730](https://github.com/getsentry/sentry-go/pull/730))

### Bug Fixes

- Apply `ClientOptions.SampleRate` only to errors & messages ([#754](https://github.com/getsentry/sentry-go/pull/754))
- Check if git is available before executing any git commands ([#737](https://github.com/getsentry/sentry-go/pull/737))

## 0.25.0

The Sentry SDK team is happy to announce the immediate availability of Sentry Go SDK v0.25.0.

### Breaking Changes

As previously announced, this release removes two global constants from the SDK.

- `sentry.Version` was removed. Use `sentry.SDKVersion` instead ([#727](https://github.com/getsentry/sentry-go/pull/727))
- `sentry.SDKIdentifier` was removed. Use `Client.GetSDKIdentifier()` instead ([#727](https://github.com/getsentry/sentry-go/pull/727))

### Features

- Add `ClientOptions.IgnoreTransactions`, which allows you to ignore specific transactions based on their name ([#717](https://github.com/getsentry/sentry-go/pull/717))
- Add `ClientOptions.Tags`, which allows you to set global tags that are applied to all events. You can also define tags by setting `SENTRY_TAGS_` environment variables ([#718](https://github.com/getsentry/sentry-go/pull/718))

### Bug fixes

- Fix an issue in the profiler that would cause an infinite loop if the duration of a transaction is longer than 30 seconds ([#724](https://github.com/getsentry/sentry-go/issues/724))

### Misc

- `dsn.RequestHeaders()` is not to be removed, though it is still considered deprecated and should only be used when using a custom transport that sends events to the `/store` endpoint ([#720](https://github.com/getsentry/sentry-go/pull/720))

## 0.24.1

The Sentry SDK team is happy to announce the immediate availability of Sentry Go SDK v0.24.1.

### Bug fixes

- Prevent a panic in `sentryotel.flushSpanProcessor()` ([(#711)](https://github.com/getsentry/sentry-go/pull/711))
- Prevent a panic when setting the SDK identifier ([#715](https://github.com/getsentry/sentry-go/pull/715))

## 0.24.0

The Sentry SDK team is happy to announce the immediate availability of Sentry Go SDK v0.24.0.

### Deprecations

- `sentry.Version` to be removed in 0.25.0. Use `sentry.SDKVersion` instead.
- `sentry.SDKIdentifier` to be removed in 0.25.0. Use `Client.GetSDKIdentifier()` instead.
- `dsn.RequestHeaders()` to be removed after 0.25.0, but no earlier than December 1, 2023. Requests to the `/envelope` endpoint are authenticated using the DSN in the envelope header.

### Features

- Run a single instance of the profiler instead of multiple ones for each Go routine ([#655](https://github.com/getsentry/sentry-go/pull/655))
- Use the route path as the transaction names when using the Gin integration ([#675](https://github.com/getsentry/sentry-go/pull/675))
- Set the SDK name accordingly when a framework integration is used ([#694](https://github.com/getsentry/sentry-go/pull/694))
- Read release information (VCS revision) from `debug.ReadBuildInfo` ([#704](https://github.com/getsentry/sentry-go/pull/704))

### Bug fixes

- [otel] Fix incorrect usage of `attributes.Value.AsString` ([#684](https://github.com/getsentry/sentry-go/pull/684))
- Fix trace function name parsing in profiler on go1.21+ ([#695](https://github.com/getsentry/sentry-go/pull/695))

### Misc

- Test against Go 1.21 ([#695](https://github.com/getsentry/sentry-go/pull/695))
- Make tests more robust ([#698](https://github.com/getsentry/sentry-go/pull/698), [#699](https://github.com/getsentry/sentry-go/pull/699), [#700](https://github.com/getsentry/sentry-go/pull/700), [#702](https://github.com/getsentry/sentry-go/pull/702))

## 0.23.0

The Sentry SDK team is happy to announce the immediate availability of Sentry Go SDK v0.23.0.

### Features

- Initial support for [Cron Monitoring](https://docs.sentry.io/product/crons/) ([#661](https://github.com/getsentry/sentry-go/pull/661))

  This is how the basic usage of the feature looks like:

  ```go
  // 🟡 Notify Sentry your job is running:
  checkinId := sentry.CaptureCheckIn(
    &sentry.CheckIn{
      MonitorSlug: "<monitor-slug>",
      Status:      sentry.CheckInStatusInProgress,
    },
    nil,
  )

  // Execute your scheduled task here...

  // 🟢 Notify Sentry your job has completed successfully:
  sentry.CaptureCheckIn(
    &sentry.CheckIn{
      ID:          *checkinId,
      MonitorSlug: "<monitor-slug>",
      Status:      sentry.CheckInStatusOK,
    },
    nil,
  )
  ```

  A full example of using Crons Monitoring is available [here](https://github.com/getsentry/sentry-go/blob/dde4d360660838f3c2e0ced8205bc8f7a8d312d9/_examples/crons/main.go).

  More documentation on configuring and using Crons [can be found here](https://docs.sentry.io/platforms/go/crons/).

- Add support for [Event Attachments](https://docs.sentry.io/platforms/go/enriching-events/attachments/) ([#670](https://github.com/getsentry/sentry-go/pull/670))

  It's now possible to add file/binary payloads to Sentry events:

  ```go
  sentry.ConfigureScope(func(scope *sentry.Scope) {
    scope.AddAttachment(&Attachment{
      Filename:    "report.html",
      ContentType: "text/html",
      Payload:     []byte("<h1>Look, HTML</h1>"),
    })
  })
  ```

  The attachment will then be accessible on the Issue Details page.

- Add sampling decision to trace envelope header ([#666](https://github.com/getsentry/sentry-go/pull/666))
- Expose SpanFromContext function ([#672](https://github.com/getsentry/sentry-go/pull/672))

### Bug fixes

- Make `Span.Finish` a no-op when the span is already finished ([#660](https://github.com/getsentry/sentry-go/pull/660))

## 0.22.0

The Sentry SDK team is happy to announce the immediate availability of Sentry Go SDK v0.22.0.

This release contains initial [profiling](https://docs.sentry.io/product/profiling/) support, as well as a few bug fixes and improvements.

### Features

- Initial (alpha) support for [profiling](https://docs.sentry.io/product/profiling/) ([#626](https://github.com/getsentry/sentry-go/pull/626))

  Profiling is disabled by default. To enable it, configure both `TracesSampleRate` and `ProfilesSampleRate` when initializing the SDK:

  ```go
  err := sentry.Init(sentry.ClientOptions{
    Dsn: "__DSN__",
    EnableTracing: true,
    TracesSampleRate: 1.0,
    // The sampling rate for profiling is relative to TracesSampleRate. In this case, we'll capture profiles for 100% of transactions.
    ProfilesSampleRate: 1.0,
  })
  ```

  More documentation on profiling and current limitations [can be found here](https://docs.sentry.io/platforms/go/profiling/).

- Add transactions/tracing support go the Gin integration ([#644](https://github.com/getsentry/sentry-go/pull/644))

### Bug fixes

- Always set a valid source on transactions ([#637](https://github.com/getsentry/sentry-go/pull/637))
- Clone scope.Context in more places to avoid panics on concurrent reads and writes ([#638](https://github.com/getsentry/sentry-go/pull/638))
  - Fixes [#570](https://github.com/getsentry/sentry-go/issues/570)
- Fix frames recognized as not being in-app still showing as in-app ([#647](https://github.com/getsentry/sentry-go/pull/647))

## 0.21.0

The Sentry SDK team is happy to announce the immediate availability of Sentry Go SDK v0.21.0.

Note: this release includes one **breaking change** and some **deprecations**, which are listed below.

### Breaking Changes

**This change does not apply if you use [https://sentry.io](https://sentry.io)**

- Remove support for the `/store` endpoint ([#631](https://github.com/getsentry/sentry-go/pull/631))
  - This change requires a self-hosted version of Sentry 20.6.0 or higher. If you are using a version of [self-hosted Sentry](https://develop.sentry.dev/self-hosted/) (aka *on-premise*) older than 20.6.0, then you will need to [upgrade](https://develop.sentry.dev/self-hosted/releases/) your instance.

### Features

- Rename four span option functions ([#611](https://github.com/getsentry/sentry-go/pull/611), [#624](https://github.com/getsentry/sentry-go/pull/624))
  - `TransctionSource` -> `WithTransactionSource`
  - `SpanSampled` -> `WithSpanSampled`
  - `OpName` -> `WithOpName`
  - `TransactionName` -> `WithTransactionName`
  - Old functions `TransctionSource`, `SpanSampled`, `OpName`, and `TransactionName` are still available but are now **deprecated** and will be removed in a future release.
- Make `client.EventFromMessage` and `client.EventFromException` methods public ([#607](https://github.com/getsentry/sentry-go/pull/607))
- Add `client.SetException` method ([#607](https://github.com/getsentry/sentry-go/pull/607))
  - This allows to set or add errors to an existing `Event`.

### Bug Fixes

- Protect from panics while doing concurrent reads/writes to Span data fields ([#609](https://github.com/getsentry/sentry-go/pull/609))
- [otel] Improve detection of Sentry-related spans ([#632](https://github.com/getsentry/sentry-go/pull/632), [#636](https://github.com/getsentry/sentry-go/pull/636))
  - Fixes cases when HTTP spans containing requests to Sentry were captured by Sentry ([#627](https://github.com/getsentry/sentry-go/issues/627))

### Misc

- Drop testing in (legacy) GOPATH mode ([#618](https://github.com/getsentry/sentry-go/pull/618))
- Remove outdated documentation from https://pkg.go.dev/github.com/getsentry/sentry-go ([#623](https://github.com/getsentry/sentry-go/pull/623))

## 0.20.0

The Sentry SDK team is happy to announce the immediate availability of Sentry Go SDK v0.20.0.

Note: this release has some **breaking changes**, which are listed below.

### Breaking Changes

- Remove the following methods: `Scope.SetTransaction()`, `Scope.Transaction()` ([#605](https://github.com/getsentry/sentry-go/pull/605))

  Span.Name should be used instead to access the transaction's name.

  For example, the following [`TracesSampler`](https://docs.sentry.io/platforms/go/configuration/sampling/#setting-a-sampling-function) function should be now written as follows:

  **Before:**
  ```go
  TracesSampler: func(ctx sentry.SamplingContext) float64 {
    hub := sentry.GetHubFromContext(ctx.Span.Context())
    if hub.Scope().Transaction() == "GET /health" {
      return 0
    }
    return 1
  },
  ```

  **After:**
  ```go
  TracesSampler: func(ctx sentry.SamplingContext) float64 {
    if ctx.Span.Name == "GET /health" {
      return 0
    }
    return 1
  },
  ```

### Features

- Add `Span.SetContext()` method ([#599](https://github.com/getsentry/sentry-go/pull/599/))
  - It is recommended to use it instead of `hub.Scope().SetContext` when setting or updating context on transactions.
- Add `DebugMeta` interface to `Event` and extend `Frame` structure with more fields ([#606](https://github.com/getsentry/sentry-go/pull/606))
  - More about DebugMeta interface [here](https://develop.sentry.dev/sdk/event-payloads/debugmeta/).

### Bug Fixes

- [otel] Fix missing OpenTelemetry context on some events ([#599](https://github.com/getsentry/sentry-go/pull/599), [#605](https://github.com/getsentry/sentry-go/pull/605))
  - Fixes ([#596](https://github.com/getsentry/sentry-go/issues/596)).
- [otel] Better handling for HTTP span attributes ([#610](https://github.com/getsentry/sentry-go/pull/610))

### Misc

- Bump minimum versions: `github.com/kataras/iris/v12` to 12.2.0, `github.com/labstack/echo/v4` to v4.10.0 ([#595](https://github.com/getsentry/sentry-go/pull/595))
  - Resolves [GO-2022-1144 / CVE-2022-41717](https://deps.dev/advisory/osv/GO-2022-1144), [GO-2023-1495 / CVE-2022-41721](https://deps.dev/advisory/osv/GO-2023-1495), [GO-2022-1059 / CVE-2022-32149](https://deps.dev/advisory/osv/GO-2022-1059).
- Bump `google.golang.org/protobuf` minimum required version to 1.29.1  ([#604](https://github.com/getsentry/sentry-go/pull/604))
  - This fixes a potential denial of service issue ([CVE-2023-24535](https://github.com/advisories/GHSA-hw7c-3rfg-p46j)).
- Exclude the `otel` module when building in GOPATH mode ([#615](https://github.com/getsentry/sentry-go/pull/615))

## 0.19.0

The Sentry SDK team is happy to announce the immediate availability of Sentry Go SDK v0.19.0.

### Features

- Add support for exception mechanism metadata ([#564](https://github.com/getsentry/sentry-go/pull/564/))
  - More about exception mechanisms [here](https://develop.sentry.dev/sdk/event-payloads/exception/#exception-mechanism).

### Bug Fixes
- [otel] Use the correct "trace" context when sending a Sentry error ([#580](https://github.com/getsentry/sentry-go/pull/580/))


### Misc
- Drop support for Go 1.17, add support for Go 1.20 ([#563](https://github.com/getsentry/sentry-go/pull/563/))
  - According to our policy, we're officially supporting the last three minor releases of Go.
- Switch repository license to MIT ([#583](https://github.com/getsentry/sentry-go/pull/583/))
  - More about Sentry licensing [here](https://open.sentry.io/licensing/).
- Bump `golang.org/x/text` minimum required version to 0.3.8 ([#586](https://github.com/getsentry/sentry-go/pull/586))
  - This fixes [CVE-2022-32149](https://github.com/advisories/GHSA-69ch-w2m2-3vjp) vulnerability.

## 0.18.0

The Sentry SDK team is happy to announce the immediate availability of Sentry Go SDK v0.18.0.
This release contains initial support for [OpenTelemetry](https://opentelemetry.io/) and various other bug fixes and improvements.

**Note**: This is the last release supporting Go 1.17.

### Features

- Initial support for [OpenTelemetry](https://opentelemetry.io/).
  You can now send all your OpenTelemetry spans to Sentry.

  Install the `otel` module

  ```bash
  go get github.com/getsentry/sentry-go \
         github.com/getsentry/sentry-go/otel
  ```

  Configure the Sentry and OpenTelemetry SDKs

  ```go
  import (
      "go.opentelemetry.io/otel"
      sdktrace "go.opentelemetry.io/otel/sdk/trace"
      "github.com/getsentry/sentry-go"
      "github.com/getsentry/sentry-go/otel"
      // ...
  )

  // Initlaize the Sentry SDK
  sentry.Init(sentry.ClientOptions{
      Dsn:              "__DSN__",
      EnableTracing:    true,
      TracesSampleRate: 1.0,
  })

  // Set up the Sentry span processor
  tp := sdktrace.NewTracerProvider(
      sdktrace.WithSpanProcessor(sentryotel.NewSentrySpanProcessor()),
      // ...
  )
  otel.SetTracerProvider(tp)

  // Set up the Sentry propagator
  otel.SetTextMapPropagator(sentryotel.NewSentryPropagator())
  ```

  You can read more about using OpenTelemetry with Sentry in our [docs](https://docs.sentry.io/platforms/go/performance/instrumentation/opentelemetry/).

### Bug Fixes

- Do not freeze the Dynamic Sampling Context when no Sentry values are present in the baggage header ([#532](https://github.com/getsentry/sentry-go/pull/532))
- Create a frozen Dynamic Sampling Context when calling `span.ToBaggage()` ([#566](https://github.com/getsentry/sentry-go/pull/566))
- Fix baggage parsing and encoding in vendored otel package ([#568](https://github.com/getsentry/sentry-go/pull/568))

### Misc

- Add `Span.SetDynamicSamplingContext()` ([#539](https://github.com/getsentry/sentry-go/pull/539/))
- Add various getters for `Dsn` ([#540](https://github.com/getsentry/sentry-go/pull/540))
- Add `SpanOption::SpanSampled` ([#546](https://github.com/getsentry/sentry-go/pull/546))
- Add `Span.SetData()` ([#542](https://github.com/getsentry/sentry-go/pull/542))
- Add `Span.IsTransaction()` ([#543](https://github.com/getsentry/sentry-go/pull/543))
- Add `Span.GetTransaction()` method ([#558](https://github.com/getsentry/sentry-go/pull/558))

## 0.17.0

The Sentry SDK team is happy to announce the immediate availability of Sentry Go SDK v0.17.0.
This release contains a new `BeforeSendTransaction` hook option and corrects two regressions introduced in `0.16.0`.

### Features

- Add `BeforeSendTransaction` hook to `ClientOptions` ([#517](https://github.com/getsentry/sentry-go/pull/517))
  - Here's [an example](https://github.com/getsentry/sentry-go/blob/master/_examples/http/main.go#L56-L66) of how BeforeSendTransaction can be used to modify or drop transaction events.

### Bug Fixes

- Do not crash in Span.Finish() when the Client is empty [#520](https://github.com/getsentry/sentry-go/pull/520)
  - Fixes [#518](https://github.com/getsentry/sentry-go/issues/518)
- Attach non-PII/non-sensitive request headers to events when `ClientOptions.SendDefaultPii` is set to `false` ([#524](https://github.com/getsentry/sentry-go/pull/524))
  - Fixes [#523](https://github.com/getsentry/sentry-go/issues/523)

### Misc

- Clarify how to handle logrus.Fatalf events ([#501](https://github.com/getsentry/sentry-go/pull/501/))
- Rename the `examples` directory to `_examples` ([#521](https://github.com/getsentry/sentry-go/pull/521))
  - This removes an indirect dependency to `github.com/golang-jwt/jwt`

## 0.16.0

The Sentry SDK team is happy to announce the immediate availability of Sentry Go SDK v0.16.0.
Due to ongoing work towards a stable API for `v1.0.0`, we sadly had to include **two breaking changes** in this release.

### Breaking Changes

- Add `EnableTracing`, a boolean option flag to enable performance monitoring (`false` by default).
   - If you're using `TracesSampleRate` or `TracesSampler`, this option is **required** to enable performance monitoring.

      ```go
      sentry.Init(sentry.ClientOptions{
          EnableTracing: true,
          TracesSampleRate: 1.0,
      })
      ```
- Unify TracesSampler [#498](https://github.com/getsentry/sentry-go/pull/498)
    - `TracesSampler` was changed to a callback that must return a `float64` between `0.0` and `1.0`.

       For example, you can apply a sample rate of `1.0` (100%) to all `/api` transactions, and a sample rate of `0.5` (50%) to all other transactions.
       You can read more about this in our [SDK docs](https://docs.sentry.io/platforms/go/configuration/filtering/#using-sampling-to-filter-transaction-events).

       ```go
       sentry.Init(sentry.ClientOptions{
           TracesSampler: sentry.TracesSampler(func(ctx sentry.SamplingContext) float64 {
                hub := sentry.GetHubFromContext(ctx.Span.Context())
                name := hub.Scope().Transaction()

                if strings.HasPrefix(name, "GET /api") {
                    return 1.0
                }

                return 0.5
            }),
        }
        ```

### Features

- Send errors logged with [Logrus](https://github.com/sirupsen/logrus) to Sentry.
    - Have a look at our [logrus examples](https://github.com/getsentry/sentry-go/blob/master/_examples/logrus/main.go) on how to use the integration.
- Add support for Dynamic Sampling [#491](https://github.com/getsentry/sentry-go/pull/491)
    - You can read more about Dynamic Sampling in our [product docs](https://docs.sentry.io/product/data-management-settings/dynamic-sampling/).
- Add detailed logging about the reason transactions are being dropped.
    - You can enable SDK logging via `sentry.ClientOptions.Debug: true`.

### Bug Fixes

- Do not clone the hub when calling `StartTransaction` [#505](https://github.com/getsentry/sentry-go/pull/505)
    - Fixes [#502](https://github.com/getsentry/sentry-go/issues/502)

## 0.15.0

- fix: Scope values should not override Event values (#446)
- feat: Make maximum amount of spans configurable (#460)
- feat: Add a method to start a transaction (#482)
- feat: Extend User interface by adding Data, Name and Segment (#483)
- feat: Add ClientOptions.SendDefaultPII (#485)

## 0.14.0

- feat: Add function to continue from trace string (#434)
- feat: Add `max-depth` options (#428)
- *[breaking]* ref: Use a `Context` type mapping to a `map[string]interface{}` for all event contexts (#444)
- *[breaking]* ref: Replace deprecated `ioutil` pkg with `os` & `io` (#454)
- ref: Optimize `stacktrace.go` from size and speed (#467)
- ci: Test against `go1.19` and `go1.18`, drop `go1.16` and `go1.15` support (#432, #477)
- deps: Dependency update to fix CVEs (#462, #464, #477)

_NOTE:_ This version drops support for Go 1.16 and Go 1.15. The currently supported Go versions are the last 3 stable releases: 1.19, 1.18 and 1.17.

## v0.13.0

- ref: Change DSN ProjectID to be a string (#420)
- fix: When extracting PCs from stack frames, try the `PC` field (#393)
- build: Bump gin-gonic/gin from v1.4.0 to v1.7.7 (#412)
- build: Bump Go version in go.mod (#410)
- ci: Bump golangci-lint version in GH workflow (#419)
- ci: Update GraphQL config with appropriate permissions (#417)
- ci: ci: Add craft release automation (#422)

## v0.12.0

- feat: Automatic Release detection (#363, #369, #386, #400)
- fix: Do not change Hub.lastEventID for transactions (#379)
- fix: Do not clear LastEventID when events are dropped (#382)
- Updates to documentation (#366, #385)

_NOTE:_
This version drops support for Go 1.14, however no changes have been made that would make the SDK not work with Go 1.14. The currently supported Go versions are the last 3 stable releases: 1.15, 1.16 and 1.17.
There are two behavior changes related to `LastEventID`, both of which were intended to align the behavior of the Sentry Go SDK with other Sentry SDKs.
The new [automatic release detection feature](https://github.com/getsentry/sentry-go/issues/335) makes it easier to use Sentry and separate events per release without requiring extra work from users. We intend to improve this functionality in a future release by utilizing information that will be available in runtime starting with Go 1.18. The tracking issue is [#401](https://github.com/getsentry/sentry-go/issues/401).

## v0.11.0

- feat(transports): Category-based Rate Limiting ([#354](https://github.com/getsentry/sentry-go/pull/354))
- feat(transports): Report User-Agent identifying SDK ([#357](https://github.com/getsentry/sentry-go/pull/357))
- fix(scope): Include event processors in clone ([#349](https://github.com/getsentry/sentry-go/pull/349))
- Improvements to `go doc` documentation ([#344](https://github.com/getsentry/sentry-go/pull/344), [#350](https://github.com/getsentry/sentry-go/pull/350), [#351](https://github.com/getsentry/sentry-go/pull/351))
- Miscellaneous changes to our testing infrastructure with GitHub Actions
  ([57123a40](https://github.com/getsentry/sentry-go/commit/57123a409be55f61b1d5a6da93c176c55a399ad0), [#128](https://github.com/getsentry/sentry-go/pull/128), [#338](https://github.com/getsentry/sentry-go/pull/338), [#345](https://github.com/getsentry/sentry-go/pull/345), [#346](https://github.com/getsentry/sentry-go/pull/346), [#352](https://github.com/getsentry/sentry-go/pull/352), [#353](https://github.com/getsentry/sentry-go/pull/353), [#355](https://github.com/getsentry/sentry-go/pull/355))

_NOTE:_
This version drops support for Go 1.13. The currently supported Go versions are the last 3 stable releases: 1.14, 1.15 and 1.16.
Users of the tracing functionality (`StartSpan`, etc) should upgrade to this version to benefit from separate rate limits for errors and transactions.
There are no breaking changes and upgrading should be a smooth experience for all users.

## v0.10.0

- feat: Debug connection reuse (#323)
- fix: Send root span data as `Event.Extra` (#329)
- fix: Do not double sample transactions (#328)
- fix: Do not override trace context of transactions (#327)
- fix: Drain and close API response bodies (#322)
- ci: Run tests against Go tip (#319)
- ci: Move away from Travis in favor of GitHub Actions (#314) (#321)

## v0.9.0

- feat: Initial tracing and performance monitoring support (#285)
- doc: Revamp sentryhttp documentation (#304)
- fix: Hub.PopScope never empties the scope stack (#300)
- ref: Report Event.Timestamp in local time (#299)
- ref: Report Breadcrumb.Timestamp in local time (#299)

_NOTE:_
This version introduces support for [Sentry's Performance Monitoring](https://docs.sentry.io/platforms/go/performance/).
The new tracing capabilities are beta, and we plan to expand them on future versions. Feedback is welcome, please open new issues on GitHub.
The `sentryhttp` package got better API docs, an [updated usage example](https://github.com/getsentry/sentry-go/tree/master/_examples/http) and support for creating automatic transactions as part of Performance Monitoring.

## v0.8.0

- build: Bump required version of Iris (#296)
- fix: avoid unnecessary allocation in Client.processEvent (#293)
- doc: Remove deprecation of sentryhttp.HandleFunc (#284)
- ref: Update sentryhttp example (#283)
- doc: Improve documentation of sentryhttp package (#282)
- doc: Clarify SampleRate documentation (#279)
- fix: Remove RawStacktrace (#278)
- docs: Add example of custom HTTP transport
- ci: Test against go1.15, drop go1.12 support (#271)

_NOTE:_
This version comes with a few updates. Some examples and documentation have been
improved. We've bumped the supported version of the Iris framework to avoid
LGPL-licensed modules in the module dependency graph.
The `Exception.RawStacktrace` and `Thread.RawStacktrace` fields have been
removed to conform to Sentry's ingestion protocol, only `Exception.Stacktrace`
and `Thread.Stacktrace` should appear in user code.

## v0.7.0

- feat: Include original error when event cannot be encoded as JSON (#258)
- feat: Use Hub from request context when available (#217, #259)
- feat: Extract stack frames from golang.org/x/xerrors (#262)
- feat: Make Environment Integration preserve existing context data (#261)
- feat: Recover and RecoverWithContext with arbitrary types (#268)
- feat: Report bad usage of CaptureMessage and CaptureEvent (#269)
- feat: Send debug logging to stderr by default (#266)
- feat: Several improvements to documentation (#223, #245, #250, #265)
- feat: Example of Recover followed by panic (#241, #247)
- feat: Add Transactions and Spans (to support OpenTelemetry Sentry Exporter) (#235, #243, #254)
- fix: Set either Frame.Filename or Frame.AbsPath (#233)
- fix: Clone requestBody to new Scope (#244)
- fix: Synchronize access and mutation of Hub.lastEventID (#264)
- fix: Avoid repeated syscalls in prepareEvent (#256)
- fix: Do not allocate new RNG for every event (#256)
- fix: Remove stale replace directive in go.mod (#255)
- fix(http): Deprecate HandleFunc, remove duplication (#260)

_NOTE:_
This version comes packed with several fixes and improvements and no breaking
changes.
Notably, there is a change in how the SDK reports file names in stack traces
that should resolve any ambiguity when looking at stack traces and using the
Suspect Commits feature.
We recommend all users to upgrade.

## v0.6.1

- fix: Use NewEvent to init Event struct (#220)

_NOTE:_
A change introduced in v0.6.0 with the intent of avoiding allocations made a
pattern used in official examples break in certain circumstances (attempting
to write to a nil map).
This release reverts the change such that maps in the Event struct are always
allocated.

## v0.6.0

- feat: Read module dependencies from runtime/debug (#199)
- feat: Support chained errors using Unwrap (#206)
- feat: Report chain of errors when available (#185)
- **[breaking]** fix: Accept http.RoundTripper to customize transport (#205)
  Before the SDK accepted a concrete value of type `*http.Transport` in
  `ClientOptions`, now it accepts any value implementing the `http.RoundTripper`
  interface. Note that `*http.Transport` implements `http.RoundTripper`, so most
  code bases will continue to work unchanged.
  Users of custom transport gain the ability to pass in other implementations of
  `http.RoundTripper` and may be able to simplify their code bases.
- fix: Do not panic when scope event processor drops event (#192)
- **[breaking]** fix: Use time.Time for timestamps (#191)
  Users of sentry-go typically do not need to manipulate timestamps manually.
  For those who do, the field type changed from `int64` to `time.Time`, which
  should be more convenient to use. The recommended way to get the current time
  is `time.Now().UTC()`.
- fix: Report usage error including stack trace (#189)
- feat: Add Exception.ThreadID field (#183)
- ci: Test against Go 1.14, drop 1.11 (#170)
- feat: Limit reading bytes from request bodies (#168)
- **[breaking]** fix: Rename fasthttp integration package sentryhttp => sentryfasthttp
  The current recommendation is to use a named import, in which case existing
  code should not require any change:
  ```go
  package main

  import (
  	"fmt"

  	"github.com/getsentry/sentry-go"
  	sentryfasthttp "github.com/getsentry/sentry-go/fasthttp"
  	"github.com/valyala/fasthttp"
  )
  ```

_NOTE:_
This version includes some new features and a few breaking changes, none of
which should pose troubles with upgrading. Most code bases should be able to
upgrade without any changes.

## v0.5.1

- fix: Ignore err.Cause() when it is nil (#160)

## v0.5.0

- fix: Synchronize access to HTTPTransport.disabledUntil (#158)
- docs: Update Flush documentation (#153)
- fix: HTTPTransport.Flush panic and data race (#140)

_NOTE:_
This version changes the implementation of the default transport, modifying the
behavior of `sentry.Flush`. The previous behavior was to wait until there were
no buffered events; new concurrent events kept `Flush` from returning. The new
behavior is to wait until the last event prior to the call to `Flush` has been
sent or the timeout; new concurrent events have no effect. The new behavior is
inline with the [Unified API
Guidelines](https://docs.sentry.io/development/sdk-dev/unified-api/).

We have updated the documentation and examples to clarify that `Flush` is meant
to be called typically only once before program termination, to wait for
in-flight events to be sent to Sentry. Calling `Flush` after every event is not
recommended, as it introduces unnecessary latency to the surrounding function.
Please verify the usage of `sentry.Flush` in your code base.

## v0.4.0

- fix(stacktrace): Correctly report package names (#127)
- fix(stacktrace): Do not rely on AbsPath of files (#123)
- build: Require github.com/ugorji/go@v1.1.7 (#110)
- fix: Correctly store last event id (#99)
- fix: Include request body in event payload (#94)
- build: Reset go.mod version to 1.11 (#109)
- fix: Eliminate data race in modules integration (#105)
- feat: Add support for path prefixes in the DSN (#102)
- feat: Add HTTPClient option (#86)
- feat: Extract correct type and value from top-most error (#85)
- feat: Check for broken pipe errors in Gin integration (#82)
- fix: Client.CaptureMessage accept nil EventModifier (#72)

## v0.3.1

- feat: Send extra information exposed by the Go runtime (#76)
- fix: Handle new lines in module integration (#65)
- fix: Make sure that cache is locked when updating for contextifyFramesIntegration
- ref: Update Iris integration and example to version 12
- misc: Remove indirect dependencies in order to move them to separate go.mod files

## v0.3.0

- feat: Retry event marshaling without contextual data if the first pass fails
- fix: Include `url.Parse` error in `DsnParseError`
- fix: Make more `Scope` methods safe for concurrency
- fix: Synchronize concurrent access to `Hub.client`
- ref: Remove mutex from `Scope` exported API
- ref: Remove mutex from `Hub` exported API
- ref: Compile regexps for `filterFrames` only once
- ref: Change `SampleRate` type to `float64`
- doc: `Scope.Clear` not safe for concurrent use
- ci: Test sentry-go with `go1.13`, drop `go1.10`

_NOTE:_
This version removes some of the internal APIs that landed publicly (namely `Hub/Scope` mutex structs) and may require (but shouldn't) some changes to your code.
It's not done through major version update, as we are still in `0.x` stage.

## v0.2.1

- fix: Run `Contextify` integration on `Threads` as well

## v0.2.0

- feat: Add `SetTransaction()` method on the `Scope`
- feat: `fasthttp` framework support with `sentryfasthttp` package
- fix: Add `RWMutex` locks to internal `Hub` and `Scope` changes

## v0.1.3

- feat: Move frames context reading into `contextifyFramesIntegration` (#28)

_NOTE:_
In case of any performance issues due to source contexts IO, you can let us know and turn off the integration in the meantime with:

```go
sentry.Init(sentry.ClientOptions{
	Integrations: func(integrations []sentry.Integration) []sentry.Integration {
		var filteredIntegrations []sentry.Integration
		for _, integration := range integrations {
			if integration.Name() == "ContextifyFrames" {
				continue
			}
			filteredIntegrations = append(filteredIntegrations, integration)
		}
		return filteredIntegrations
	},
})
```

## v0.1.2

- feat: Better source code location resolution and more useful inapp frames (#26)
- feat: Use `noopTransport` when no `Dsn` provided (#27)
- ref: Allow empty `Dsn` instead of returning an error (#22)
- fix: Use `NewScope` instead of literal struct inside a `scope.Clear` call (#24)
- fix: Add to `WaitGroup` before the request is put inside a buffer (#25)

## v0.1.1

- fix: Check for initialized `Client` in `AddBreadcrumbs` (#20)
- build: Bump version when releasing with Craft (#19)

## v0.1.0

- First stable release! \o/

## v0.0.1-beta.5

- feat: **[breaking]** Add `NewHTTPTransport` and `NewHTTPSyncTransport` which accepts all transport options
- feat: New `HTTPSyncTransport` that blocks after each call
- feat: New `Echo` integration
- ref: **[breaking]** Remove `BufferSize` option from `ClientOptions` and move it to `HTTPTransport` instead
- ref: Export default `HTTPTransport`
- ref: Export `net/http` integration handler
- ref: Set `Request` instantly in the package handlers, not in `recoverWithSentry` so it can be accessed later on
- ci: Add craft config

## v0.0.1-beta.4

- feat: `IgnoreErrors` client option and corresponding integration
- ref: Reworked `net/http` integration, wrote better example and complete readme
- ref: Reworked `Gin` integration, wrote better example and complete readme
- ref: Reworked `Iris` integration, wrote better example and complete readme
- ref: Reworked `Negroni` integration, wrote better example and complete readme
- ref: Reworked `Martini` integration, wrote better example and complete readme
- ref: Remove `Handle()` from frameworks handlers and return it directly from New

## v0.0.1-beta.3

- feat: `Iris` framework support with `sentryiris` package
- feat: `Gin` framework support with `sentrygin` package
- feat: `Martini` framework support with `sentrymartini` package
- feat: `Negroni` framework support with `sentrynegroni` package
- feat: Add `Hub.Clone()` for easier frameworks integration
- feat: Return `EventID` from `Recovery` methods
- feat: Add `NewScope` and `NewEvent` functions and use them in the whole codebase
- feat: Add `AddEventProcessor` to the `Client`
- fix: Operate on requests body copy instead of the original
- ref: Try to read source files from the root directory, based on the filename as well, to make it work on AWS Lambda
- ref: Remove `gocertifi` dependence and document how to provide your own certificates
- ref: **[breaking]** Remove `Decorate` and `DecorateFunc` methods in favor of `sentryhttp` package
- ref: **[breaking]** Allow for integrations to live on the client, by passing client instance in `SetupOnce` method
- ref: **[breaking]** Remove `GetIntegration` from the `Hub`
- ref: **[breaking]** Remove `GlobalEventProcessors` getter from the public API

## v0.0.1-beta.2

- feat: Add `AttachStacktrace` client option to include stacktrace for messages
- feat: Add `BufferSize` client option to configure transport buffer size
- feat: Add `SetRequest` method on a `Scope` to control `Request` context data
- feat: Add `FromHTTPRequest` for `Request` type for easier extraction
- ref: Extract `Request` information more accurately
- fix: Attach `ServerName`, `Release`, `Dist`, `Environment` options to the event
- fix: Don't log events dropped due to full transport buffer as sent
- fix: Don't panic and create an appropriate event when called `CaptureException` or `Recover` with `nil` value

## v0.0.1-beta

- Initial release
