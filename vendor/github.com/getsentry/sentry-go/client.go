package sentry

import (
	"context"
	"crypto/x509"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/getsentry/sentry-go/internal/debug"
	"github.com/getsentry/sentry-go/internal/debuglog"
	httpInternal "github.com/getsentry/sentry-go/internal/http"
	"github.com/getsentry/sentry-go/internal/protocol"
	"github.com/getsentry/sentry-go/internal/ratelimit"
	"github.com/getsentry/sentry-go/internal/telemetry"
)

// The identifier of the SDK.
const sdkIdentifier = "sentry.go"

const (
	// maxErrorDepth is the maximum number of errors reported in a chain of errors.
	// This protects the SDK from an arbitrarily long chain of wrapped errors.
	//
	// An additional consideration is that arguably reporting a long chain of errors
	// is of little use when debugging production errors with Sentry. The Sentry UI
	// is not optimized for long chains either. The top-level error together with a
	// stack trace is often the most useful information.
	maxErrorDepth = 100

	// defaultMaxSpans limits the default number of recorded spans per transaction. The limit is
	// meant to bound memory usage and prevent too large transaction events that
	// would be rejected by Sentry.
	defaultMaxSpans = 1000

	// defaultMaxBreadcrumbs is the default maximum number of breadcrumbs added to
	// an event. Can be overwritten with the MaxBreadcrumbs option.
	defaultMaxBreadcrumbs = 100
)

// hostname is the host name reported by the kernel. It is precomputed once to
// avoid syscalls when capturing events.
//
// The error is ignored because retrieving the host name is best-effort. If the
// error is non-nil, there is nothing to do other than retrying. We choose not
// to retry for now.
var hostname, _ = os.Hostname()

// lockedRand is a random number generator safe for concurrent use. Its API is
// intentionally limited and it is not meant as a full replacement for a
// rand.Rand.
type lockedRand struct {
	mu sync.Mutex
	r  *rand.Rand
}

// Float64 returns a pseudo-random number in [0.0,1.0).
func (r *lockedRand) Float64() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.r.Float64()
}

// rng is the internal random number generator.
//
// We do not use the global functions from math/rand because, while they are
// safe for concurrent use, any package in a build could change the seed and
// affect the generated numbers, for instance making them deterministic. On the
// other hand, the source returned from rand.NewSource is not safe for
// concurrent use, so we need to couple its use with a sync.Mutex.
var rng = &lockedRand{
	// #nosec G404 -- We are fine using transparent, non-secure value here.
	r: rand.New(rand.NewSource(time.Now().UnixNano())),
}

// usageError is used to report to Sentry an SDK usage error.
//
// It is not exported because it is never returned by any function or method in
// the exported API.
type usageError struct {
	error
}

// DebugLogger is an instance of log.Logger that is used to provide debug information about running Sentry Client
// can be enabled by either using debuglog.SetOutput directly or with Debug client option.
var DebugLogger = debuglog.GetLogger()

// EventProcessor is a function that processes an event.
// Event processors are used to change an event before it is sent to Sentry.
type EventProcessor func(event *Event, hint *EventHint) *Event

// EventModifier is the interface that wraps the ApplyToEvent method.
//
// ApplyToEvent changes an event based on external data and/or
// an event hint.
type EventModifier interface {
	ApplyToEvent(event *Event, hint *EventHint, client *Client) *Event
}

var globalEventProcessors []EventProcessor

// AddGlobalEventProcessor adds processor to the global list of event
// processors. Global event processors apply to all events.
//
// AddGlobalEventProcessor is deprecated. Most users will prefer to initialize
// the SDK with Init and provide a ClientOptions.BeforeSend function or use
// Scope.AddEventProcessor instead.
func AddGlobalEventProcessor(processor EventProcessor) {
	globalEventProcessors = append(globalEventProcessors, processor)
}

// Integration allows for registering a functions that modify or discard captured events.
type Integration interface {
	Name() string
	SetupOnce(client *Client)
}

// ClientOptions that configures a SDK Client.
type ClientOptions struct {
	// The DSN to use. If the DSN is not set, the client is effectively
	// disabled.
	Dsn string
	// In debug mode, the debug information is printed to stdout to help you
	// understand what sentry is doing.
	Debug bool
	// Configures whether SDK should generate and attach stacktraces to pure
	// capture message calls.
	AttachStacktrace bool
	// The sample rate for event submission in the range [0.0, 1.0]. By default,
	// all events are sent. Thus, as a historical special case, the sample rate
	// 0.0 is treated as if it was 1.0. To drop all events, set the DSN to the
	// empty string.
	SampleRate float64
	// Enable performance tracing.
	EnableTracing bool
	// The sample rate for sampling traces in the range [0.0, 1.0].
	TracesSampleRate float64
	// Used to customize the sampling of traces, overrides TracesSampleRate.
	TracesSampler TracesSampler
	// Control with URLs trace propagation should be enabled. Does not support regex patterns.
	TracePropagationTargets []string
	// PropagateTraceparent is used to control whether the W3C Trace Context HTTP traceparent header
	// is propagated on outgoing http requests.
	PropagateTraceparent bool
	// List of regexp strings that will be used to match against event's message
	// and if applicable, caught errors type and value.
	// If the match is found, then a whole event will be dropped.
	IgnoreErrors []string
	// List of regexp strings that will be used to match against a transaction's
	// name.  If a match is found, then the transaction  will be dropped.
	IgnoreTransactions []string
	// If this flag is enabled, certain personally identifiable information (PII) is added by active integrations.
	// By default, no such data is sent.
	SendDefaultPII bool
	// BeforeSend is called before error events are sent to Sentry.
	// You can use it to mutate the event or return nil to discard it.
	BeforeSend func(event *Event, hint *EventHint) *Event
	// BeforeSendLong is called before log events are sent to Sentry.
	// You can use it to mutate the log event or return nil to discard it.
	BeforeSendLog func(event *Log) *Log
	// BeforeSendTransaction is called before transaction events are sent to Sentry.
	// Use it to mutate the transaction or return nil to discard the transaction.
	BeforeSendTransaction func(event *Event, hint *EventHint) *Event
	// Before breadcrumb add callback.
	BeforeBreadcrumb func(breadcrumb *Breadcrumb, hint *BreadcrumbHint) *Breadcrumb
	// BeforeSendMetric is called before metric events are sent to Sentry.
	// You can use it to mutate the metric or return nil to discard it.
	BeforeSendMetric func(metric *Metric) *Metric
	// Integrations to be installed on the current Client, receives default
	// integrations.
	Integrations func([]Integration) []Integration
	// io.Writer implementation that should be used with the Debug mode.
	DebugWriter io.Writer
	// The transport to use. Defaults to HTTPTransport.
	Transport Transport
	// The server name to be reported.
	ServerName string
	// The release to be sent with events.
	//
	// Some Sentry features are built around releases, and, thus, reporting
	// events with a non-empty release improves the product experience. See
	// https://docs.sentry.io/product/releases/.
	//
	// If Release is not set, the SDK will try to derive a default value
	// from environment variables or the Git repository in the working
	// directory.
	//
	// If you distribute a compiled binary, it is recommended to set the
	// Release value explicitly at build time. As an example, you can use:
	//
	// 	go build -ldflags='-X main.release=VALUE'
	//
	// That will set the value of a predeclared variable 'release' in the
	// 'main' package to 'VALUE'. Then, use that variable when initializing
	// the SDK:
	//
	// 	sentry.Init(ClientOptions{Release: release})
	//
	// See https://golang.org/cmd/go/ and https://golang.org/cmd/link/ for
	// the official documentation of -ldflags and -X, respectively.
	Release string
	// The dist to be sent with events.
	Dist string
	// The environment to be sent with events.
	Environment string
	// Maximum number of breadcrumbs
	// when MaxBreadcrumbs is negative then ignore breadcrumbs.
	MaxBreadcrumbs int
	// Maximum number of spans.
	//
	// See https://develop.sentry.dev/sdk/envelopes/#size-limits for size limits
	// applied during event ingestion. Events that exceed these limits might get dropped.
	MaxSpans int
	// An optional pointer to http.Client that will be used with a default
	// HTTPTransport. Using your own client will make HTTPTransport, HTTPProxy,
	// HTTPSProxy and CaCerts options ignored.
	HTTPClient *http.Client
	// An optional pointer to http.Transport that will be used with a default
	// HTTPTransport. Using your own transport will make HTTPProxy, HTTPSProxy
	// and CaCerts options ignored.
	HTTPTransport http.RoundTripper
	// An optional HTTP proxy to use.
	// This will default to the HTTP_PROXY environment variable.
	HTTPProxy string
	// An optional HTTPS proxy to use.
	// This will default to the HTTPS_PROXY environment variable.
	// HTTPS_PROXY takes precedence over HTTP_PROXY for https requests.
	HTTPSProxy string
	// An optional set of SSL certificates to use.
	CaCerts *x509.CertPool
	// MaxErrorDepth is the maximum number of errors reported in a chain of errors.
	// This protects the SDK from an arbitrarily long chain of wrapped errors.
	//
	// An additional consideration is that arguably reporting a long chain of errors
	// is of little use when debugging production errors with Sentry. The Sentry UI
	// is not optimized for long chains either. The top-level error together with a
	// stack trace is often the most useful information.
	MaxErrorDepth int
	// Default event tags. These are overridden by tags set on a scope.
	Tags map[string]string
	// EnableLogs controls when logs should be emitted.
	EnableLogs bool
	// DisableMetrics controls when metrics should be emitted.
	DisableMetrics bool
	// TraceIgnoreStatusCodes is a list of HTTP status codes that should not be traced.
	// Each element can be either:
	// - A single-element slice [code] for a specific status code
	// - A two-element slice [min, max] for a range of status codes (inclusive)
	// When an HTTP request results in a status code that matches any of these codes or ranges,
	// the transaction will not be sent to Sentry.
	//
	// Examples:
	//   [][]int{{404}}                           // ignore only status code 404
	//   [][]int{{400, 405}}                     // ignore status codes 400-405
	//   [][]int{{404}, {500}}                   // ignore status codes 404 and 500
	//   [][]int{{404}, {400, 405}, {500, 599}}  // ignore 404, range 400-405, and range 500-599
	//
	// By default, this ignores 404 status codes.
	//
	// IMPORTANT: to not ignore any status codes, the option should be an empty slice and not nil. The nil option is
	// used for defaulting to 404 ignores.
	TraceIgnoreStatusCodes [][]int
	// DisableTelemetryBuffer disables the telemetry buffer layer for prioritizing events and uses the old transport layer.
	DisableTelemetryBuffer bool
}

// Client is the underlying processor that is used by the main API and Hub
// instances. It must be created with NewClient.
type Client struct {
	mu              sync.RWMutex
	options         ClientOptions
	dsn             *Dsn
	eventProcessors []EventProcessor
	integrations    []Integration
	sdkIdentifier   string
	sdkVersion      string
	// Transport is read-only. Replacing the transport of an existing client is
	// not supported, create a new client instead.
	Transport          Transport
	batchLogger        *logBatchProcessor
	batchMeter         *metricBatchProcessor
	telemetryProcessor *telemetry.Processor
}

// NewClient creates and returns an instance of Client configured using
// ClientOptions.
//
// Most users will not create clients directly. Instead, initialize the SDK with
// Init and use the package-level functions (for simple programs that run on a
// single goroutine) or hub methods (for concurrent programs, for example web
// servers).
func NewClient(options ClientOptions) (*Client, error) {
	// The default error event sample rate for all SDKs is 1.0 (send all).
	//
	// In Go, the zero value (default) for float64 is 0.0, which means that
	// constructing a client with NewClient(ClientOptions{}), or, equivalently,
	// initializing the SDK with Init(ClientOptions{}) without an explicit
	// SampleRate would drop all events.
	//
	// To retain the desired default behavior, we exceptionally flip SampleRate
	// from 0.0 to 1.0 here. Setting the sample rate to 0.0 is not very useful
	// anyway, and the same end result can be achieved in many other ways like
	// not initializing the SDK, setting the DSN to the empty string or using an
	// event processor that always returns nil.
	//
	// An alternative API could be such that default options don't need to be
	// the same as Go's zero values, for example using the Functional Options
	// pattern. That would either require a breaking change if we want to reuse
	// the obvious NewClient name, or a new function as an alternative
	// constructor.
	if options.SampleRate == 0.0 {
		options.SampleRate = 1.0
	}

	if options.Debug {
		debugWriter := options.DebugWriter
		if debugWriter == nil {
			debugWriter = os.Stderr
		}
		debuglog.SetOutput(debugWriter)
	}

	if options.Dsn == "" {
		options.Dsn = os.Getenv("SENTRY_DSN")
	}

	if options.Release == "" {
		options.Release = defaultRelease()
	}

	if options.Environment == "" {
		options.Environment = os.Getenv("SENTRY_ENVIRONMENT")
	}

	if options.MaxErrorDepth == 0 {
		options.MaxErrorDepth = maxErrorDepth
	}

	if options.MaxSpans == 0 {
		options.MaxSpans = defaultMaxSpans
	}

	if options.TraceIgnoreStatusCodes == nil {
		options.TraceIgnoreStatusCodes = [][]int{{404}}
	}

	// SENTRYGODEBUG is a comma-separated list of key=value pairs (similar
	// to GODEBUG). It is not a supported feature: recognized debug options
	// may change any time.
	//
	// The intended public is SDK developers. It is orthogonal to
	// options.Debug, which is also available for SDK users.
	dbg := strings.Split(os.Getenv("SENTRYGODEBUG"), ",")
	sort.Strings(dbg)
	// dbgOpt returns true when the given debug option is enabled, for
	// example SENTRYGODEBUG=someopt=1.
	dbgOpt := func(opt string) bool {
		s := opt + "=1"
		return dbg[sort.SearchStrings(dbg, s)%len(dbg)] == s
	}
	if dbgOpt("httpdump") || dbgOpt("httptrace") {
		options.HTTPTransport = &debug.Transport{
			RoundTripper: http.DefaultTransport,
			Output:       os.Stderr,
			Dump:         dbgOpt("httpdump"),
			Trace:        dbgOpt("httptrace"),
		}
	}

	var dsn *Dsn
	if options.Dsn != "" {
		var err error
		dsn, err = NewDsn(options.Dsn)
		if err != nil {
			return nil, err
		}
	}

	client := Client{
		options:       options,
		dsn:           dsn,
		sdkIdentifier: sdkIdentifier,
		sdkVersion:    SDKVersion,
	}

	client.setupTransport()

	// noop Telemetry Buffers and Processor fow now
	// if !options.DisableTelemetryBuffer {
	// 	client.setupTelemetryProcessor()
	// } else
	if options.EnableLogs {
		client.batchLogger = newLogBatchProcessor(&client)
		client.batchLogger.Start()
	}

	if !options.DisableMetrics {
		client.batchMeter = newMetricBatchProcessor(&client)
		client.batchMeter.Start()
	}

	client.setupIntegrations()

	return &client, nil
}

func (client *Client) setupTransport() {
	opts := client.options
	transport := opts.Transport

	if transport == nil {
		if opts.Dsn == "" {
			transport = new(noopTransport)
		} else {
			transport = NewHTTPTransport()
		}
	}

	transport.Configure(opts)
	client.Transport = transport
}

func (client *Client) setupTelemetryProcessor() { // nolint: unused
	if client.options.DisableTelemetryBuffer {
		return
	}

	if client.dsn == nil {
		debuglog.Println("Telemetry buffer disabled: no DSN configured")
		return
	}

	// We currently disallow using custom Transport with the new Telemetry Processor, due to the difference in transport signatures.
	// The option should be enabled when the new Transport interface signature changes.
	if client.options.Transport != nil {
		debuglog.Println("Cannot enable Telemetry Processor/Buffers with custom Transport: fallback to old transport")
		if client.options.EnableLogs {
			client.batchLogger = newLogBatchProcessor(client)
			client.batchLogger.Start()
		}
		if !client.options.DisableMetrics {
			client.batchMeter = newMetricBatchProcessor(client)
			client.batchMeter.Start()
		}
		return
	}

	transport := httpInternal.NewAsyncTransport(httpInternal.TransportOptions{
		Dsn:           client.options.Dsn,
		HTTPClient:    client.options.HTTPClient,
		HTTPTransport: client.options.HTTPTransport,
		HTTPProxy:     client.options.HTTPProxy,
		HTTPSProxy:    client.options.HTTPSProxy,
		CaCerts:       client.options.CaCerts,
	})
	client.Transport = &internalAsyncTransportAdapter{transport: transport}

	buffers := map[ratelimit.Category]telemetry.Buffer[protocol.TelemetryItem]{
		ratelimit.CategoryError:       telemetry.NewRingBuffer[protocol.TelemetryItem](ratelimit.CategoryError, 100, telemetry.OverflowPolicyDropOldest, 1, 0),
		ratelimit.CategoryTransaction: telemetry.NewRingBuffer[protocol.TelemetryItem](ratelimit.CategoryTransaction, 1000, telemetry.OverflowPolicyDropOldest, 1, 0),
		ratelimit.CategoryLog:         telemetry.NewRingBuffer[protocol.TelemetryItem](ratelimit.CategoryLog, 10*100, telemetry.OverflowPolicyDropOldest, 100, 5*time.Second),
		ratelimit.CategoryMonitor:     telemetry.NewRingBuffer[protocol.TelemetryItem](ratelimit.CategoryMonitor, 100, telemetry.OverflowPolicyDropOldest, 1, 0),
		ratelimit.CategoryTraceMetric: telemetry.NewRingBuffer[protocol.TelemetryItem](ratelimit.CategoryTraceMetric, 10*100, telemetry.OverflowPolicyDropOldest, 100, 5*time.Second),
	}

	sdkInfo := &protocol.SdkInfo{
		Name:    client.sdkIdentifier,
		Version: client.sdkVersion,
	}

	client.telemetryProcessor = telemetry.NewProcessor(buffers, transport, &client.dsn.Dsn, sdkInfo)
}

func (client *Client) setupIntegrations() {
	integrations := []Integration{
		new(contextifyFramesIntegration),
		new(environmentIntegration),
		new(modulesIntegration),
		new(ignoreErrorsIntegration),
		new(ignoreTransactionsIntegration),
		new(globalTagsIntegration),
	}

	if client.options.Integrations != nil {
		integrations = client.options.Integrations(integrations)
	}

	for _, integration := range integrations {
		if client.integrationAlreadyInstalled(integration.Name()) {
			debuglog.Printf("Integration %s is already installed\n", integration.Name())
			continue
		}
		client.integrations = append(client.integrations, integration)
		integration.SetupOnce(client)
		debuglog.Printf("Integration installed: %s\n", integration.Name())
	}

	sort.Slice(client.integrations, func(i, j int) bool {
		return client.integrations[i].Name() < client.integrations[j].Name()
	})
}

// AddEventProcessor adds an event processor to the client. It must not be
// called from concurrent goroutines. Most users will prefer to use
// ClientOptions.BeforeSend or Scope.AddEventProcessor instead.
//
// Note that typical programs have only a single client created by Init and the
// client is shared among multiple hubs, one per goroutine, such that adding an
// event processor to the client affects all hubs that share the client.
func (client *Client) AddEventProcessor(processor EventProcessor) {
	client.eventProcessors = append(client.eventProcessors, processor)
}

// Options return ClientOptions for the current Client.
func (client *Client) Options() ClientOptions {
	// Note: internally, consider using `client.options` instead of `client.Options()` to avoid copying the object each time.
	return client.options
}

// CaptureMessage captures an arbitrary message.
func (client *Client) CaptureMessage(message string, hint *EventHint, scope EventModifier) *EventID {
	event := client.EventFromMessage(message, LevelInfo)
	return client.CaptureEvent(event, hint, scope)
}

// CaptureException captures an error.
func (client *Client) CaptureException(exception error, hint *EventHint, scope EventModifier) *EventID {
	event := client.EventFromException(exception, LevelError)
	return client.CaptureEvent(event, hint, scope)
}

// CaptureCheckIn captures a check in.
func (client *Client) CaptureCheckIn(checkIn *CheckIn, monitorConfig *MonitorConfig, scope EventModifier) *EventID {
	event := client.EventFromCheckIn(checkIn, monitorConfig)
	if event != nil && event.CheckIn != nil {
		client.CaptureEvent(event, nil, scope)
		return &event.CheckIn.ID
	}
	return nil
}

// CaptureEvent captures an event on the currently active client if any.
//
// The event must already be assembled. Typically, code would instead use
// the utility methods like CaptureException. The return value is the
// event ID. In case Sentry is disabled or event was dropped, the return value will be nil.
func (client *Client) CaptureEvent(event *Event, hint *EventHint, scope EventModifier) *EventID {
	return client.processEvent(event, hint, scope)
}

func (client *Client) captureLog(log *Log, _ *Scope) bool {
	if log == nil {
		return false
	}

	if client.options.BeforeSendLog != nil {
		log = client.options.BeforeSendLog(log)
		if log == nil {
			debuglog.Println("Log dropped due to BeforeSendLog callback.")
			return false
		}
	}

	if client.telemetryProcessor != nil {
		if !client.telemetryProcessor.Add(log) {
			debuglog.Print("Dropping log: telemetry buffer full or category missing")
			return false
		}
	} else if client.batchLogger != nil {
		if !client.batchLogger.Send(log) {
			debuglog.Printf("Dropping log [%s]: buffer full", log.Level)
			return false
		}
	}

	return true
}

func (client *Client) captureMetric(metric *Metric, _ *Scope) bool {
	if metric == nil {
		return false
	}

	if client.options.BeforeSendMetric != nil {
		metric = client.options.BeforeSendMetric(metric)
		if metric == nil {
			debuglog.Println("Metric dropped due to BeforeSendMetric callback.")
			return false
		}
	}

	if client.telemetryProcessor != nil {
		if !client.telemetryProcessor.Add(metric) {
			debuglog.Printf("Dropping metric: telemetry buffer full or category missing")
			return false
		}
	} else if client.batchMeter != nil {
		if !client.batchMeter.Send(metric) {
			debuglog.Printf("Dropping metric %q: buffer full", metric.Name)
			return false
		}
	}

	return true
}

// Recover captures a panic.
// Returns EventID if successfully, or nil if there's no error to recover from.
func (client *Client) Recover(err interface{}, hint *EventHint, scope EventModifier) *EventID {
	if err == nil {
		err = recover()
	}

	// Normally we would not pass a nil Context, but RecoverWithContext doesn't
	// use the Context for communicating deadline nor cancelation. All it does
	// is store the Context in the EventHint and there nil means the Context is
	// not available.
	// nolint: staticcheck
	return client.RecoverWithContext(nil, err, hint, scope)
}

// RecoverWithContext captures a panic and passes relevant context object.
// Returns EventID if successfully, or nil if there's no error to recover from.
func (client *Client) RecoverWithContext(
	ctx context.Context,
	err interface{},
	hint *EventHint,
	scope EventModifier,
) *EventID {
	if err == nil {
		err = recover()
	}
	if err == nil {
		return nil
	}

	if ctx != nil {
		if hint == nil {
			hint = &EventHint{}
		}
		if hint.Context == nil {
			hint.Context = ctx
		}
	}

	var event *Event
	switch err := err.(type) {
	case error:
		event = client.EventFromException(err, LevelFatal)
	case string:
		event = client.EventFromMessage(err, LevelFatal)
	default:
		event = client.EventFromMessage(fmt.Sprintf("%#v", err), LevelFatal)
	}
	return client.CaptureEvent(event, hint, scope)
}

// Flush waits until the underlying Transport sends any buffered events to the
// Sentry server, blocking for at most the given timeout. It returns false if
// the timeout was reached. In that case, some events may not have been sent.
//
// Flush should be called before terminating the program to avoid
// unintentionally dropping events.
//
// Do not call Flush indiscriminately after every call to CaptureEvent,
// CaptureException or CaptureMessage. Instead, to have the SDK send events over
// the network synchronously, configure it to use the HTTPSyncTransport in the
// call to Init.
func (client *Client) Flush(timeout time.Duration) bool {
	if client.batchLogger != nil || client.batchMeter != nil || client.telemetryProcessor != nil {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		return client.FlushWithContext(ctx)
	}
	return client.Transport.Flush(timeout)
}

// FlushWithContext waits until the underlying Transport sends any buffered events
// to the Sentry server, blocking for at most the duration specified by the context.
// It returns false if the context is canceled before the events are sent. In such a case,
// some events may not be delivered.
//
// FlushWithContext should be called before terminating the program to ensure no
// events are unintentionally dropped.
//
// Avoid calling FlushWithContext indiscriminately after each call to CaptureEvent,
// CaptureException, or CaptureMessage. To send events synchronously over the network,
// configure the SDK to use HTTPSyncTransport during initialization with Init.

func (client *Client) FlushWithContext(ctx context.Context) bool {
	if client.batchLogger != nil {
		client.batchLogger.Flush(ctx.Done())
	}
	if client.batchMeter != nil {
		client.batchMeter.Flush(ctx.Done())
	}
	if client.telemetryProcessor != nil {
		return client.telemetryProcessor.FlushWithContext(ctx)
	}
	return client.Transport.FlushWithContext(ctx)
}

// Close clean up underlying Transport resources.
//
// Close should be called after Flush and before terminating the program
// otherwise some events may be lost.
func (client *Client) Close() {
	if client.telemetryProcessor != nil {
		client.telemetryProcessor.Close(5 * time.Second)
	}
	if client.batchLogger != nil {
		client.batchLogger.Shutdown()
	}
	if client.batchMeter != nil {
		client.batchMeter.Shutdown()
	}
	client.Transport.Close()
}

// EventFromMessage creates an event from the given message string.
func (client *Client) EventFromMessage(message string, level Level) *Event {
	if message == "" {
		err := usageError{fmt.Errorf("%s called with empty message", callerFunctionName())}
		return client.EventFromException(err, level)
	}
	event := NewEvent()
	event.Level = level
	event.Message = message

	if client.options.AttachStacktrace {
		event.Threads = []Thread{{
			Stacktrace: NewStacktrace(),
			Crashed:    false,
			Current:    true,
		}}
	}

	return event
}

// EventFromException creates a new Sentry event from the given `error` instance.
func (client *Client) EventFromException(exception error, level Level) *Event {
	event := NewEvent()
	event.Level = level

	err := exception
	if err == nil {
		err = usageError{fmt.Errorf("%s called with nil error", callerFunctionName())}
	}

	event.SetException(err, client.options.MaxErrorDepth)

	return event
}

// EventFromCheckIn creates a new Sentry event from the given `check_in` instance.
func (client *Client) EventFromCheckIn(checkIn *CheckIn, monitorConfig *MonitorConfig) *Event {
	if checkIn == nil {
		return nil
	}

	event := NewEvent()
	event.Type = checkInType

	var checkInID EventID
	if checkIn.ID == "" {
		checkInID = EventID(uuid())
	} else {
		checkInID = checkIn.ID
	}

	event.CheckIn = &CheckIn{
		ID:          checkInID,
		MonitorSlug: checkIn.MonitorSlug,
		Status:      checkIn.Status,
		Duration:    checkIn.Duration,
	}
	event.MonitorConfig = monitorConfig

	return event
}

func (client *Client) SetSDKIdentifier(identifier string) {
	client.mu.Lock()
	defer client.mu.Unlock()

	client.sdkIdentifier = identifier
}

func (client *Client) GetSDKIdentifier() string {
	client.mu.RLock()
	defer client.mu.RUnlock()

	return client.sdkIdentifier
}

func (client *Client) processEvent(event *Event, hint *EventHint, scope EventModifier) *EventID {
	if event == nil {
		err := usageError{fmt.Errorf("%s called with nil event", callerFunctionName())}
		return client.CaptureException(err, hint, scope)
	}

	// Transactions are sampled by options.TracesSampleRate or
	// options.TracesSampler when they are started. Other events
	// (errors, messages) are sampled here. Does not apply to check-ins.
	if event.Type != transactionType && event.Type != checkInType && !sample(client.options.SampleRate) {
		debuglog.Println("Event dropped due to SampleRate hit.")
		return nil
	}

	if event = client.prepareEvent(event, hint, scope); event == nil {
		return nil
	}

	// Apply beforeSend* processors
	if hint == nil {
		hint = &EventHint{}
	}
	switch event.Type {
	case transactionType:
		if client.options.BeforeSendTransaction != nil {
			if event = client.options.BeforeSendTransaction(event, hint); event == nil {
				debuglog.Println("Transaction dropped due to BeforeSendTransaction callback.")
				return nil
			}
		}
	case checkInType: // not a default case, since we shouldn't apply BeforeSend on check-in events
	default:
		if client.options.BeforeSend != nil {
			if event = client.options.BeforeSend(event, hint); event == nil {
				debuglog.Println("Event dropped due to BeforeSend callback.")
				return nil
			}
		}
	}

	if client.telemetryProcessor != nil {
		if !client.telemetryProcessor.Add(event) {
			debuglog.Println("Event dropped: telemetry buffer full or unavailable")
		}
	} else {
		client.Transport.SendEvent(event)
	}

	return &event.EventID
}

func (client *Client) prepareEvent(event *Event, hint *EventHint, scope EventModifier) *Event {
	if event.EventID == "" {
		// TODO set EventID when the event is created, same as in other SDKs. It's necessary for profileTransaction.ID.
		event.EventID = EventID(uuid())
	}

	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	if event.Level == "" {
		event.Level = LevelInfo
	}

	if event.ServerName == "" {
		event.ServerName = client.options.ServerName

		if event.ServerName == "" {
			event.ServerName = hostname
		}
	}

	if event.Release == "" {
		event.Release = client.options.Release
	}

	if event.Dist == "" {
		event.Dist = client.options.Dist
	}

	if event.Environment == "" {
		event.Environment = client.options.Environment
	}

	event.Platform = "go"
	event.Sdk = SdkInfo{
		Name:         client.GetSDKIdentifier(),
		Version:      SDKVersion,
		Integrations: client.listIntegrations(),
		Packages: []SdkPackage{{
			Name:    "sentry-go",
			Version: SDKVersion,
		}},
	}

	if scope != nil {
		event = scope.ApplyToEvent(event, hint, client)
		if event == nil {
			return nil
		}
	}

	for _, processor := range client.eventProcessors {
		id := event.EventID
		event = processor(event, hint)
		if event == nil {
			debuglog.Printf("Event dropped by one of the Client EventProcessors: %s\n", id)
			return nil
		}
	}

	for _, processor := range globalEventProcessors {
		id := event.EventID
		event = processor(event, hint)
		if event == nil {
			debuglog.Printf("Event dropped by one of the Global EventProcessors: %s\n", id)
			return nil
		}
	}

	return event
}

func (client *Client) listIntegrations() []string {
	integrations := make([]string, len(client.integrations))
	for i, integration := range client.integrations {
		integrations[i] = integration.Name()
	}
	return integrations
}

func (client *Client) integrationAlreadyInstalled(name string) bool {
	for _, integration := range client.integrations {
		if integration.Name() == name {
			return true
		}
	}
	return false
}

// sample returns true with the given probability, which must be in the range
// [0.0, 1.0].
func sample(probability float64) bool {
	return rng.Float64() < probability
}
