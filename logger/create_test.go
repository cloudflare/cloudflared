package logger

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLogLevelParse(t *testing.T) {
	lvls, err := ParseLevelString("fatal")
	assert.NoError(t, err)
	assert.Equal(t, []Level{FatalLevel}, lvls)

	lvls, err = ParseLevelString("error")
	assert.NoError(t, err)
	assert.Equal(t, []Level{FatalLevel, ErrorLevel}, lvls)

	lvls, err = ParseLevelString("info")
	assert.NoError(t, err)
	assert.Equal(t, []Level{FatalLevel, ErrorLevel, InfoLevel}, lvls)

	lvls, err = ParseLevelString("info")
	assert.NoError(t, err)
	assert.Equal(t, []Level{FatalLevel, ErrorLevel, InfoLevel}, lvls)

	lvls, err = ParseLevelString("warn")
	assert.NoError(t, err)
	assert.Equal(t, []Level{FatalLevel, ErrorLevel, InfoLevel}, lvls)

	lvls, err = ParseLevelString("debug")
	assert.NoError(t, err)
	assert.Equal(t, []Level{FatalLevel, ErrorLevel, InfoLevel, DebugLevel}, lvls)

	_, err = ParseLevelString("blah")
	assert.Error(t, err)

	_, err = ParseLevelString("")
	assert.Error(t, err)
}

func TestPathSanitizer(t *testing.T) {
	assert.Equal(t, "somebad/path/log.bat.log", SanitizeLogPath("\t somebad/path/log.bat\n\n"))
	assert.Equal(t, "proper/path/cloudflared.log", SanitizeLogPath("proper/path/cloudflared.log"))
	assert.Equal(t, "proper/path/", SanitizeLogPath("proper/path/"))
	assert.Equal(t, "proper/path/cloudflared.log", SanitizeLogPath("\tproper/path/cloudflared\n\n"))
}
