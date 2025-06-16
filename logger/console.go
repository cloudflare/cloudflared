package logger

import (
	"bytes"
	"fmt"
	"io"

	jsoniter "github.com/json-iterator/go"
)

var json = jsoniter.ConfigCompatibleWithStandardLibrary

// consoleWriter allows us the simplicity to prevent duplicate json keys in the logger events reported.
//
// By default zerolog constructs the json event in parts by appending each additional key after the first. It
// doesn't have any internal state or struct of the json message representation so duplicate keys can be
// inserted without notice and no pruning will occur before writing the log event out to the io.Writer.
//
// To help prevent these duplicate keys, we will decode the json log event and then immediately re-encode it
// again as writing it to the output io.Writer. Since we encode it to a map[string]any, duplicate keys
// are pruned. We pay the cost of decoding and encoding the log event for each time, but helps prevent
// us from needing to worry about adding duplicate keys in the log event from different areas of code.
type consoleWriter struct {
	out io.Writer
}

func (c *consoleWriter) Write(p []byte) (n int, err error) {
	var evt map[string]any
	d := json.NewDecoder(bytes.NewReader(p))
	d.UseNumber()
	err = d.Decode(&evt)
	if err != nil {
		return n, fmt.Errorf("cannot decode event: %s", err)
	}
	e := json.NewEncoder(c.out)
	return len(p), e.Encode(evt)
}
