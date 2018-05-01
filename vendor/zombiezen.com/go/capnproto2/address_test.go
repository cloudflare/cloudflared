package capnp

import (
	"testing"
)

func TestAddressElement(t *testing.T) {
	tests := []struct {
		a   Address
		i   int32
		sz  Size
		out Address
		ok  bool
	}{
		{0, 0, 0, 0, true},
		{0, 1, 0, 0, true},
		{0, 1, 8, 8, true},
		{0, 2, 8, 16, true},
		{24, 1, 0, 24, true},
		{24, 1, 8, 32, true},
		{24, 2, 8, 40, true},
		{0, 0x7fffffff, 3, 0, false},
		{0xffffffff, 0x7fffffff, 0xffffffff, 0, false},
	}
	for _, test := range tests {
		out, ok := test.a.element(test.i, test.sz)
		if ok != test.ok || (ok && out != test.out) {
			t.Errorf("%#v.element(%d, %d) = %#v, %t; want %#v, %t", test.a, test.i, test.sz, out, ok, test.out, test.ok)
		}
	}
}
