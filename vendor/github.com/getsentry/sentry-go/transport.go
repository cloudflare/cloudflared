package sentry

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/getsentry/sentry-go/internal/debuglog"
	httpinternal "github.com/getsentry/sentry-go/internal/http"
	"github.com/getsentry/sentry-go/internal/protocol"
	"github.com/getsentry/sentry-go/internal/ratelimit"
	"github.com/getsentry/sentry-go/internal/util"
)

const (
	defaultBufferSize = 1000
	defaultTimeout    = time.Second * 30
)

// Transport is used by the Client to deliver events to remote server.
type Transport interface {
	Flush(timeout time.Duration) bool
	FlushWithContext(ctx context.Context) bool
	Configure(options ClientOptions)
	SendEvent(event *Event)
	Close()
}

func getProxyConfig(options ClientOptions) func(*http.Request) (*url.URL, error) {
	if options.HTTPSProxy != "" {
		return func(*http.Request) (*url.URL, error) {
			return url.Parse(options.HTTPSProxy)
		}
	}

	if options.HTTPProxy != "" {
		return func(*http.Request) (*url.URL, error) {
			return url.Parse(options.HTTPProxy)
		}
	}

	return http.ProxyFromEnvironment
}

func getTLSConfig(options ClientOptions) *tls.Config {
	if options.CaCerts != nil {
		// #nosec G402 -- We should be using `MinVersion: tls.VersionTLS12`,
		// 				 but we don't want to break peoples code without the major bump.
		return &tls.Config{
			RootCAs: options.CaCerts,
		}
	}

	return nil
}

func getRequestBodyFromEvent(event *Event) []byte {
	body, err := json.Marshal(event)
	if err == nil {
		return body
	}

	msg := fmt.Sprintf("Could not encode original event as JSON. "+
		"Succeeded by removing Breadcrumbs, Contexts and Extra. "+
		"Please verify the data you attach to the scope. "+
		"Error: %s", err)
	// Try to serialize the event, with all the contextual data that allows for interface{} stripped.
	event.Breadcrumbs = nil
	event.Contexts = nil
	event.Extra = map[string]interface{}{
		"info": msg,
	}
	body, err = json.Marshal(event)
	if err == nil {
		debuglog.Println(msg)
		return body
	}

	// This should _only_ happen when Event.Exception[0].Stacktrace.Frames[0].Vars is unserializable
	// Which won't ever happen, as we don't use it now (although it's the part of public interface accepted by Sentry)
	// Juuust in case something, somehow goes utterly wrong.
	debuglog.Println("Event couldn't be marshaled, even with stripped contextual data. Skipping delivery. " +
		"Please notify the SDK owners with possibly broken payload.")
	return nil
}

func encodeAttachment(enc *json.Encoder, b io.Writer, attachment *Attachment) error {
	// Attachment header
	err := enc.Encode(struct {
		Type        string `json:"type"`
		Length      int    `json:"length"`
		Filename    string `json:"filename"`
		ContentType string `json:"content_type,omitempty"`
	}{
		Type:        "attachment",
		Length:      len(attachment.Payload),
		Filename:    attachment.Filename,
		ContentType: attachment.ContentType,
	})
	if err != nil {
		return err
	}

	// Attachment payload
	if _, err = b.Write(attachment.Payload); err != nil {
		return err
	}

	// "Envelopes should be terminated with a trailing newline."
	//
	// [1]: https://develop.sentry.dev/sdk/envelopes/#envelopes
	if _, err := b.Write([]byte("\n")); err != nil {
		return err
	}

	return nil
}

func encodeEnvelopeItem(enc *json.Encoder, itemType string, body json.RawMessage) error {
	// Item header
	err := enc.Encode(struct {
		Type   string `json:"type"`
		Length int    `json:"length"`
	}{
		Type:   itemType,
		Length: len(body),
	})
	if err == nil {
		// payload
		err = enc.Encode(body)
	}
	return err
}

func encodeEnvelopeLogs(enc *json.Encoder, count int, body json.RawMessage) error {
	err := enc.Encode(
		struct {
			Type        string `json:"type"`
			ItemCount   int    `json:"item_count"`
			ContentType string `json:"content_type"`
		}{
			Type:        logEvent.Type,
			ItemCount:   count,
			ContentType: logEvent.ContentType,
		})
	if err == nil {
		err = enc.Encode(body)
	}
	return err
}

func encodeEnvelopeMetrics(enc *json.Encoder, count int, body json.RawMessage) error {
	err := enc.Encode(
		struct {
			Type        string `json:"type"`
			ItemCount   int    `json:"item_count"`
			ContentType string `json:"content_type"`
		}{
			Type:        traceMetricEvent.Type,
			ItemCount:   count,
			ContentType: traceMetricEvent.ContentType,
		})
	if err == nil {
		err = enc.Encode(body)
	}
	return err
}

func envelopeFromBody(event *Event, dsn *Dsn, sentAt time.Time, body json.RawMessage) (*bytes.Buffer, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)

	// Construct the trace envelope header
	var trace = map[string]string{}
	if dsc := event.sdkMetaData.dsc; dsc.HasEntries() {
		for k, v := range dsc.Entries {
			trace[k] = v
		}
	}

	// Envelope header
	err := enc.Encode(struct {
		EventID EventID           `json:"event_id"`
		SentAt  time.Time         `json:"sent_at"`
		Dsn     string            `json:"dsn"`
		Sdk     map[string]string `json:"sdk"`
		Trace   map[string]string `json:"trace,omitempty"`
	}{
		EventID: event.EventID,
		SentAt:  sentAt,
		Trace:   trace,
		Dsn:     dsn.String(),
		Sdk: map[string]string{
			"name":    event.Sdk.Name,
			"version": event.Sdk.Version,
		},
	})
	if err != nil {
		return nil, err
	}

	switch event.Type {
	case transactionType, checkInType:
		err = encodeEnvelopeItem(enc, event.Type, body)
	case logEvent.Type:
		err = encodeEnvelopeLogs(enc, len(event.Logs), body)
	case traceMetricEvent.Type:
		err = encodeEnvelopeMetrics(enc, len(event.Metrics), body)
	default:
		err = encodeEnvelopeItem(enc, eventType, body)
	}

	if err != nil {
		return nil, err
	}

	// Attachments
	for _, attachment := range event.Attachments {
		if err := encodeAttachment(enc, &b, attachment); err != nil {
			return nil, err
		}
	}

	return &b, nil
}

func getRequestFromEvent(ctx context.Context, event *Event, dsn *Dsn) (r *http.Request, err error) {
	defer func() {
		if r != nil {
			r.Header.Set("User-Agent", fmt.Sprintf("%s/%s", event.Sdk.Name, event.Sdk.Version))
			r.Header.Set("Content-Type", "application/x-sentry-envelope")

			auth := fmt.Sprintf("Sentry sentry_version=%s, "+
				"sentry_client=%s/%s, sentry_key=%s", apiVersion, event.Sdk.Name, event.Sdk.Version, dsn.GetPublicKey())

			// The key sentry_secret is effectively deprecated and no longer needs to be set.
			// However, since it was required in older self-hosted versions,
			// it should still passed through to Sentry if set.
			if dsn.GetSecretKey() != "" {
				auth = fmt.Sprintf("%s, sentry_secret=%s", auth, dsn.GetSecretKey())
			}

			r.Header.Set("X-Sentry-Auth", auth)
		}
	}()

	body := getRequestBodyFromEvent(event)
	if body == nil {
		return nil, errors.New("event could not be marshaled")
	}

	envelope, err := envelopeFromBody(event, dsn, time.Now(), body)
	if err != nil {
		return nil, err
	}

	if ctx == nil {
		ctx = context.Background()
	}

	return http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		dsn.GetAPIURL().String(),
		envelope,
	)
}

// ================================
// HTTPTransport
// ================================

// A batch groups items that are processed sequentially.
type batch struct {
	items   chan batchItem
	started chan struct{} // closed to signal items started to be worked on
	done    chan struct{} // closed to signal completion of all items
}

type batchItem struct {
	request         *http.Request
	category        ratelimit.Category
	eventIdentifier string
}

// HTTPTransport is the default, non-blocking, implementation of Transport.
//
// Clients using this transport will enqueue requests in a buffer and return to
// the caller before any network communication has happened. Requests are sent
// to Sentry sequentially from a background goroutine.
type HTTPTransport struct {
	dsn       *Dsn
	client    *http.Client
	transport http.RoundTripper

	// buffer is a channel of batches. Calling Flush terminates work on the
	// current in-flight items and starts a new batch for subsequent events.
	buffer chan batch

	startOnce sync.Once
	closeOnce sync.Once

	// Size of the transport buffer. Defaults to 30.
	BufferSize int
	// HTTP Client request timeout. Defaults to 30 seconds.
	Timeout time.Duration

	mu     sync.RWMutex
	limits ratelimit.Map

	// receiving signal will terminate worker.
	done chan struct{}
}

// NewHTTPTransport returns a new pre-configured instance of HTTPTransport.
func NewHTTPTransport() *HTTPTransport {
	transport := HTTPTransport{
		BufferSize: defaultBufferSize,
		Timeout:    defaultTimeout,
		done:       make(chan struct{}),
	}
	return &transport
}

// Configure is called by the Client itself, providing it it's own ClientOptions.
func (t *HTTPTransport) Configure(options ClientOptions) {
	dsn, err := NewDsn(options.Dsn)
	if err != nil {
		debuglog.Printf("%v\n", err)
		return
	}
	t.dsn = dsn

	// A buffered channel with capacity 1 works like a mutex, ensuring only one
	// goroutine can access the current batch at a given time. Access is
	// synchronized by reading from and writing to the channel.
	t.buffer = make(chan batch, 1)
	t.buffer <- batch{
		items:   make(chan batchItem, t.BufferSize),
		started: make(chan struct{}),
		done:    make(chan struct{}),
	}

	if options.HTTPTransport != nil {
		t.transport = options.HTTPTransport
	} else {
		t.transport = &http.Transport{
			Proxy:           getProxyConfig(options),
			TLSClientConfig: getTLSConfig(options),
		}
	}

	if options.HTTPClient != nil {
		t.client = options.HTTPClient
	} else {
		t.client = &http.Client{
			Transport: t.transport,
			Timeout:   t.Timeout,
		}
	}

	t.startOnce.Do(func() {
		go t.worker()
	})
}

// SendEvent assembles a new packet out of Event and sends it to the remote server.
func (t *HTTPTransport) SendEvent(event *Event) {
	t.SendEventWithContext(context.Background(), event)
}

// SendEventWithContext assembles a new packet out of Event and sends it to the remote server.
func (t *HTTPTransport) SendEventWithContext(ctx context.Context, event *Event) {
	if t.dsn == nil {
		return
	}

	category := event.toCategory()

	if t.disabled(category) {
		return
	}

	request, err := getRequestFromEvent(ctx, event, t.dsn)
	if err != nil {
		return
	}

	// <-t.buffer is equivalent to acquiring a lock to access the current batch.
	// A few lines below, t.buffer <- b releases the lock.
	//
	// The lock must be held during the select block below to guarantee that
	// b.items is not closed while trying to send to it. Remember that sending
	// on a closed channel panics.
	//
	// Note that the select block takes a bounded amount of CPU time because of
	// the default case that is executed if sending on b.items would block. That
	// is, the event is dropped if it cannot be sent immediately to the b.items
	// channel (used as a queue).
	b := <-t.buffer

	identifier := eventIdentifier(event)

	select {
	case b.items <- batchItem{
		request:         request,
		category:        category,
		eventIdentifier: identifier,
	}:
		debuglog.Printf(
			"Sending %s to %s project: %s",
			identifier,
			t.dsn.GetHost(),
			t.dsn.GetProjectID(),
		)
	default:
		debuglog.Println("Event dropped due to transport buffer being full.")
	}

	t.buffer <- b
}

// Flush waits until any buffered events are sent to the Sentry server, blocking
// for at most the given timeout. It returns false if the timeout was reached.
// In that case, some events may not have been sent.
//
// Flush should be called before terminating the program to avoid
// unintentionally dropping events.
//
// Do not call Flush indiscriminately after every call to SendEvent. Instead, to
// have the SDK send events over the network synchronously, configure it to use
// the HTTPSyncTransport in the call to Init.
func (t *HTTPTransport) Flush(timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return t.FlushWithContext(ctx)
}

// FlushWithContext works like Flush, but it accepts a context.Context instead of a timeout.
func (t *HTTPTransport) FlushWithContext(ctx context.Context) bool {
	return t.flushInternal(ctx.Done())
}

func (t *HTTPTransport) flushInternal(timeout <-chan struct{}) bool {
	// Wait until processing the current batch has started or the timeout.
	//
	// We must wait until the worker has seen the current batch, because it is
	// the only way b.done will be closed. If we do not wait, there is a
	// possible execution flow in which b.done is never closed, and the only way
	// out of Flush would be waiting for the timeout, which is undesired.
	var b batch

	for {
		select {
		case b = <-t.buffer:
			select {
			case <-b.started:
				goto started
			default:
				t.buffer <- b
			}
		case <-timeout:
			goto fail
		}
	}

started:
	// Signal that there won't be any more items in this batch, so that the
	// worker inner loop can end.
	close(b.items)
	// Start a new batch for subsequent events.
	t.buffer <- batch{
		items:   make(chan batchItem, t.BufferSize),
		started: make(chan struct{}),
		done:    make(chan struct{}),
	}

	// Wait until the current batch is done or the timeout.
	select {
	case <-b.done:
		debuglog.Println("Buffer flushed successfully.")
		return true
	case <-timeout:
		goto fail
	}

fail:
	debuglog.Println("Buffer flushing was canceled or timed out.")
	return false
}

// Close will terminate events sending loop.
// It useful to prevent goroutines leak in case of multiple HTTPTransport instances initiated.
//
// Close should be called after Flush and before terminating the program
// otherwise some events may be lost.
func (t *HTTPTransport) Close() {
	t.closeOnce.Do(func() {
		close(t.done)
	})
}

func (t *HTTPTransport) worker() {
	for b := range t.buffer {
		// Signal that processing of the current batch has started.
		close(b.started)

		// Return the batch to the buffer so that other goroutines can use it.
		// Equivalent to releasing a lock.
		t.buffer <- b

		// Process all batch items.
	loop:
		for {
			select {
			case <-t.done:
				return
			case item, open := <-b.items:
				if !open {
					break loop
				}
				if t.disabled(item.category) {
					continue
				}

				response, err := t.client.Do(item.request)
				if err != nil {
					debuglog.Printf("There was an issue with sending an event: %v", err)
					continue
				}
				util.HandleHTTPResponse(response, item.eventIdentifier)

				t.mu.Lock()
				if t.limits == nil {
					t.limits = make(ratelimit.Map)
				}
				t.limits.Merge(ratelimit.FromResponse(response))
				t.mu.Unlock()

				// Drain body up to a limit and close it, allowing the
				// transport to reuse TCP connections.
				_, _ = io.CopyN(io.Discard, response.Body, util.MaxDrainResponseBytes)
				response.Body.Close()
			}
		}

		// Signal that processing of the batch is done.
		close(b.done)
	}
}

func (t *HTTPTransport) disabled(c ratelimit.Category) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	disabled := t.limits.IsRateLimited(c)
	if disabled {
		debuglog.Printf("Too many requests for %q, backing off till: %v", c, t.limits.Deadline(c))
	}
	return disabled
}

// ================================
// HTTPSyncTransport
// ================================

// HTTPSyncTransport is a blocking implementation of Transport.
//
// Clients using this transport will send requests to Sentry sequentially and
// block until a response is returned.
//
// The blocking behavior is useful in a limited set of use cases. For example,
// use it when deploying code to a Function as a Service ("Serverless")
// platform, where any work happening in a background goroutine is not
// guaranteed to execute.
//
// For most cases, prefer HTTPTransport.
type HTTPSyncTransport struct {
	dsn       *Dsn
	client    *http.Client
	transport http.RoundTripper

	mu     sync.Mutex
	limits ratelimit.Map

	// HTTP Client request timeout. Defaults to 30 seconds.
	Timeout time.Duration
}

// NewHTTPSyncTransport returns a new pre-configured instance of HTTPSyncTransport.
func NewHTTPSyncTransport() *HTTPSyncTransport {
	transport := HTTPSyncTransport{
		Timeout: defaultTimeout,
		limits:  make(ratelimit.Map),
	}

	return &transport
}

// Configure is called by the Client itself, providing it it's own ClientOptions.
func (t *HTTPSyncTransport) Configure(options ClientOptions) {
	dsn, err := NewDsn(options.Dsn)
	if err != nil {
		debuglog.Printf("%v\n", err)
		return
	}
	t.dsn = dsn

	if options.HTTPTransport != nil {
		t.transport = options.HTTPTransport
	} else {
		t.transport = &http.Transport{
			Proxy:           getProxyConfig(options),
			TLSClientConfig: getTLSConfig(options),
		}
	}

	if options.HTTPClient != nil {
		t.client = options.HTTPClient
	} else {
		t.client = &http.Client{
			Transport: t.transport,
			Timeout:   t.Timeout,
		}
	}
}

// SendEvent assembles a new packet out of Event and sends it to the remote server.
func (t *HTTPSyncTransport) SendEvent(event *Event) {
	t.SendEventWithContext(context.Background(), event)
}

func (t *HTTPSyncTransport) Close() {}

// SendEventWithContext assembles a new packet out of Event and sends it to the remote server.
func (t *HTTPSyncTransport) SendEventWithContext(ctx context.Context, event *Event) {
	if t.dsn == nil {
		return
	}

	if t.disabled(event.toCategory()) {
		return
	}

	request, err := getRequestFromEvent(ctx, event, t.dsn)
	if err != nil {
		return
	}

	identifier := eventIdentifier(event)
	debuglog.Printf(
		"Sending %s to %s project: %s",
		identifier,
		t.dsn.GetHost(),
		t.dsn.GetProjectID(),
	)

	response, err := t.client.Do(request)
	if err != nil {
		debuglog.Printf("There was an issue with sending an event: %v", err)
		return
	}
	util.HandleHTTPResponse(response, identifier)

	t.mu.Lock()
	if t.limits == nil {
		t.limits = make(ratelimit.Map)
	}

	t.limits.Merge(ratelimit.FromResponse(response))
	t.mu.Unlock()

	// Drain body up to a limit and close it, allowing the
	// transport to reuse TCP connections.
	_, _ = io.CopyN(io.Discard, response.Body, util.MaxDrainResponseBytes)
	response.Body.Close()
}

// Flush is a no-op for HTTPSyncTransport. It always returns true immediately.
func (t *HTTPSyncTransport) Flush(_ time.Duration) bool {
	return true
}

// FlushWithContext is a no-op for HTTPSyncTransport. It always returns true immediately.
func (t *HTTPSyncTransport) FlushWithContext(_ context.Context) bool {
	return true
}

func (t *HTTPSyncTransport) disabled(c ratelimit.Category) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	disabled := t.limits.IsRateLimited(c)
	if disabled {
		debuglog.Printf("Too many requests for %q, backing off till: %v", c, t.limits.Deadline(c))
	}
	return disabled
}

// ================================
// noopTransport
// ================================

// noopTransport is an implementation of Transport interface which drops all the events.
// Only used internally when an empty DSN is provided, which effectively disables the SDK.
type noopTransport struct{}

var _ Transport = noopTransport{}

func (noopTransport) Configure(ClientOptions) {
	debuglog.Println("Sentry client initialized with an empty DSN. Using noopTransport. No events will be delivered.")
}

func (noopTransport) SendEvent(*Event) {
	debuglog.Println("Event dropped due to noopTransport usage.")
}

func (noopTransport) Flush(time.Duration) bool {
	return true
}

func (noopTransport) FlushWithContext(context.Context) bool {
	return true
}

func (noopTransport) Close() {}

// ================================
// Internal Transport Adapters
// ================================

// newInternalAsyncTransport creates a new AsyncTransport from internal/http
// wrapped to satisfy the Transport interface.
//
// This is not yet exposed in the public API and is for internal experimentation.
func newInternalAsyncTransport() Transport {
	return &internalAsyncTransportAdapter{}
}

// internalAsyncTransportAdapter wraps the internal AsyncTransport to implement
// the root-level Transport interface.
type internalAsyncTransportAdapter struct {
	transport protocol.TelemetryTransport
	dsn       *protocol.Dsn
}

func (a *internalAsyncTransportAdapter) Configure(options ClientOptions) {
	transportOptions := httpinternal.TransportOptions{
		Dsn:           options.Dsn,
		HTTPClient:    options.HTTPClient,
		HTTPTransport: options.HTTPTransport,
		HTTPProxy:     options.HTTPProxy,
		HTTPSProxy:    options.HTTPSProxy,
		CaCerts:       options.CaCerts,
	}

	a.transport = httpinternal.NewAsyncTransport(transportOptions)

	if options.Dsn != "" {
		dsn, err := protocol.NewDsn(options.Dsn)
		if err != nil {
			debuglog.Printf("Failed to parse DSN in adapter: %v\n", err)
		} else {
			a.dsn = dsn
		}
	}
}

func (a *internalAsyncTransportAdapter) SendEvent(event *Event) {
	header := &protocol.EnvelopeHeader{EventID: string(event.EventID), SentAt: time.Now(), Sdk: &protocol.SdkInfo{Name: event.Sdk.Name, Version: event.Sdk.Version}}
	if a.dsn != nil {
		header.Dsn = a.dsn.String()
	}
	if header.EventID == "" {
		header.EventID = protocol.GenerateEventID()
	}
	envelope := protocol.NewEnvelope(header)
	item, err := event.ToEnvelopeItem()
	if err != nil {
		debuglog.Printf("Failed to convert event to envelope item: %v", err)
		return
	}
	envelope.AddItem(item)

	for _, attachment := range event.Attachments {
		attachmentItem := protocol.NewAttachmentItem(attachment.Filename, attachment.ContentType, attachment.Payload)
		envelope.AddItem(attachmentItem)
	}

	if err := a.transport.SendEnvelope(envelope); err != nil {
		debuglog.Printf("Error sending envelope: %v", err)
	}
}

func (a *internalAsyncTransportAdapter) Flush(timeout time.Duration) bool {
	return a.transport.Flush(timeout)
}

func (a *internalAsyncTransportAdapter) FlushWithContext(ctx context.Context) bool {
	return a.transport.FlushWithContext(ctx)
}

func (a *internalAsyncTransportAdapter) Close() {
	a.transport.Close()
}
