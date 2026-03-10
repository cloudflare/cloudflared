package sentry

import (
	"fmt"
	"reflect"
	"slices"
)

const (
	MechanismTypeGeneric string = "generic"
	MechanismTypeChained string = "chained"
	MechanismTypeUnwrap  string = "unwrap"
	MechanismSourceCause string = "cause"
)

type visited struct {
	ptrs map[uintptr]struct{}
	msgs map[string]struct{}
}

func (v *visited) seenError(err error) bool {
	t := reflect.ValueOf(err)
	if t.Kind() == reflect.Ptr && !t.IsNil() {
		ptr := t.Pointer()
		if _, ok := v.ptrs[ptr]; ok {
			return true
		}
		v.ptrs[ptr] = struct{}{}
		return false
	}

	key := t.String() + err.Error()
	if _, ok := v.msgs[key]; ok {
		return true
	}
	v.msgs[key] = struct{}{}
	return false
}

func convertErrorToExceptions(err error, maxErrorDepth int) []Exception {
	var exceptions []Exception
	vis := &visited{
		ptrs: make(map[uintptr]struct{}),
		msgs: make(map[string]struct{}),
	}
	convertErrorDFS(err, &exceptions, nil, "", vis, maxErrorDepth, 0)

	// mechanism type is used for debugging purposes, but since we can't really distinguish the origin of who invoked
	// captureException, we set it to nil if the error is not chained.
	if len(exceptions) == 1 {
		exceptions[0].Mechanism = nil
	}

	slices.Reverse(exceptions)

	// Add a trace of the current stack to the top level(outermost) error in a chain if
	// it doesn't have a stack trace yet.
	// We only add to the most recent error to avoid duplication and because the
	// current stack is most likely unrelated to errors deeper in the chain.
	if len(exceptions) > 0 && exceptions[len(exceptions)-1].Stacktrace == nil {
		exceptions[len(exceptions)-1].Stacktrace = NewStacktrace()
	}

	return exceptions
}

func convertErrorDFS(err error, exceptions *[]Exception, parentID *int, source string, visited *visited, maxErrorDepth int, currentDepth int) {
	if err == nil {
		return
	}

	if visited.seenError(err) {
		return
	}

	_, isExceptionGroup := err.(interface{ Unwrap() []error })

	exception := Exception{
		Value:      err.Error(),
		Type:       reflect.TypeOf(err).String(),
		Stacktrace: ExtractStacktrace(err),
	}

	currentID := len(*exceptions)

	var mechanismType string

	if parentID == nil {
		mechanismType = MechanismTypeGeneric
		source = ""
	} else {
		mechanismType = MechanismTypeChained
	}

	exception.Mechanism = &Mechanism{
		Type:             mechanismType,
		ExceptionID:      currentID,
		ParentID:         parentID,
		Source:           source,
		IsExceptionGroup: isExceptionGroup,
	}

	*exceptions = append(*exceptions, exception)

	if maxErrorDepth >= 0 && currentDepth >= maxErrorDepth {
		return
	}

	switch v := err.(type) {
	case interface{ Unwrap() []error }:
		unwrapped := v.Unwrap()
		for i := range unwrapped {
			if unwrapped[i] != nil {
				childSource := fmt.Sprintf("errors[%d]", i)
				convertErrorDFS(unwrapped[i], exceptions, &currentID, childSource, visited, maxErrorDepth, currentDepth+1)
			}
		}
	case interface{ Unwrap() error }:
		unwrapped := v.Unwrap()
		if unwrapped != nil {
			convertErrorDFS(unwrapped, exceptions, &currentID, MechanismTypeUnwrap, visited, maxErrorDepth, currentDepth+1)
		}
	case interface{ Cause() error }:
		cause := v.Cause()
		if cause != nil {
			convertErrorDFS(cause, exceptions, &currentID, MechanismSourceCause, visited, maxErrorDepth, currentDepth+1)
		}
	}
}
