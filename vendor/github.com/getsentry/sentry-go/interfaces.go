package sentry

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/getsentry/sentry-go/attribute"
	"github.com/getsentry/sentry-go/internal/debuglog"
	"github.com/getsentry/sentry-go/internal/protocol"
	"github.com/getsentry/sentry-go/internal/ratelimit"
)

const errorType = ""
const eventType = "event"
const transactionType = "transaction"
const checkInType = "check_in"

var logEvent = struct {
	Type        string
	ContentType string
}{
	"log",
	"application/vnd.sentry.items.log+json",
}

var traceMetricEvent = struct {
	Type        string
	ContentType string
}{
	"trace_metric",
	"application/vnd.sentry.items.trace-metric+json",
}

// Level marks the severity of the event.
type Level string

// Describes the severity of the event.
const (
	LevelDebug   Level = "debug"
	LevelInfo    Level = "info"
	LevelWarning Level = "warning"
	LevelError   Level = "error"
	LevelFatal   Level = "fatal"
)

// SdkInfo contains all metadata about the SDK.
type SdkInfo = protocol.SdkInfo
type SdkPackage = protocol.SdkPackage

// TODO: This type could be more useful, as map of interface{} is too generic
// and requires a lot of type assertions in beforeBreadcrumb calls
// plus it could just be map[string]interface{} then.

// BreadcrumbHint contains information that can be associated with a Breadcrumb.
type BreadcrumbHint map[string]interface{}

// Breadcrumb specifies an application event that occurred before a Sentry event.
// An event may contain one or more breadcrumbs.
type Breadcrumb struct {
	Type      string                 `json:"type,omitempty"`
	Category  string                 `json:"category,omitempty"`
	Message   string                 `json:"message,omitempty"`
	Data      map[string]interface{} `json:"data,omitempty"`
	Level     Level                  `json:"level,omitempty"`
	Timestamp time.Time              `json:"timestamp,omitzero"`
}

// TODO: provide constants for known breadcrumb types.
// See https://develop.sentry.dev/sdk/event-payloads/breadcrumbs/#breadcrumb-types.

// Logger provides a chaining API for structured logging to Sentry.
type Logger interface {
	// Write implements the io.Writer interface. Currently, the [sentry.Hub] is
	// context aware, in order to get the correct trace correlation. Using this
	// might result in incorrect span association on logs. If you need to use
	// Write it is recommended to create a NewLogger so that the associated context
	// is passed correctly.
	Write(p []byte) (n int, err error)

	// SetAttributes allows attaching parameters to the logger using the attribute API.
	// These attributes will be included in all subsequent log entries.
	SetAttributes(...attribute.Builder)

	// Trace defines the [sentry.LogLevel] for the log entry.
	Trace() LogEntry
	// Debug defines the [sentry.LogLevel] for the log entry.
	Debug() LogEntry
	// Info defines the [sentry.LogLevel] for the log entry.
	Info() LogEntry
	// Warn defines the [sentry.LogLevel] for the log entry.
	Warn() LogEntry
	// Error defines the [sentry.LogLevel] for the log entry.
	Error() LogEntry
	// Fatal defines the [sentry.LogLevel] for the log entry.
	Fatal() LogEntry
	// Panic defines the [sentry.LogLevel] for the log entry.
	Panic() LogEntry
	// LFatal defines the [sentry.LogLevel] for the log entry. This only sets
	// the level to fatal, but does not panic or exit.
	LFatal() LogEntry
	// GetCtx returns the [context.Context] set on the logger.
	GetCtx() context.Context
}

// LogEntry defines the interface for a log entry that supports chaining attributes.
type LogEntry interface {
	// WithCtx creates a new LogEntry with the specified context without overwriting the previous one.
	WithCtx(ctx context.Context) LogEntry
	// String adds a string attribute to the LogEntry.
	String(key, value string) LogEntry
	// Int adds an int attribute to the LogEntry.
	Int(key string, value int) LogEntry
	// Int64 adds an int64 attribute to the LogEntry.
	Int64(key string, value int64) LogEntry
	// Float64 adds a float64 attribute to the LogEntry.
	Float64(key string, value float64) LogEntry
	// Bool adds a bool attribute to the LogEntry.
	Bool(key string, value bool) LogEntry
	// Emit emits the LogEntry with the provided arguments.
	Emit(args ...interface{})
	// Emitf emits the LogEntry using a format string and arguments.
	Emitf(format string, args ...interface{})
}

// Meter provides an interface for recording metrics.
type Meter interface {
	// WithCtx returns a new Meter that uses the given context for trace/span association.
	WithCtx(ctx context.Context) Meter
	// SetAttributes allows attaching parameters to the meter using the attribute API.
	// These attributes will be included in all subsequent metrics.
	SetAttributes(attrs ...attribute.Builder)
	// Count records a count metric.
	Count(name string, count int64, opts ...MeterOption)
	// Gauge records a gauge metric.
	Gauge(name string, value float64, opts ...MeterOption)
	// Distribution records a distribution metric.
	Distribution(name string, sample float64, opts ...MeterOption)
}

// MeterOption configures a metric recording call.
type MeterOption func(*meterOptions)

type meterOptions struct {
	unit       string
	scope      *Scope
	attributes map[string]attribute.Value
}

// WithUnit sets the unit for the metric (e.g., "millisecond", "byte").
func WithUnit(unit string) MeterOption {
	return func(o *meterOptions) {
		o.unit = unit
	}
}

// WithScopeOverride sets a custom scope for the metric, overriding the default scope from the hub.
func WithScopeOverride(scope *Scope) MeterOption {
	return func(o *meterOptions) {
		o.scope = scope
	}
}

// WithAttributes sets attributes for the metric.
func WithAttributes(attrs ...attribute.Builder) MeterOption {
	return func(o *meterOptions) {
		if o.attributes == nil {
			o.attributes = make(map[string]attribute.Value, len(attrs))
		}
		for _, a := range attrs {
			if a.Value.Type() == attribute.INVALID {
				debuglog.Printf("invalid attribute: %v", a)
				continue
			}
			o.attributes[a.Key] = a.Value
		}
	}
}

// Attachment allows associating files with your events to aid in investigation.
// An event may contain one or more attachments.
type Attachment struct {
	Filename    string
	ContentType string
	Payload     []byte
}

// User describes the user associated with an Event. If this is used, at least
// an ID or an IP address should be provided.
type User struct {
	ID        string            `json:"id,omitempty"`
	Email     string            `json:"email,omitempty"`
	IPAddress string            `json:"ip_address,omitempty"`
	Username  string            `json:"username,omitempty"`
	Name      string            `json:"name,omitempty"`
	Data      map[string]string `json:"data,omitempty"`
}

func (u User) IsEmpty() bool {
	if u.ID != "" {
		return false
	}

	if u.Email != "" {
		return false
	}

	if u.IPAddress != "" {
		return false
	}

	if u.Username != "" {
		return false
	}

	if u.Name != "" {
		return false
	}

	if len(u.Data) > 0 {
		return false
	}

	return true
}

// Request contains information on a HTTP request related to the event.
type Request struct {
	URL         string            `json:"url,omitempty"`
	Method      string            `json:"method,omitempty"`
	Data        string            `json:"data,omitempty"`
	QueryString string            `json:"query_string,omitempty"`
	Cookies     string            `json:"cookies,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
}

var sensitiveHeaders = map[string]struct{}{
	"_csrf":               {},
	"_csrf_token":         {},
	"_session":            {},
	"_xsrf":               {},
	"Api-Key":             {},
	"Apikey":              {},
	"Auth":                {},
	"Authorization":       {},
	"Cookie":              {},
	"Credentials":         {},
	"Csrf":                {},
	"Csrf-Token":          {},
	"Csrftoken":           {},
	"Ip-Address":          {},
	"Passwd":              {},
	"Password":            {},
	"Private-Key":         {},
	"Privatekey":          {},
	"Proxy-Authorization": {},
	"Remote-Addr":         {},
	"Secret":              {},
	"Session":             {},
	"Sessionid":           {},
	"Token":               {},
	"User-Session":        {},
	"X-Api-Key":           {},
	"X-Csrftoken":         {},
	"X-Forwarded-For":     {},
	"X-Real-Ip":           {},
	"XSRF-TOKEN":          {},
}

// NewRequest returns a new Sentry Request from the given http.Request.
//
// NewRequest avoids operations that depend on network access. In particular, it
// does not read r.Body.
func NewRequest(r *http.Request) *Request {
	prot := protocol.SchemeHTTP
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		prot = protocol.SchemeHTTPS
	}
	url := fmt.Sprintf("%s://%s%s", prot, r.Host, r.URL.Path)

	var cookies string
	var env map[string]string
	headers := map[string]string{}

	if client := CurrentHub().Client(); client != nil && client.options.SendDefaultPII {
		// We read only the first Cookie header because of the specification:
		// https://tools.ietf.org/html/rfc6265#section-5.4
		// When the user agent generates an HTTP request, the user agent MUST NOT
		// attach more than one Cookie header field.
		cookies = r.Header.Get("Cookie")

		headers = make(map[string]string, len(r.Header))
		for k, v := range r.Header {
			headers[k] = strings.Join(v, ",")
		}

		if addr, port, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			env = map[string]string{"REMOTE_ADDR": addr, "REMOTE_PORT": port}
		}
	} else {
		for k, v := range r.Header {
			if _, ok := sensitiveHeaders[k]; !ok {
				headers[k] = strings.Join(v, ",")
			}
		}
	}

	headers["Host"] = r.Host

	return &Request{
		URL:         url,
		Method:      r.Method,
		QueryString: r.URL.RawQuery,
		Cookies:     cookies,
		Headers:     headers,
		Env:         env,
	}
}

// Mechanism is the mechanism by which an exception was generated and handled.
type Mechanism struct {
	Type             string         `json:"type"`
	Description      string         `json:"description,omitempty"`
	HelpLink         string         `json:"help_link,omitempty"`
	Source           string         `json:"source,omitempty"`
	Handled          *bool          `json:"handled,omitempty"`
	ParentID         *int           `json:"parent_id,omitempty"`
	ExceptionID      int            `json:"exception_id"`
	IsExceptionGroup bool           `json:"is_exception_group,omitempty"`
	Data             map[string]any `json:"data,omitempty"`
}

// SetUnhandled indicates that the exception is an unhandled exception, i.e.
// from a panic.
func (m *Mechanism) SetUnhandled() {
	m.Handled = Pointer(false)
}

// Exception specifies an error that occurred.
type Exception struct {
	Type       string      `json:"type,omitempty"`  // used as the main issue title
	Value      string      `json:"value,omitempty"` // used as the main issue subtitle
	Module     string      `json:"module,omitempty"`
	ThreadID   uint64      `json:"thread_id,omitempty"`
	Stacktrace *Stacktrace `json:"stacktrace,omitempty"`
	Mechanism  *Mechanism  `json:"mechanism,omitempty"`
}

// SDKMetaData is a struct to stash data which is needed at some point in the SDK's event processing pipeline
// but which shouldn't get send to Sentry.
type SDKMetaData struct {
	dsc DynamicSamplingContext
}

// Contains information about how the name of the transaction was determined.
type TransactionInfo struct {
	Source TransactionSource `json:"source,omitempty"`
}

// The DebugMeta interface is not used in Golang apps, but may be populated
// when proxying Events from other platforms, like iOS, Android, and the
// Web.  (See: https://develop.sentry.dev/sdk/event-payloads/debugmeta/ ).
type DebugMeta struct {
	SdkInfo *DebugMetaSdkInfo `json:"sdk_info,omitempty"`
	Images  []DebugMetaImage  `json:"images,omitempty"`
}

type DebugMetaSdkInfo struct {
	SdkName           string `json:"sdk_name,omitempty"`
	VersionMajor      int    `json:"version_major,omitempty"`
	VersionMinor      int    `json:"version_minor,omitempty"`
	VersionPatchlevel int    `json:"version_patchlevel,omitempty"`
}

type DebugMetaImage struct {
	Type        string `json:"type,omitempty"`         // all
	ImageAddr   string `json:"image_addr,omitempty"`   // macho,elf,pe
	ImageSize   int    `json:"image_size,omitempty"`   // macho,elf,pe
	DebugID     string `json:"debug_id,omitempty"`     // macho,elf,pe,wasm,sourcemap
	DebugFile   string `json:"debug_file,omitempty"`   // macho,elf,pe,wasm
	CodeID      string `json:"code_id,omitempty"`      // macho,elf,pe,wasm
	CodeFile    string `json:"code_file,omitempty"`    // macho,elf,pe,wasm,sourcemap
	ImageVmaddr string `json:"image_vmaddr,omitempty"` // macho,elf,pe
	Arch        string `json:"arch,omitempty"`         // macho,elf,pe
	UUID        string `json:"uuid,omitempty"`         // proguard
}

// EventID is a hexadecimal string representing a unique uuid4 for an Event.
// An EventID must be 32 characters long, lowercase and not have any dashes.
type EventID string

type Context = map[string]interface{}

// Event is the fundamental data structure that is sent to Sentry.
type Event struct {
	Breadcrumbs []*Breadcrumb          `json:"breadcrumbs,omitempty"`
	Contexts    map[string]Context     `json:"contexts,omitempty"`
	Dist        string                 `json:"dist,omitempty"`
	Environment string                 `json:"environment,omitempty"`
	EventID     EventID                `json:"event_id,omitempty"`
	Extra       map[string]interface{} `json:"extra,omitempty"`
	Fingerprint []string               `json:"fingerprint,omitempty"`
	Level       Level                  `json:"level,omitempty"`
	Message     string                 `json:"message,omitempty"`
	Platform    string                 `json:"platform,omitempty"`
	Release     string                 `json:"release,omitempty"`
	Sdk         SdkInfo                `json:"sdk,omitempty"`
	ServerName  string                 `json:"server_name,omitempty"`
	Threads     []Thread               `json:"threads,omitempty"`
	Tags        map[string]string      `json:"tags,omitempty"`
	Timestamp   time.Time              `json:"timestamp,omitzero"`
	Transaction string                 `json:"transaction,omitempty"`
	User        User                   `json:"user,omitempty"`
	Logger      string                 `json:"logger,omitempty"`
	Modules     map[string]string      `json:"modules,omitempty"`
	Request     *Request               `json:"request,omitempty"`
	Exception   []Exception            `json:"exception,omitempty"`
	DebugMeta   *DebugMeta             `json:"debug_meta,omitempty"`
	Attachments []*Attachment          `json:"-"`

	// The fields below are only relevant for transactions.

	Type            string           `json:"type,omitempty"`
	StartTime       time.Time        `json:"start_timestamp,omitzero"`
	Spans           []*Span          `json:"spans,omitempty"`
	TransactionInfo *TransactionInfo `json:"transaction_info,omitempty"`

	// The fields below are only relevant for crons/check ins

	CheckIn       *CheckIn       `json:"check_in,omitempty"`
	MonitorConfig *MonitorConfig `json:"monitor_config,omitempty"`

	// The fields below are only relevant for logs
	Logs []Log `json:"-"`

	// The fields below are only relevant for metrics
	Metrics []Metric `json:"-"`

	// The fields below are not part of the final JSON payload.

	sdkMetaData SDKMetaData
}

// SetException appends the unwrapped errors to the event's exception list.
//
// maxErrorDepth is the maximum depth of the error chain we will look
// into while unwrapping the errors. If maxErrorDepth is -1, we will
// unwrap all errors in the chain.
func (e *Event) SetException(exception error, maxErrorDepth int) {
	if exception == nil {
		return
	}

	exceptions := convertErrorToExceptions(exception, maxErrorDepth)
	if len(exceptions) == 0 {
		return
	}

	e.Exception = exceptions
}

// ToEnvelopeItem converts the Event to a Sentry envelope item.
func (e *Event) ToEnvelopeItem() (*protocol.EnvelopeItem, error) {
	eventBody, err := json.Marshal(e)
	if err != nil {
		// Try fallback: remove problematic fields and retry
		e.Breadcrumbs = nil
		e.Contexts = nil
		e.Extra = map[string]interface{}{
			"info": fmt.Sprintf("Could not encode original event as JSON. "+
				"Succeeded by removing Breadcrumbs, Contexts and Extra. "+
				"Please verify the data you attach to the scope. "+
				"Error: %s", err),
		}

		eventBody, err = json.Marshal(e)
		if err != nil {
			return nil, fmt.Errorf("event could not be marshaled even with fallback: %w", err)
		}

		DebugLogger.Printf("Event marshaling succeeded with fallback after removing problematic fields")
	}

	// TODO: all event types should be abstracted to implement EnvelopeItemConvertible and convert themselves.
	var item *protocol.EnvelopeItem
	switch e.Type {
	case transactionType:
		item = protocol.NewEnvelopeItem(protocol.EnvelopeItemTypeTransaction, eventBody)
	case checkInType:
		item = protocol.NewEnvelopeItem(protocol.EnvelopeItemTypeCheckIn, eventBody)
	case logEvent.Type:
		item = protocol.NewLogItem(len(e.Logs), eventBody)
	case traceMetricEvent.Type:
		item = protocol.NewTraceMetricItem(len(e.Metrics), eventBody)
	default:
		item = protocol.NewEnvelopeItem(protocol.EnvelopeItemTypeEvent, eventBody)
	}

	return item, nil
}

// GetCategory returns the rate limit category for this event.
func (e *Event) GetCategory() ratelimit.Category {
	return e.toCategory()
}

// GetEventID returns the event ID.
func (e *Event) GetEventID() string {
	return string(e.EventID)
}

// GetSdkInfo returns SDK information for the envelope header.
func (e *Event) GetSdkInfo() *protocol.SdkInfo {
	return &e.Sdk
}

// GetDynamicSamplingContext returns trace context for the envelope header.
func (e *Event) GetDynamicSamplingContext() map[string]string {
	trace := make(map[string]string)
	if dsc := e.sdkMetaData.dsc; dsc.HasEntries() {
		for k, v := range dsc.Entries {
			trace[k] = v
		}
	}
	return trace
}

// TODO: Event.Contexts map[string]interface{} => map[string]EventContext,
// to prevent accidentally storing T when we mean *T.
// For example, the TraceContext must be stored as *TraceContext to pick up the
// MarshalJSON method (and avoid copying).
// type EventContext interface{ EventContext() }

// MarshalJSON converts the Event struct to JSON.
func (e *Event) MarshalJSON() ([]byte, error) {
	if e.Type == checkInType {
		return e.checkInMarshalJSON()
	}
	return e.defaultMarshalJSON()
}

func (e *Event) defaultMarshalJSON() ([]byte, error) {
	// event aliases Event to allow calling json.Marshal without an infinite
	// loop. It preserves all fields while none of the attached methods.
	type event Event

	if e.Type == transactionType {
		return json.Marshal(struct{ *event }{(*event)(e)})
	}
	// metrics and logs should be serialized under the same `items` json field.
	if e.Type == logEvent.Type {
		type logEvent struct {
			*event
			Items []Log           `json:"items,omitempty"`
			Type  json.RawMessage `json:"type,omitempty"`
		}
		return json.Marshal(logEvent{event: (*event)(e), Items: e.Logs})
	}

	if e.Type == traceMetricEvent.Type {
		type metricEvent struct {
			*event
			Items []Metric        `json:"items,omitempty"`
			Type  json.RawMessage `json:"type,omitempty"`
		}
		return json.Marshal(metricEvent{event: (*event)(e), Items: e.Metrics})
	}

	// errorEvent is like Event with shadowed fields for customizing JSON
	// marshaling.
	type errorEvent struct {
		*event

		// The fields below are not part of error events and only make sense to
		// be sent for transactions. They shadow the respective fields in Event
		// and are meant to remain nil, triggering the omitempty behavior.

		Type            json.RawMessage `json:"type,omitempty"`
		StartTime       json.RawMessage `json:"start_timestamp,omitempty"`
		Spans           json.RawMessage `json:"spans,omitempty"`
		TransactionInfo json.RawMessage `json:"transaction_info,omitempty"`
	}

	x := errorEvent{event: (*event)(e)}
	return json.Marshal(x)
}

func (e *Event) checkInMarshalJSON() ([]byte, error) {
	checkIn := serializedCheckIn{
		CheckInID:     string(e.CheckIn.ID),
		MonitorSlug:   e.CheckIn.MonitorSlug,
		Status:        e.CheckIn.Status,
		Duration:      e.CheckIn.Duration.Seconds(),
		Release:       e.Release,
		Environment:   e.Environment,
		MonitorConfig: nil,
	}

	if e.MonitorConfig != nil {
		checkIn.MonitorConfig = &MonitorConfig{
			Schedule:              e.MonitorConfig.Schedule,
			CheckInMargin:         e.MonitorConfig.CheckInMargin,
			MaxRuntime:            e.MonitorConfig.MaxRuntime,
			Timezone:              e.MonitorConfig.Timezone,
			FailureIssueThreshold: e.MonitorConfig.FailureIssueThreshold,
			RecoveryThreshold:     e.MonitorConfig.RecoveryThreshold,
		}
	}

	return json.Marshal(checkIn)
}

func (e *Event) toCategory() ratelimit.Category {
	switch e.Type {
	case errorType:
		return ratelimit.CategoryError
	case transactionType:
		return ratelimit.CategoryTransaction
	case logEvent.Type:
		return ratelimit.CategoryLog
	case checkInType:
		return ratelimit.CategoryMonitor
	case traceMetricEvent.Type:
		return ratelimit.CategoryTraceMetric
	default:
		return ratelimit.CategoryUnknown
	}
}

// NewEvent creates a new Event.
func NewEvent() *Event {
	return &Event{
		Contexts: make(map[string]Context),
		Extra:    make(map[string]interface{}),
		Tags:     make(map[string]string),
		Modules:  make(map[string]string),
	}
}

// Thread specifies threads that were running at the time of an event.
type Thread struct {
	ID         string      `json:"id,omitempty"`
	Name       string      `json:"name,omitempty"`
	Stacktrace *Stacktrace `json:"stacktrace,omitempty"`
	Crashed    bool        `json:"crashed,omitempty"`
	Current    bool        `json:"current,omitempty"`
}

// EventHint contains information that can be associated with an Event.
type EventHint struct {
	Data               interface{}
	EventID            string
	OriginalException  error
	RecoveredException interface{}
	Context            context.Context
	Request            *http.Request
	Response           *http.Response
}

type Log struct {
	Timestamp  time.Time                  `json:"timestamp,omitzero"`
	TraceID    TraceID                    `json:"trace_id"`
	SpanID     SpanID                     `json:"span_id,omitzero"`
	Level      LogLevel                   `json:"level"`
	Severity   int                        `json:"severity_number,omitempty"`
	Body       string                     `json:"body"`
	Attributes map[string]attribute.Value `json:"attributes,omitempty"`
}

// GetCategory returns the rate limit category for logs.
func (l *Log) GetCategory() ratelimit.Category {
	return ratelimit.CategoryLog
}

// GetEventID returns empty string (event ID set when batching).
func (l *Log) GetEventID() string {
	return ""
}

// GetSdkInfo returns nil (SDK info set when batching).
func (l *Log) GetSdkInfo() *protocol.SdkInfo {
	return nil
}

// GetDynamicSamplingContext returns nil (trace context set when batching).
func (l *Log) GetDynamicSamplingContext() map[string]string {
	return nil
}

type MetricType string

const (
	MetricTypeInvalid      MetricType = ""
	MetricTypeCounter      MetricType = "counter"
	MetricTypeGauge        MetricType = "gauge"
	MetricTypeDistribution MetricType = "distribution"
)

type Metric struct {
	Timestamp  time.Time                  `json:"timestamp,omitzero"`
	TraceID    TraceID                    `json:"trace_id"`
	SpanID     SpanID                     `json:"span_id,omitzero"`
	Type       MetricType                 `json:"type"`
	Name       string                     `json:"name"`
	Value      MetricValue                `json:"value"`
	Unit       string                     `json:"unit,omitempty"`
	Attributes map[string]attribute.Value `json:"attributes,omitempty"`
}

// GetCategory returns the rate limit category for metrics.
func (m *Metric) GetCategory() ratelimit.Category {
	return ratelimit.CategoryTraceMetric
}

// GetEventID returns empty string (event ID set when batching).
func (m *Metric) GetEventID() string {
	return ""
}

// GetSdkInfo returns nil (SDK info set when batching).
func (m *Metric) GetSdkInfo() *protocol.SdkInfo {
	return nil
}

// GetDynamicSamplingContext returns nil (trace context set when batching).
func (m *Metric) GetDynamicSamplingContext() map[string]string {
	return nil
}

// MetricValue stores metric values with full precision.
// It supports int64 (for counters) and float64 (for gauges and distributions).
type MetricValue struct {
	value attribute.Value
}

// Int64MetricValue creates a MetricValue from an int64.
// Used for counter metrics to preserve full int64 precision.
func Int64MetricValue(v int64) MetricValue {
	return MetricValue{value: attribute.Int64Value(v)}
}

// Float64MetricValue creates a MetricValue from a float64.
// Used for gauge and distribution metrics.
func Float64MetricValue(v float64) MetricValue {
	return MetricValue{value: attribute.Float64Value(v)}
}

// Type returns the type of the stored value (attribute.INT64 or attribute.FLOAT64).
func (v MetricValue) Type() attribute.Type {
	return v.value.Type()
}

// Int64 returns the value as int64 if it holds an int64.
// The second return value indicates whether the type matched.
func (v MetricValue) Int64() (int64, bool) {
	if v.value.Type() == attribute.INT64 {
		return v.value.AsInt64(), true
	}
	return 0, false
}

// Float64 returns the value as float64 if it holds a float64.
// The second return value indicates whether the type matched.
func (v MetricValue) Float64() (float64, bool) {
	if v.value.Type() == attribute.FLOAT64 {
		return v.value.AsFloat64(), true
	}
	return 0, false
}

// AsInterface returns the value as int64 or float64.
// Use type assertion or type switch to handle the result.
func (v MetricValue) AsInterface() any {
	return v.value.AsInterface()
}

// MarshalJSON serializes the value as a bare number.
func (v MetricValue) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.value.AsInterface())
}
