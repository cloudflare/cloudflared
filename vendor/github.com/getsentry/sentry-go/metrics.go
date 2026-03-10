package sentry

import (
	"context"
	"maps"
	"os"
	"sync"
	"time"

	"github.com/getsentry/sentry-go/attribute"
	"github.com/getsentry/sentry-go/internal/debuglog"
)

// Duration Units.
const (
	UnitNanosecond  = "nanosecond"
	UnitMicrosecond = "microsecond"
	UnitMillisecond = "millisecond"
	UnitSecond      = "second"
	UnitMinute      = "minute"
	UnitHour        = "hour"
	UnitDay         = "day"
	UnitWeek        = "week"
)

// Information Units.
const (
	UnitBit      = "bit"
	UnitByte     = "byte"
	UnitKilobyte = "kilobyte"
	UnitKibibyte = "kibibyte"
	UnitMegabyte = "megabyte"
	UnitMebibyte = "mebibyte"
	UnitGigabyte = "gigabyte"
	UnitGibibyte = "gibibyte"
	UnitTerabyte = "terabyte"
	UnitTebibyte = "tebibyte"
	UnitPetabyte = "petabyte"
	UnitPebibyte = "pebibyte"
	UnitExabyte  = "exabyte"
	UnitExbibyte = "exbibyte"
)

// Fraction Units.
const (
	UnitRatio   = "ratio"
	UnitPercent = "percent"
)

// NewMeter returns a new Meter. If there is no Client bound to the current hub, or if metrics are disabled,
// it returns a no-op Meter that discards all metrics.
func NewMeter(ctx context.Context) Meter {
	hub := GetHubFromContext(ctx)
	if hub == nil {
		hub = CurrentHub()
	}
	client := hub.Client()
	if client != nil && !client.options.DisableMetrics {
		// build default attrs
		serverAddr := client.options.ServerName
		if serverAddr == "" {
			serverAddr, _ = os.Hostname()
		}

		defaults := map[string]string{
			"sentry.release":        client.options.Release,
			"sentry.environment":    client.options.Environment,
			"sentry.server.address": serverAddr,
			"sentry.sdk.name":       client.sdkIdentifier,
			"sentry.sdk.version":    client.sdkVersion,
		}

		defaultAttrs := make(map[string]attribute.Value)
		for k, v := range defaults {
			if v != "" {
				defaultAttrs[k] = attribute.StringValue(v)
			}
		}

		return &sentryMeter{
			ctx:               ctx,
			hub:               hub,
			attributes:        make(map[string]attribute.Value),
			defaultAttributes: defaultAttrs,
			mu:                sync.RWMutex{},
		}
	}

	debuglog.Printf("fallback to noopMeter: metrics disabled")
	return &noopMeter{}
}

type sentryMeter struct {
	ctx               context.Context
	hub               *Hub
	attributes        map[string]attribute.Value
	defaultAttributes map[string]attribute.Value
	mu                sync.RWMutex
}

func (m *sentryMeter) emit(ctx context.Context, metricType MetricType, name string, value MetricValue, unit string, attributes map[string]attribute.Value, customScope *Scope) {
	if name == "" {
		debuglog.Println("empty name provided, dropping metric")
		return
	}

	hub := hubFromContexts(ctx, m.ctx)
	if hub == nil {
		hub = m.hub
	}

	client := hub.Client()
	if client == nil {
		return
	}

	scope := hub.Scope()
	if customScope != nil {
		scope = customScope
	}
	traceID, spanID := resolveTrace(scope, ctx, m.ctx)

	// Pre-allocate with capacity hint to avoid map growth reallocations
	estimatedCap := len(m.defaultAttributes) + len(attributes) + 8 // scope ~3 + call-specific ~5
	attrs := make(map[string]attribute.Value, estimatedCap)

	// attribute precedence: default -> scope -> instance (from SetAttrs) -> entry-specific
	for k, v := range m.defaultAttributes {
		attrs[k] = v
	}
	scope.populateAttrs(attrs)

	m.mu.RLock()
	for k, v := range m.attributes {
		attrs[k] = v
	}
	m.mu.RUnlock()

	for k, v := range attributes {
		attrs[k] = v
	}

	metric := &Metric{
		Timestamp:  time.Now(),
		TraceID:    traceID,
		SpanID:     spanID,
		Type:       metricType,
		Name:       name,
		Value:      value,
		Unit:       unit,
		Attributes: attrs,
	}

	if client.captureMetric(metric, scope) && client.options.Debug {
		debuglog.Printf("Metric %s [%s]: %v %s", metricType, name, value.AsInterface(), unit)
	}
}

// WithCtx returns a new Meter that uses the given context for trace/span association.
func (m *sentryMeter) WithCtx(ctx context.Context) Meter {
	m.mu.RLock()
	attrsCopy := maps.Clone(m.attributes)
	m.mu.RUnlock()

	return &sentryMeter{
		ctx:               ctx,
		hub:               m.hub,
		attributes:        attrsCopy,
		defaultAttributes: m.defaultAttributes,
		mu:                sync.RWMutex{},
	}
}

func (m *sentryMeter) applyOptions(opts []MeterOption) *meterOptions {
	o := &meterOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// Count implements Meter.
func (m *sentryMeter) Count(name string, count int64, opts ...MeterOption) {
	o := m.applyOptions(opts)
	m.emit(m.ctx, MetricTypeCounter, name, Int64MetricValue(count), o.unit, o.attributes, o.scope)
}

// Distribution implements Meter.
func (m *sentryMeter) Distribution(name string, sample float64, opts ...MeterOption) {
	o := m.applyOptions(opts)
	m.emit(m.ctx, MetricTypeDistribution, name, Float64MetricValue(sample), o.unit, o.attributes, o.scope)
}

// Gauge implements Meter.
func (m *sentryMeter) Gauge(name string, value float64, opts ...MeterOption) {
	o := m.applyOptions(opts)
	m.emit(m.ctx, MetricTypeGauge, name, Float64MetricValue(value), o.unit, o.attributes, o.scope)
}

// SetAttributes implements Meter.
func (m *sentryMeter) SetAttributes(attrs ...attribute.Builder) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, a := range attrs {
		if a.Value.Type() == attribute.INVALID {
			debuglog.Printf("invalid attribute: %v", a)
			continue
		}
		m.attributes[a.Key] = a.Value
	}
}

// noopMeter is a no-operation implementation of Meter.
// This is used when there is no client available in the context or when metrics are disabled.
type noopMeter struct{}

// WithCtx implements Meter.
func (n *noopMeter) WithCtx(_ context.Context) Meter {
	return n
}

// Count implements Meter.
func (n *noopMeter) Count(name string, _ int64, _ ...MeterOption) {
	debuglog.Printf("Metric %q is being dropped. Turn on metrics by setting DisableMetrics to false", name)
}

// Distribution implements Meter.
func (n *noopMeter) Distribution(name string, _ float64, _ ...MeterOption) {
	debuglog.Printf("Metric %q is being dropped. Turn on metrics by setting DisableMetrics to false", name)
}

// Gauge implements Meter.
func (n *noopMeter) Gauge(name string, _ float64, _ ...MeterOption) {
	debuglog.Printf("Metric %q is being dropped. Turn on metrics by setting DisableMetrics to false", name)
}

// SetAttributes implements Meter.
func (n *noopMeter) SetAttributes(_ ...attribute.Builder) {
	debuglog.Printf("No attributes attached. Turn on metrics by setting DisableMetrics to false")
}
