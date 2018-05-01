package pogs

import (
	"encoding/hex"
	"math/rand"
	"testing"

	"zombiezen.com/go/capnproto2"
	air "zombiezen.com/go/capnproto2/internal/aircraftlib"
)

type A struct {
	Name     string
	BirthDay int64
	Phone    string
	Siblings int32
	Spouse   bool
	Money    float64
}

func generateA(r *rand.Rand) *A {
	return &A{
		Name:     randString(r, 16),
		BirthDay: r.Int63(),
		Phone:    randString(r, 10),
		Siblings: r.Int31n(5),
		Spouse:   r.Intn(2) == 1,
		Money:    r.Float64(),
	}
}

func BenchmarkExtract(b *testing.B) {
	r := rand.New(rand.NewSource(12345))
	data := make([][]byte, 1000)
	for i := range data {
		a := generateA(r)
		msg, seg, _ := capnp.NewMessage(capnp.SingleSegment(nil))
		root, _ := air.NewRootBenchmarkA(seg)
		Insert(air.BenchmarkA_TypeID, root.Struct, a)
		data[i], _ = msg.Marshal()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msg, _ := capnp.Unmarshal(data[r.Intn(len(data))])
		root, _ := msg.RootPtr()
		var a A
		Extract(&a, air.BenchmarkA_TypeID, root.Struct())
	}
}

func BenchmarkInsert(b *testing.B) {
	r := rand.New(rand.NewSource(12345))
	data := make([]*A, 1000)
	for i := range data {
		data[i] = generateA(r)
	}
	arena := make([]byte, 0, 512)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a := data[r.Intn(len(data))]
		msg, seg, _ := capnp.NewMessage(capnp.SingleSegment(arena[:0]))
		root, _ := air.NewRootBenchmarkA(seg)
		Insert(air.BenchmarkA_TypeID, root.Struct, a)
		msg.Marshal()
	}
}

func randString(r *rand.Rand, n int) string {
	b := make([]byte, (n+1)/2)
	// Go 1.6 adds a Rand.Read method, but since we want to be compatible with Go 1.4...
	for i := range b {
		b[i] = byte(r.Intn(255))
	}
	return hex.EncodeToString(b)[:n]
}
