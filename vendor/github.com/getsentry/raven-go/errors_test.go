package raven

import (
	"fmt"
	"reflect"
	"testing"

	pkgErrors "github.com/pkg/errors"
)

func TestWrapWithExtraGeneratesProperErrWithExtra(t *testing.T) {
	errMsg := "This is bad"
	baseErr := fmt.Errorf(errMsg)
	extraInfo := map[string]interface{}{
		"string": "string",
		"int":    1,
		"float":  1.001,
		"bool":   false,
	}

	testErr := WrapWithExtra(baseErr, extraInfo)
	wrapped, ok := testErr.(ErrWithExtra)
	if !ok {
		t.Errorf("Wrapped error does not conform to expected protocol.")
	}

	if !reflect.DeepEqual(wrapped.Cause(), baseErr) {
		t.Errorf("Failed to unwrap error, got %+v, expected %+v", wrapped.Cause(), baseErr)
	}

	returnedExtra := wrapped.ExtraInfo()
	for expectedKey, expectedVal := range extraInfo {
		val, ok := returnedExtra[expectedKey]
		if !ok {
			t.Errorf("Extra data missing key: %s", expectedKey)
		}
		if val != expectedVal {
			t.Errorf("Extra data [%s]: Got: %+v, expected: %+v", expectedKey, val, expectedVal)
		}
	}

	if wrapped.Error() != errMsg {
		t.Errorf("Wrong error message, got: %q, expected: %q", wrapped.Error(), errMsg)
	}
}

func TestWrapWithExtraGeneratesCausableError(t *testing.T) {
	baseErr := fmt.Errorf("this is bad")
	testErr := WrapWithExtra(baseErr, nil)
	cause := pkgErrors.Cause(testErr)

	if !reflect.DeepEqual(cause, baseErr) {
		t.Errorf("Failed to unwrap error, got %+v, expected %+v", cause, baseErr)
	}
}

func TestExtractErrorPullsExtraData(t *testing.T) {
	extraInfo := map[string]interface{}{
		"string": "string",
		"int":    1,
		"float":  1.001,
		"bool":   false,
	}
	emptyInfo := map[string]interface{}{}

	testCases := []struct {
		Error    error
		Expected map[string]interface{}
	}{
		// Unwrapped error shouldn't include anything
		{
			Error:    fmt.Errorf("This is bad"),
			Expected: emptyInfo,
		},
		// Wrapped error with nil map should extract as empty info
		{
			Error:    WrapWithExtra(fmt.Errorf("This is bad"), nil),
			Expected: emptyInfo,
		},
		// Wrapped error with empty map should extract as empty info
		{
			Error:    WrapWithExtra(fmt.Errorf("This is bad"), emptyInfo),
			Expected: emptyInfo,
		},
		// Wrapped error with extra info should extract with all data
		{
			Error:    WrapWithExtra(fmt.Errorf("This is bad"), extraInfo),
			Expected: extraInfo,
		},
		// Nested wrapped error should extract all the info
		{
			Error: WrapWithExtra(
				WrapWithExtra(fmt.Errorf("This is bad"),
					map[string]interface{}{
						"inner": "123",
					}),
				map[string]interface{}{
					"outer": "456",
				},
			),
			Expected: map[string]interface{}{
				"inner": "123",
				"outer": "456",
			},
		},
		// Futher wrapping of errors shouldn't allow for value override
		{
			Error: WrapWithExtra(
				WrapWithExtra(fmt.Errorf("This is bad"),
					map[string]interface{}{
						"dontoverride": "123",
					}),
				map[string]interface{}{
					"dontoverride": "456",
				},
			),
			Expected: map[string]interface{}{
				"dontoverride": "123",
			},
		},
	}

	for i, test := range testCases {
		extracted := extractExtra(test.Error)
		if len(test.Expected) != len(extracted) {
			t.Errorf(
				"Case [%d]: Mismatched amount of data between provided and extracted extra. Got: %+v Expected: %+v",
				i,
				extracted,
				test.Expected,
			)
		}

		for expectedKey, expectedVal := range test.Expected {
			val, ok := extracted[expectedKey]
			if !ok {
				t.Errorf("Case [%d]: Extra data missing key: %s", i, expectedKey)
			}
			if val != expectedVal {
				t.Errorf("Case [%d]: Wrong extra data for %q. Got: %+v, expected: %+v", i, expectedKey, val, expectedVal)
			}
		}
	}
}
