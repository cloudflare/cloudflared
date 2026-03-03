package ratelimit

import (
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// Reference:
// https://github.com/getsentry/relay/blob/46dfaa850b8717a6e22c3e9a275ba17fe673b9da/relay-base-schema/src/data_category.rs#L231-L271

// Category classifies supported payload types that can be ingested by Sentry
// and, therefore, rate limited.
type Category string

// Known rate limit categories that are specified in rate limit headers.
const (
	CategoryUnknown     Category = "unknown" // Unknown category should not get rate limited
	CategoryAll         Category = ""        // Special category for empty categories (applies to all)
	CategoryError       Category = "error"
	CategoryTransaction Category = "transaction"
	CategoryLog         Category = "log_item"
	CategoryMonitor     Category = "monitor"
	CategoryTraceMetric Category = "trace_metric"
)

// knownCategories is the set of currently known categories. Other categories
// are ignored for the purpose of rate-limiting.
var knownCategories = map[Category]struct{}{
	CategoryAll:         {},
	CategoryError:       {},
	CategoryTransaction: {},
	CategoryLog:         {},
	CategoryMonitor:     {},
	CategoryTraceMetric: {},
}

// String returns the category formatted for debugging.
func (c Category) String() string {
	switch c {
	case CategoryAll:
		return "CategoryAll"
	case CategoryError:
		return "CategoryError"
	case CategoryTransaction:
		return "CategoryTransaction"
	case CategoryLog:
		return "CategoryLog"
	case CategoryMonitor:
		return "CategoryMonitor"
	case CategoryTraceMetric:
		return "CategoryTraceMetric"
	default:
		// For unknown categories, use the original formatting logic
		caser := cases.Title(language.English)
		rv := "Category"
		for _, w := range strings.Fields(string(c)) {
			rv += caser.String(w)
		}
		return rv
	}
}

// Priority represents the importance level of a category for buffer management.
type Priority int

const (
	PriorityCritical Priority = iota + 1
	PriorityHigh
	PriorityMedium
	PriorityLow
	PriorityLowest
)

func (p Priority) String() string {
	switch p {
	case PriorityCritical:
		return "critical"
	case PriorityHigh:
		return "high"
	case PriorityMedium:
		return "medium"
	case PriorityLow:
		return "low"
	case PriorityLowest:
		return "lowest"
	default:
		return "unknown"
	}
}

// GetPriority returns the priority level for this category.
func (c Category) GetPriority() Priority {
	switch c {
	case CategoryError:
		return PriorityCritical
	case CategoryMonitor:
		return PriorityHigh
	case CategoryLog:
		return PriorityLow
	case CategoryTransaction:
		return PriorityMedium
	case CategoryTraceMetric:
		return PriorityLow
	default:
		return PriorityMedium
	}
}
