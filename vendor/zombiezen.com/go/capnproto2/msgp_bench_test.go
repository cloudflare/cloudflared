// +build msgpbench
//go:generate msgp -tests=false -o msgp_bench_gen_test.go
//msgp:Tuple Event

package capnp_test

import (
	"bytes"
	"io"
	"math/rand"
	"testing"
	"time"

	"github.com/tinylib/msgp/msgp"
)

type Event struct {
	Name     string
	BirthDay time.Time
	Phone    string
	Siblings int
	Spouse   bool
	Money    float64
}

func BenchmarkUnmarshalMsgp(b *testing.B) {
	r := rand.New(rand.NewSource(12345))
	data := make([][]byte, 1000)
	for i := range data {
		msg, _ := (*Event)(generateA(r)).MarshalMsg(nil)
		data[i] = msg
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		var e Event
		msg := data[r.Intn(len(data))]
		_, err := e.UnmarshalMsg(msg)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalMsgpReader(b *testing.B) {
	var buf bytes.Buffer

	r := rand.New(rand.NewSource(12345))
	w := msgp.NewWriter(&buf)
	count := 10000

	for i := 0; i < count; i++ {
		event := (*Event)(generateA(r))
		err := event.EncodeMsg(w)
		if err != nil {
			b.Fatal(err)
		}
	}

	w.Flush()
	blob := buf.Bytes()

	b.ReportAllocs()
	b.SetBytes(int64(buf.Len()))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		r := msgp.NewReader(bytes.NewReader(blob))

		for {
			var e Event
			err := e.DecodeMsg(r)

			if err == io.EOF {
				break
			}

			if err != nil {
				b.Fatal(err)
			}
		}
	}
}
