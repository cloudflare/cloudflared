package servertiming

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Metric represents a single metric for the Server-Timing header.
//
// The easiest way to use the Metric is to use NewMetric and chain it. This
// results in a single line defer at the top of a function time a function.
//
//   timing := FromContext(r.Context())
//   defer timing.NewMetric("sql").Start().Stop()
//
// For timing around specific blocks of code:
//
//   m := timing.NewMetric("sql").Start()
//   // ... run your code being timed here
//   m.Stop()
//
// A metric is expected to represent a single timing event. Therefore,
// no functions on the struct are safe for concurrency by default. If a single
// Metric is shared by multiple concurrenty goroutines, you must lock access
// manually.
type Metric struct {
	// Name is the name of the metric. This must be a valid RFC7230 "token"
	// format. In a gist, this is an alphanumeric string that may contain
	// most common symbols but may not contain any whitespace. The exact
	// syntax can be found in RFC7230.
	//
	// It is common for Name to be a unique identifier (such as "sql-1") and
	// for a more human-friendly name to be used in the "desc" field.
	Name string

	// Duration is the duration of this Metric.
	Duration time.Duration

	// Desc is any string describing this metric. For example: "SQL Primary".
	// The specific format of this is `token | quoted-string` according to
	// RFC7230.
	Desc string

	// Extra is a set of extra parameters and values to send with the
	// metric. The specification states that unrecognized parameters are
	// to be ignored so it should be safe to add additional data here. The
	// key must be a valid "token" (same syntax as Name) and the value can
	// be any "token | quoted-string" (same as Desc field).
	//
	// If this map contains a key that would be sent by another field in this
	// struct (such as "desc"), then this value is prioritized over the
	// struct value.
	Extra map[string]string

	// startTime is the time that this metric recording was started if
	// Start() was called.
	startTime time.Time
}

// WithDesc is a chaining-friendly helper to set the Desc field on the Metric.
func (m *Metric) WithDesc(desc string) *Metric {
	m.Desc = desc
	return m
}

// Start starts a timer for recording the duration of some task. This must
// be paired with a Stop call to set the duration. Calling this again will
// reset the start time for a subsequent Stop call.
func (m *Metric) Start() *Metric {
	m.startTime = time.Now()
	return m
}

// Stop ends the timer started with Start and records the duration in the
// Duration field. Calling this multiple times will modify the Duration based
// on the last time Start was called.
//
// If Start was never called, this function has zero effect.
func (m *Metric) Stop() *Metric {
	// Only record if we have a start time set with Start()
	if !m.startTime.IsZero() {
		m.Duration = time.Since(m.startTime)
	}

	return m
}

// String returns the valid Server-Timing metric entry value.
func (m *Metric) String() string {
	// Begin building parts, expected capacity is length of extra
	// fields plus id, desc, dur.
	parts := make([]string, 1, len(m.Extra)+3)
	parts[0] = m.Name

	// Description
	if _, ok := m.Extra[paramNameDesc]; !ok && m.Desc != "" {
		parts = append(parts, headerEncodeParam(paramNameDesc, m.Desc))
	}

	// Duration
	if _, ok := m.Extra[paramNameDur]; !ok && m.Duration > 0 {
		parts = append(parts, headerEncodeParam(
			paramNameDur,
			strconv.FormatFloat(float64(m.Duration)/float64(time.Millisecond), 'f', -1, 64),
		))
	}

	// All remaining extra params
	for k, v := range m.Extra {
		parts = append(parts, headerEncodeParam(k, v))
	}

	return strings.Join(parts, ";")
}

// GoString is needed for fmt.GoStringer so %v works on pointer value.
func (m *Metric) GoString() string {
	if m == nil {
		return "nil"
	}

	return fmt.Sprintf("*%#v", *m)
}
