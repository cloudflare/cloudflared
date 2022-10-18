package tracing

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewIdentity(t *testing.T) {
	testCases := []struct {
		testCase string
		trace    string
		expected string
	}{
		{
			testCase: "full length trace",
			trace:    "ec31ad8a01fde11fdcabe2efdce36873:52726f6cabc144f5:0:1",
			expected: "ec31ad8a01fde11fdcabe2efdce36873:52726f6cabc144f5:0:1",
		},
		{
			testCase: "short trace ID",
			trace:    "ad8a01fde11fdcabe2efdce36873:52726f6cabc144f5:0:1",
			expected: "0000ad8a01fde11fdcabe2efdce36873:52726f6cabc144f5:0:1",
		},
		{
			testCase: "short trace ID with 0s in the middle",
			trace:    "ad8a01fde11f000002efdce36873:52726f6cabc144f5:0:1",
			expected: "0000ad8a01fde11f000002efdce36873:52726f6cabc144f5:0:1",
		},
		{
			testCase: "short trace ID with 0s in the beginning and middle",
			trace:    "001ad8a01fde11fdcabe2efdce36873:52726f6cabc144f5:0:1",
			expected: "0001ad8a01fde11fdcabe2efdce36873:52726f6cabc144f5:0:1",
		},
		{
			testCase: "no trace",
			trace:    "",
		},
		{
			testCase: "missing flags",
			trace:    "ec31ad8a01fde11fdcabe2efdce36873:52726f6cabc144f5:0",
		},
		{
			testCase: "missing separator",
			trace:    "ec31ad8a01fde11fdcabe2efdce3687352726f6cabc144f501",
		},
	}

	for _, testCase := range testCases {
		identity, err := NewIdentity(testCase.trace)
		if testCase.expected != "" {
			require.NoError(t, err)
			require.Equal(t, testCase.expected, identity.String())

			serializedIdentity, err := identity.MarshalBinary()
			require.NoError(t, err)
			deserializedIdentity := new(Identity)
			err = deserializedIdentity.UnmarshalBinary(serializedIdentity)
			require.NoError(t, err)
			require.Equal(t, identity, deserializedIdentity)

		} else {
			require.Error(t, err)
			require.Nil(t, identity)
		}
	}
}
