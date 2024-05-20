package tunnel

import (
	"testing"

	"github.com/cloudflare/cloudflared/tunnelrpc/pogs"

	"github.com/stretchr/testify/assert"
)

func TestSingleTag(t *testing.T) {
	testCases := []struct {
		Input  string
		Output pogs.Tag
		Fail   bool
	}{
		{Input: "x=y", Output: pogs.Tag{Name: "x", Value: "y"}},
		{Input: "More-Complex=Tag Values", Output: pogs.Tag{Name: "More-Complex", Value: "Tag Values"}},
		{Input: "First=Equals=Wins", Output: pogs.Tag{Name: "First", Value: "Equals=Wins"}},
		{Input: "x=", Fail: true},
		{Input: "=y", Fail: true},
		{Input: "=", Fail: true},
		{Input: "No spaces allowed=in key names", Fail: true},
		{Input: "omg\nwtf=bbq", Fail: true},
	}
	for i, testCase := range testCases {
		tag, ok := NewTagFromCLI(testCase.Input)
		assert.Equalf(t, !testCase.Fail, ok, "mismatched success for test case %d", i)
		assert.Equalf(t, testCase.Output, tag, "mismatched output for test case %d", i)
	}
}

func TestTagSlice(t *testing.T) {
	tagSlice, err := NewTagSliceFromCLI([]string{"a=b", "c=d", "e=f"})
	assert.NoError(t, err)
	assert.Len(t, tagSlice, 3)
	assert.Equal(t, "a", tagSlice[0].Name)
	assert.Equal(t, "b", tagSlice[0].Value)
	assert.Equal(t, "c", tagSlice[1].Name)
	assert.Equal(t, "d", tagSlice[1].Value)
	assert.Equal(t, "e", tagSlice[2].Name)
	assert.Equal(t, "f", tagSlice[2].Value)

	tagSlice, err = NewTagSliceFromCLI([]string{"a=b", "=", "e=f"})
	assert.Error(t, err)
}
