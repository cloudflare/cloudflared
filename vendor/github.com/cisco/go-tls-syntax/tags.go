package syntax

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// Allow types to mark themselves as valid for TLS to marshal/unmarshal
type Validator interface {
	ValidForTLS() error
}

var (
	validatorType = reflect.TypeOf(new(Validator)).Elem()
)

// `tls:"head=2,min=2,max=255,varint"`

type fieldOptions struct {
	omitHeader   bool // whether to omit the slice header
	varintHeader bool // whether to encode the header length as a varint
	headerSize   int  // length of length in bytes
	minSize      int  // minimum vector size in bytes
	maxSize      int  // maximum vector size in bytes

	varint   bool // whether to encode as a varint
	optional bool // whether to encode pointer as optional
	omit     bool // whether to skip a field
}

func mutuallyExclusive(vals []bool) bool {
	set := 0
	for _, val := range vals {
		if val {
			set += 1
		}
	}
	return set <= 1
}

func (opts fieldOptions) Consistent() bool {
	// No more than one of the header options must be set
	headerPaths := []bool{opts.omitHeader, opts.varintHeader, opts.headerSize > 1}
	if !mutuallyExclusive(headerPaths) {
		return false
	}

	// Max must be greater than min
	if opts.maxSize > 0 && opts.minSize > opts.maxSize {
		return false
	}

	// varint and optional are mutually exclusive with each other, and with the slice options
	headerOpts := (opts.omitHeader || opts.varintHeader || opts.headerSize > 1 || opts.maxSize > 0 || opts.minSize > 0)
	encodePaths := []bool{headerOpts, opts.varint, opts.optional}
	if !mutuallyExclusive(encodePaths) {
		return false
	}

	// Omit is mutually exclusive with everything else
	otherThanOmit := (headerOpts || opts.varint || opts.optional)
	if !mutuallyExclusive([]bool{opts.omit, otherThanOmit}) {
		return false
	}

	return true
}

func (opts fieldOptions) ValidForType(t reflect.Type) bool {
	headerType := t.Kind() == reflect.Slice || t.Kind() == reflect.Map
	headerTags := opts.omitHeader || opts.varintHeader || (opts.headerSize != 0) ||
		(opts.minSize != 0) || (opts.maxSize != 0)
	if headerTags && !headerType {
		return false
	}

	uintRequired := opts.varint
	if uintRequired {
		switch t.Kind() {
		case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		default:
			return false
		}
	}

	ptrRequired := opts.optional
	if ptrRequired && t.Kind() != reflect.Ptr {
		return false
	}

	return true
}

var (
	varintOption   = "varint"
	optionalOption = "optional"
	omitOption     = "omit"

	headOptionNone   = "none"
	headOptionVarint = "varint"
	headValueNoHead  = uint(255)
	headValueVarint  = uint(254)

	optionalFlagAbsent  uint8 = 0
	optionalFlagPresent uint8 = 1
)

func atoi(a string) int {
	i, err := strconv.Atoi(a)
	if err != nil {
		panic(fmt.Errorf("Invalid header size: %v", err))
	}
	return i
}

// parseTag parses a struct field's "tls" tag as a comma-separated list of
// name=value pairs, where the values MUST be unsigned integers, or in
// the special case of head, "none" or "varint"
func parseTag(tag string) fieldOptions {
	opts := fieldOptions{}
	for _, token := range strings.Split(tag, ",") {
		parts := strings.Split(token, "=")

		// Handle name-only entries
		if len(parts) == 1 {
			switch parts[0] {
			case varintOption:
				opts.varint = true
			case optionalOption:
				opts.optional = true
			case omitOption:
				opts.omit = true
			default:
				// XXX(rlb): Ignoring unknown fields
			}
			continue
		}

		if len(parts) != 2 || len(parts[0]) == 0 || len(parts[1]) == 0 {
			panic(fmt.Errorf("Malformed tag"))
		}

		// Handle name=value entries
		switch parts[0] {
		case "head":
			switch {
			case parts[1] == headOptionNone:
				opts.omitHeader = true
			case parts[1] == headOptionVarint:
				opts.varintHeader = true
			default:
				opts.headerSize = atoi(parts[1])
			}

		case "min":
			opts.minSize = atoi(parts[1])

		case "max":
			opts.maxSize = atoi(parts[1])

		default:
			// XXX(rlb): Ignoring unknown fields
		}
	}

	if !opts.Consistent() {
		panic(fmt.Errorf("Inconsistent options"))
	}

	return opts
}
