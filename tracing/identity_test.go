package tracing

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewIdentity(t *testing.T) {
	testCases := []struct {
		testCase string
		trace    string
		valid    bool
	}{
		{
			testCase: "full length trace",
			trace:    "ec31ad8a01fde11fdcabe2efdce36873:52726f6cabc144f5:0:1",
			valid:    true,
		},
		{
			testCase: "short trace ID",
			trace:    "ad8a01fde11fdcabe2efdce36873:52726f6cabc144f5:0:1",
			valid:    true,
		},
		{
			testCase: "no trace",
			trace:    "",
			valid:    false,
		},
		{
			testCase: "missing flags",
			trace:    "ec31ad8a01fde11fdcabe2efdce36873:52726f6cabc144f5:0",
			valid:    false,
		},
		{
			testCase: "missing separator",
			trace:    "ec31ad8a01fde11fdcabe2efdce3687352726f6cabc144f501",
			valid:    false,
		},
	}

	for _, testCase := range testCases {
		identity, err := NewIdentity(testCase.trace)
		if testCase.valid {
			require.NoError(t, err, testCase.testCase)
			require.Equal(t, testCase.trace, identity.String())
		} else {
			require.Error(t, err)
			require.Nil(t, identity)
		}
	}
}
