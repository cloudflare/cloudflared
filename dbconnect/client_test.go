package dbconnect

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCommandValidateEmpty(t *testing.T) {
	stmts := []string{
		"",
		";",
		" \n\t",
		";\n;\t;",
	}

	for _, stmt := range stmts {
		cmd := Command{Statement: stmt}

		assert.Error(t, cmd.Validate(), stmt)
	}
}

func TestCommandValidateMode(t *testing.T) {
	modes := []string{
		"",
		"query",
		"ExEc",
		"PREPARE",
	}

	for _, mode := range modes {
		cmd := Command{Statement: "Ok", Mode: mode}

		assert.NoError(t, cmd.Validate(), mode)
		assert.Equal(t, strings.ToLower(mode), cmd.Mode)
	}
}

func TestCommandValidateIsolation(t *testing.T) {
	isos := []string{
		"",
		"default",
		"read_committed",
		"SNAPshot",
	}

	for _, iso := range isos {
		cmd := Command{Statement: "Ok", Isolation: iso}

		assert.NoError(t, cmd.Validate(), iso)
		assert.Equal(t, strings.ToLower(iso), cmd.Isolation)
	}
}

func TestCommandValidateTimeout(t *testing.T) {
	cmd := Command{Statement: "Ok", Timeout: 0}

	assert.NoError(t, cmd.Validate())
	assert.NotZero(t, cmd.Timeout)

	cmd = Command{Statement: "Ok", Timeout: 1 * time.Second}

	assert.NoError(t, cmd.Validate())
	assert.Equal(t, 1*time.Second, cmd.Timeout)
}

func TestCommandValidateArguments(t *testing.T) {
	cmd := Command{Statement: "Ok", Arguments: Arguments{
		Named:      map[string]interface{}{"key": "val"},
		Positional: []interface{}{"val"},
	}}

	assert.Error(t, cmd.Validate())
}

func TestCommandUnmarshalJSON(t *testing.T) {
	strs := []string{
		"{\"statement\":\"Ok\"}",
		"{\"statement\":\"Ok\",\"arguments\":[0, 3.14, \"apple\"],\"mode\":\"query\"}",
		"{\"statement\":\"Ok\",\"isolation\":\"read_uncommitted\",\"timeout\":1000}",
	}

	for _, str := range strs {
		var cmd Command
		assert.NoError(t, json.Unmarshal([]byte(str), &cmd), str)
	}

	strs = []string{
		"",
		"\"",
		"{}",
		"{\"argument\":{\"key\":\"val\"}}",
		"{\"statement\":[\"Ok\"]}",
	}

	for _, str := range strs {
		var cmd Command
		assert.Error(t, json.Unmarshal([]byte(str), &cmd), str)
	}
}

func TestArgumentsValidateNotNil(t *testing.T) {
	args := Arguments{}

	assert.NoError(t, args.Validate())
	assert.NotNil(t, args.Named)
	assert.NotNil(t, args.Positional)
}

func TestArgumentsValidateMutuallyExclusive(t *testing.T) {
	args := []Arguments{
		Arguments{},
		Arguments{Named: map[string]interface{}{"key": "val"}},
		Arguments{Positional: []interface{}{"val"}},
	}

	for _, arg := range args {
		assert.NoError(t, arg.Validate())
		assert.False(t, len(arg.Named) > 0 && len(arg.Positional) > 0)
	}

	args = []Arguments{
		Arguments{
			Named:      map[string]interface{}{"key": "val"},
			Positional: []interface{}{"val"},
		},
	}

	for _, arg := range args {
		assert.Error(t, arg.Validate())
		assert.True(t, len(arg.Named) > 0 && len(arg.Positional) > 0)
	}
}

func TestArgumentsValidateKeys(t *testing.T) {
	keys := []string{
		"",
		"_",
		"_key",
		"1",
		"1key",
		"\xf0\x28\x8c\xbc", // non-utf8
	}

	for _, key := range keys {
		args := Arguments{Named: map[string]interface{}{key: "val"}}

		assert.Error(t, args.Validate(), key)
	}
}

func TestArgumentsUnmarshalJSON(t *testing.T) {
	strs := []string{
		"{}",
		"{\"key\":\"val\"}",
		"{\"key\":[1, 3.14, {\"key\":\"val\"}]}",
		"[]",
		"[\"key\",\"val\"]",
		"[{}]",
	}

	for _, str := range strs {
		var args Arguments
		assert.NoError(t, json.Unmarshal([]byte(str), &args), str)
	}

	strs = []string{
		"",
		"\"",
		"1",
		"\"key\"",
		"{\"key\",\"val\"}",
	}

	for _, str := range strs {
		var args Arguments
		assert.Error(t, json.Unmarshal([]byte(str), &args), str)
	}
}
