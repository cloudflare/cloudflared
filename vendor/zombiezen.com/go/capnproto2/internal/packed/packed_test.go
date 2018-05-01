package packed

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/hex"
	"io"
	"io/ioutil"
	"math"
	"strings"
	"testing"
	"testing/iotest"
)

type testCase struct {
	name       string
	original   []byte
	compressed []byte
}

var compressionTests = []testCase{
	{
		"empty",
		[]byte{},
		[]byte{},
	},
	{
		"one zero word",
		[]byte{0, 0, 0, 0, 0, 0, 0, 0},
		[]byte{0, 0},
	},
	{
		"one word with mixed zero bytes",
		[]byte{0, 0, 12, 0, 0, 34, 0, 0},
		[]byte{0x24, 12, 34},
	},
	{
		"two words with mixed zero bytes",
		[]byte{
			0x08, 0x00, 0x00, 0x00, 0x03, 0x00, 0x02, 0x00,
			0x19, 0x00, 0x00, 0x00, 0xaa, 0x01, 0x00, 0x00,
		},
		[]byte{0x51, 0x08, 0x03, 0x02, 0x31, 0x19, 0xaa, 0x01},
	},
	{
		"two words with mixed zero bytes",
		[]byte{0x8, 0, 0, 0, 0x3, 0, 0x2, 0, 0x19, 0, 0, 0, 0xaa, 0x1, 0, 0},
		[]byte{0x51, 0x08, 0x03, 0x02, 0x31, 0x19, 0xaa, 0x01},
	},
	{
		"four zero words",
		[]byte{
			0, 0, 0, 0, 0, 0, 0, 0,
			0, 0, 0, 0, 0, 0, 0, 0,
			0, 0, 0, 0, 0, 0, 0, 0,
			0, 0, 0, 0, 0, 0, 0, 0,
		},
		[]byte{0x00, 0x03},
	},
	{
		"four words without zero bytes",
		[]byte{
			0x8a, 0x8a, 0x8a, 0x8a, 0x8a, 0x8a, 0x8a, 0x8a,
			0x8a, 0x8a, 0x8a, 0x8a, 0x8a, 0x8a, 0x8a, 0x8a,
			0x8a, 0x8a, 0x8a, 0x8a, 0x8a, 0x8a, 0x8a, 0x8a,
			0x8a, 0x8a, 0x8a, 0x8a, 0x8a, 0x8a, 0x8a, 0x8a,
		},
		[]byte{
			0xff,
			0x8a, 0x8a, 0x8a, 0x8a, 0x8a, 0x8a, 0x8a, 0x8a,
			0x03,
			0x8a, 0x8a, 0x8a, 0x8a, 0x8a, 0x8a, 0x8a, 0x8a,
			0x8a, 0x8a, 0x8a, 0x8a, 0x8a, 0x8a, 0x8a, 0x8a,
			0x8a, 0x8a, 0x8a, 0x8a, 0x8a, 0x8a, 0x8a, 0x8a,
		},
	},
	{
		"one word without zero bytes",
		[]byte{1, 3, 2, 4, 5, 7, 6, 8},
		[]byte{0xff, 1, 3, 2, 4, 5, 7, 6, 8, 0},
	},
	{
		"one zero word followed by one word without zero bytes",
		[]byte{0, 0, 0, 0, 0, 0, 0, 0, 1, 3, 2, 4, 5, 7, 6, 8},
		[]byte{0, 0, 0xff, 1, 3, 2, 4, 5, 7, 6, 8, 0},
	},
	{
		"one word with mixed zero bytes followed by one word without zero bytes",
		[]byte{0, 0, 12, 0, 0, 34, 0, 0, 1, 3, 2, 4, 5, 7, 6, 8},
		[]byte{0x24, 12, 34, 0xff, 1, 3, 2, 4, 5, 7, 6, 8, 0},
	},
	{
		"two words with no zero bytes",
		[]byte{1, 3, 2, 4, 5, 7, 6, 8, 8, 6, 7, 4, 5, 2, 3, 1},
		[]byte{0xff, 1, 3, 2, 4, 5, 7, 6, 8, 1, 8, 6, 7, 4, 5, 2, 3, 1},
	},
	{
		"five words, with only the last containing zero bytes",
		[]byte{
			1, 2, 3, 4, 5, 6, 7, 8,
			1, 2, 3, 4, 5, 6, 7, 8,
			1, 2, 3, 4, 5, 6, 7, 8,
			1, 2, 3, 4, 5, 6, 7, 8,
			0, 2, 4, 0, 9, 0, 5, 1,
		},
		[]byte{
			0xff, 1, 2, 3, 4, 5, 6, 7, 8,
			3,
			1, 2, 3, 4, 5, 6, 7, 8,
			1, 2, 3, 4, 5, 6, 7, 8,
			1, 2, 3, 4, 5, 6, 7, 8,
			0xd6, 2, 4, 9, 5, 1,
		},
	},
	{
		"five words, with the middle and last words containing zero bytes",
		[]byte{
			1, 2, 3, 4, 5, 6, 7, 8,
			1, 2, 3, 4, 5, 6, 7, 8,
			6, 2, 4, 3, 9, 0, 5, 1,
			1, 2, 3, 4, 5, 6, 7, 8,
			0, 2, 4, 0, 9, 0, 5, 1,
		},
		[]byte{
			0xff, 1, 2, 3, 4, 5, 6, 7, 8,
			3,
			1, 2, 3, 4, 5, 6, 7, 8,
			6, 2, 4, 3, 9, 0, 5, 1,
			1, 2, 3, 4, 5, 6, 7, 8,
			0xd6, 2, 4, 9, 5, 1,
		},
	},
	{
		"words with mixed zeroes sandwiching zero words",
		[]byte{
			8, 0, 100, 6, 0, 1, 1, 2,
			0, 0, 0, 0, 0, 0, 0, 0,
			0, 0, 0, 0, 0, 0, 0, 0,
			0, 0, 0, 0, 0, 0, 0, 0,
			0, 0, 1, 0, 2, 0, 3, 1,
		},
		[]byte{
			0xed, 8, 100, 6, 1, 1, 2,
			0, 2,
			0xd4, 1, 2, 3, 1,
		},
	},
	{
		"real-world Cap'n Proto data",
		[]byte{
			0x0, 0x0, 0x0, 0x0, 0x5, 0x0, 0x0, 0x0,
			0x0, 0x0, 0x0, 0x0, 0x2, 0x0, 0x1, 0x0,
			0x25, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0,
			0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0,
			0x1, 0x0, 0x0, 0x0, 0xc, 0x0, 0x0, 0x0,
			0xd4, 0x7, 0xc, 0x7, 0x0, 0x0, 0x0, 0x0,
		},
		[]byte{
			0x10, 0x5,
			0x50, 0x2, 0x1,
			0x1, 0x25,
			0x0, 0x0,
			0x11, 0x1, 0xc,
			0xf, 0xd4, 0x7, 0xc, 0x7,
		},
	},
	{
		"shortened benchmark data",
		[]byte{
			8, 100, 6, 0, 1, 1, 0, 2,
			8, 100, 6, 0, 1, 1, 0, 2,
			0, 0, 0, 0, 0, 0, 0, 0,
			0, 0, 0, 0, 0, 0, 0, 0,
			0, 0, 0, 0, 0, 0, 0, 0,
			0, 0, 0, 0, 0, 0, 0, 0,
			0, 1, 0, 2, 0, 3, 0, 0,
			'H', 'e', 'l', 'l', 'o', ',', ' ', 'W',
			'o', 'r', 'l', 'd', '!', ' ', ' ', 'P',
			'a', 'd', ' ', 't', 'e', 'x', 't', '.',
		},
		[]byte{
			0xb7, 8, 100, 6, 1, 1, 2,
			0xb7, 8, 100, 6, 1, 1, 2,
			0x00, 3,
			0x2a, 1, 2, 3,
			0xff, 'H', 'e', 'l', 'l', 'o', ',', ' ', 'W',
			2,
			'o', 'r', 'l', 'd', '!', ' ', ' ', 'P',
			'a', 'd', ' ', 't', 'e', 'x', 't', '.',
		},
	},
}

var decompressionTests = []testCase{
	{
		"fuzz hang #1",
		mustGunzip("\x1f\x8b\b\x00\x00\tn\x88\x00\xff\xec\xce!\x11\x000\f\x04\xc1G\xd5Q\xff\x02\x8b" +
			"\xab!(\xc9\xcc.>p\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x80\xf5^" +
			"\xf7\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00" +
			"\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x000\xc8\xc9" +
			"-\xf5?\x00\x00\xff\xff6\xe2l*\x90\xcc\x00\x00"),
		[]byte("\x00\xff\x00\xf6\x00\xff\x00\xf6\x00\xff\x00\xf6\x00\xff@\xf6\x00\xff\x00\xf6" +
			"\x00\xff\x00\xf6\x00\xff\x00\xf6\x00\xff\x00\xf6\x00\xff\x00\xf6\x00\xff\x00\xf6" +
			"\x00\xff\x00\xf6\x00\xf6\x00\xff\x00\xf6\x00\xff\x00\xf6\x05\x06 \x00\x04"),
	},
	{
		"fuzz hang #2",
		mustGunzip("\x1f\x8b\b\x00\x00\tn\x88\x00\xff\xec\xceA\r\x00\x00\b\x04\xa0\xeb\x1fد\xc6p:H@" +
			"\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00" +
			"\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00ު\xa4\xb7\x0f\x00\x00\x00\x00\x00\x00\x00" +
			"\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00" +
			"\\5\x01\x00\x00\xff\xff\r\xfb\xbac\xe0\xe8\x00\x00"),
		[]byte("\x00\xf6\x00\xff\x00\u007f\x00\xf6\x00\xff\x00\xf6\x00\xff\x00\xf6\x00\xff\x00\xf6" +
			"\x00\xff\x00\xf6\x00\xff\x00\xf6\x00\xff\x00\xf6\x00\x005\x00\xf6\x00\xff\x00" +
			"\xf6\x00\xff\x00\xf6\x00\xff\x00 \x00\xff\x00\xf6\x00\xff\x00\xf6\x00\xff\x00" +
			"\xf6\x00\xff\x00\xf6\x00\xff\x00\xf6"),
	},
}

var badDecompressionTests = []struct {
	name  string
	input []byte
}{
	{
		"wrong tag",
		[]byte{
			0xa7, 8, 100, 6, 1, 1, 2,
			0xa7, 8, 100, 6, 1, 1, 2,
		},
	},
	{
		"badly written decompression benchmark",
		bytes.Repeat([]byte{
			0xa7, 8, 100, 6, 1, 1, 2,
			0xa7, 8, 100, 6, 1, 1, 2,
			0x00, 3,
			0x2a,
			0xff, 'H', 'e', 'l', 'l', 'o', ',', ' ', 'W',
			2,
			'o', 'r', 'l', 'd', '!', ' ', ' ', 'P',
			'a', 'd', ' ', 't', 'e', 'x', 't', '.',
		}, 128),
	},
}

func TestPack(t *testing.T) {
	for _, test := range compressionTests {
		orig := make([]byte, len(test.original))
		copy(orig, test.original)
		compressed := Pack(nil, orig)
		if !bytes.Equal(compressed, test.compressed) {
			t.Errorf("%s: Pack(nil,\n%s\n) =\n%s\n; want\n%s", test.name, hex.Dump(test.original), hex.Dump(compressed), hex.Dump(test.compressed))
		}
	}
}

func TestUnpack(t *testing.T) {
	tests := make([]testCase, 0, len(compressionTests)+len(decompressionTests))
	tests = append(tests, compressionTests...)
	tests = append(tests, decompressionTests...)
	for _, test := range tests {
		compressed := make([]byte, len(test.compressed))
		copy(compressed, test.compressed)
		orig, err := Unpack(nil, compressed)
		if err != nil {
			t.Errorf("%s: Unpack(nil,\n%s\n) error: %v", test.name, hex.Dump(test.compressed), err)
		} else if !bytes.Equal(orig, test.original) {
			t.Errorf("%s: Unpack(nil,\n%s\n) =\n%s\n; want\n%s", test.name, hex.Dump(test.compressed), hex.Dump(orig), hex.Dump(test.original))
		}
	}
}

func TestUnpack_Fail(t *testing.T) {
	for _, test := range badDecompressionTests {
		compressed := make([]byte, len(test.input))
		copy(compressed, test.input)
		_, err := Unpack(nil, compressed)
		if err == nil {
			t.Errorf("%s: did not return error", test.name)
		}
	}
}

func TestReader(t *testing.T) {
	tests := make([]testCase, 0, len(compressionTests)+len(decompressionTests))
	tests = append(tests, compressionTests...)
	tests = append(tests, decompressionTests...)
testing:
	for _, test := range tests {
		for readSize := 1; readSize <= 8+2*len(test.original); readSize = nextPrime(readSize) {
			r := bytes.NewReader(test.compressed)
			d := NewReader(bufio.NewReader(r))
			buf := make([]byte, readSize)
			var actual []byte
			for {
				n, err := d.Read(buf)
				actual = append(actual, buf[:n]...)
				if err != nil {
					if err == io.EOF {
						break
					}
					t.Errorf("%s: Read: %v", test.name, err)
					continue testing
				}
			}

			if len(test.original) != len(actual) {
				t.Errorf("%s: readSize=%d: expected %d bytes, got %d", test.name, readSize, len(test.original), len(actual))
				continue
			}

			if !bytes.Equal(test.original, actual) {
				t.Errorf("%s: readSize=%d: bytes = %v; want %v", test.name, readSize, actual, test.original)
			}
		}
	}
}

func TestReader_DataErr(t *testing.T) {
	const readSize = 3
	tests := make([]testCase, 0, len(compressionTests)+len(decompressionTests))
	tests = append(tests, compressionTests...)
	tests = append(tests, decompressionTests...)
testing:
	for _, test := range tests {
		r := iotest.DataErrReader(bytes.NewReader(test.compressed))
		d := NewReader(bufio.NewReader(r))
		buf := make([]byte, readSize)
		var actual []byte
		for {
			n, err := d.Read(buf)
			actual = append(actual, buf[:n]...)
			if err != nil {
				if err == io.EOF {
					break
				}
				t.Errorf("%s: Read: %v", test.name, err)
				continue testing
			}
		}

		if len(test.original) != len(actual) {
			t.Errorf("%s: expected %d bytes, got %d", test.name, len(test.original), len(actual))
			continue
		}

		if !bytes.Equal(test.original, actual) {
			t.Errorf("%s: bytes not equal", test.name)
		}
	}
}

func TestReader_Fail(t *testing.T) {
	for _, test := range badDecompressionTests {
		d := NewReader(bufio.NewReader(bytes.NewReader(test.input)))
		_, err := ioutil.ReadAll(d)
		if err == nil {
			t.Errorf("%s: did not return error", test.name)
		}
	}
}

var result []byte

func BenchmarkPack(b *testing.B) {
	src := bytes.Repeat([]byte{
		8, 0, 100, 6, 0, 1, 1, 2,
		8, 0, 100, 6, 0, 1, 1, 2,
		0, 0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0,
		0, 1, 0, 2, 0, 3, 0, 0,
		'H', 'e', 'l', 'l', 'o', ',', ' ', 'W',
		'o', 'r', 'l', 'd', '!', ' ', ' ', 'P',
		'a', 'd', ' ', 't', 'e', 'x', 't', '.',
	}, 128)
	dst := make([]byte, 0, len(src))
	b.SetBytes(int64(len(src)))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dst = Pack(dst[:0], src)
	}
	result = dst
}

func benchUnpack(b *testing.B, src []byte) {
	var unpackedSize int
	{
		tmp, err := Unpack(nil, src)
		if err != nil {
			b.Fatal(err)
		}
		unpackedSize = len(tmp)
	}
	b.SetBytes(int64(unpackedSize))
	dst := make([]byte, 0, unpackedSize)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var err error
		dst, err = Unpack(dst[:0], src)
		if err != nil {
			b.Fatal(err)
		}
	}
	result = dst
}

func BenchmarkUnpack(b *testing.B) {
	benchUnpack(b, bytes.Repeat([]byte{
		0xb7, 8, 100, 6, 1, 1, 2,
		0xb7, 8, 100, 6, 1, 1, 2,
		0x00, 3,
		0x2a, 1, 2, 3,
		0xff, 'H', 'e', 'l', 'l', 'o', ',', ' ', 'W',
		2,
		'o', 'r', 'l', 'd', '!', ' ', ' ', 'P',
		'a', 'd', ' ', 't', 'e', 'x', 't', '.',
	}, 128))
}

func BenchmarkUnpack_Large(b *testing.B) {
	benchUnpack(b, []byte("\x00\xff\x00\xf6\x00\xff\x00\xf6\x00\xff\x00\xf6\x00\xff@\xf6\x00\xff\x00\xf6"+
		"\x00\xff\x00\xf6\x00\xff\x00\xf6\x00\xff\x00\xf6\x00\xff\x00\xf6\x00\xff\x00\xf6"+
		"\x00\xff\x00\xf6\x00\xf6\x00\xff\x00\xf6\x00\xff\x00\xf6\x05\x06 \x00\x04"))
}

func benchReader(b *testing.B, src []byte) {
	var unpackedSize int
	{
		tmp, err := Unpack(nil, src)
		if err != nil {
			b.Fatal(err)
		}
		unpackedSize = len(tmp)
	}
	b.SetBytes(int64(unpackedSize))
	r := bytes.NewReader(src)
	br := bufio.NewReader(r)

	dst := bytes.NewBuffer(make([]byte, 0, unpackedSize))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dst.Reset()
		r.Seek(0, 0)
		br.Reset(r)
		pr := NewReader(br)
		_, err := dst.ReadFrom(pr)
		if err != nil {
			b.Fatal(err)
		}
	}
	result = dst.Bytes()
}

func BenchmarkReader(b *testing.B) {
	benchReader(b, bytes.Repeat([]byte{
		0xb7, 8, 100, 6, 1, 1, 2,
		0xb7, 8, 100, 6, 1, 1, 2,
		0x00, 3,
		0x2a, 1, 2, 3,
		0xff, 'H', 'e', 'l', 'l', 'o', ',', ' ', 'W',
		2,
		'o', 'r', 'l', 'd', '!', ' ', ' ', 'P',
		'a', 'd', ' ', 't', 'e', 'x', 't', '.',
	}, 128))
}

func BenchmarkReader_Large(b *testing.B) {
	benchReader(b, []byte("\x00\xff\x00\xf6\x00\xff\x00\xf6\x00\xff\x00\xf6\x00\xff@\xf6\x00\xff\x00\xf6"+
		"\x00\xff\x00\xf6\x00\xff\x00\xf6\x00\xff\x00\xf6\x00\xff\x00\xf6\x00\xff\x00\xf6"+
		"\x00\xff\x00\xf6\x00\xf6\x00\xff\x00\xf6\x00\xff\x00\xf6\x05\x06 \x00\x04"))
}

func nextPrime(n int) int {
inc:
	for {
		n++
		root := int(math.Sqrt(float64(n)))
		for f := 2; f <= root; f++ {
			if n%f == 0 {
				continue inc
			}
		}
		return n
	}
}

func mustGunzip(s string) []byte {
	r, err := gzip.NewReader(strings.NewReader(s))
	if err != nil {
		panic(err)
	}
	data, err := ioutil.ReadAll(r)
	if err != nil {
		panic(err)
	}
	return data
}
