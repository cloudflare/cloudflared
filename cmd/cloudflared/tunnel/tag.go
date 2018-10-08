package tunnel

import (
	"fmt"
	"regexp"

	tunnelpogs "github.com/cloudflare/cloudflared/tunnelrpc/pogs"
)

// Restrict key names to characters allowed in an HTTP header name.
// Restrict key values to printable characters (what is recognised as data in an HTTP header value).
var tagRegexp = regexp.MustCompile("^([a-zA-Z0-9!#$%&'*+\\-.^_`|~]+)=([[:print:]]+)$")

func NewTagFromCLI(compoundTag string) (tunnelpogs.Tag, bool) {
	matches := tagRegexp.FindStringSubmatch(compoundTag)
	if len(matches) == 0 {
		return tunnelpogs.Tag{}, false
	}
	return tunnelpogs.Tag{Name: matches[1], Value: matches[2]}, true
}

func NewTagSliceFromCLI(tags []string) ([]tunnelpogs.Tag, error) {
	var tagSlice []tunnelpogs.Tag
	for _, compoundTag := range tags {
		if tag, ok := NewTagFromCLI(compoundTag); ok {
			tagSlice = append(tagSlice, tag)
		} else {
			return nil, fmt.Errorf("Cannot parse tag value %s", compoundTag)
		}
	}
	return tagSlice, nil
}
