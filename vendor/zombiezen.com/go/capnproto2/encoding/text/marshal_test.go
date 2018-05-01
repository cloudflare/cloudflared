package text

import (
	"bytes"
	"io/ioutil"
	"path/filepath"
	"testing"

	"zombiezen.com/go/capnproto2"
	"zombiezen.com/go/capnproto2/internal/schema"
	"zombiezen.com/go/capnproto2/schemas"
)

func readTestFile(name string) ([]byte, error) {
	path := filepath.Join("testdata", name)
	return ioutil.ReadFile(path)
}

func TestEncode(t *testing.T) {
	tests := []struct {
		constID uint64
		text    string
	}{
		{0xc0b634e19e5a9a4e, `(key = "42", value = (int32 = -123))`},
		{0x967c8fe21790b0fb, `(key = "float", value = (float64 = 3.14))`},
		{0xdf35cb2e1f5ea087, `(key = "bool", value = (bool = false))`},
		{0xb167974479102805, `(map = [(key = "foo", value = (void = void)), (key = "bar", value = (void = void))])`},
		{0x81fdbfdc91779421, `(map = [])`},
		{0x8e85252144f61858, `(data = "Hi\xde\xad\xbe\xef\xca\xfe")`},
		{0xc21398a8474837ba, `(voidList = [void, void])`},
		{0xde82c2eeb3a4b07c, `(boolList = [true, false, true, false])`},
		{0xf9e3ffc179272aa2, `(int8List = [1, -2, 3])`},
		{0xfc421b96ec6ad2b6, `(int64List = [1, -2, 3])`},
		{0xb3034b89d02775a5, `(uint8List = [255, 0, 1])`},
		{0x9246c307e46ad03b, `(uint64List = [1, 2, 3])`},
		{0xd012128a1a9cb7fc, `(float32List = [0.5, 3.14, -2])`},
		{0xf16c386c66d492e2, `(textList = ["foo", "bar", "baz"])`},
		{0xe14f4d42aa55de8c, `(dataList = ["\xde\xad\xbe\xef", "\xca\xfe"])`},
		{0xe88c91698f7f0b73, `(cheese = gouda)`},
		{0x9c51b843b337490b, `(cheeseList = [gouda, cheddar])`},
		{0x81e2aadb8bfb237b, `(matrix = [[1, 2, 3], [4, 5, 6]])`},
	}

	data, err := readTestFile("txt.capnp.out")
	if err != nil {
		t.Fatal(err)
	}
	reg := new(schemas.Registry)
	err = reg.Register(&schemas.Schema{
		Bytes: data,
		Nodes: []uint64{
			0x8df8bc5abdc060a6,
			0xd3602730c572a43b,
		},
	})
	if err != nil {
		t.Fatalf("Adding to registry: %v", err)
	}
	msg, err := capnp.Unmarshal(data)
	if err != nil {
		t.Fatal("Unmarshaling txt.capnp.out:", err)
	}
	req, err := schema.ReadRootCodeGeneratorRequest(msg)
	if err != nil {
		t.Fatal("Reading code generator request txt.capnp.out:", err)
	}
	nodes, err := req.Nodes()
	if err != nil {
		t.Fatal(err)
	}
	nodeMap := make(map[uint64]schema.Node, nodes.Len())
	for i := 0; i < nodes.Len(); i++ {
		n := nodes.At(i)
		nodeMap[n.Id()] = n
	}

	for _, test := range tests {
		c := nodeMap[test.constID]
		if !c.IsValid() {
			t.Errorf("Can't find node %#x; skipping", test.constID)
			continue
		}
		dn, _ := c.DisplayName()
		if c.Which() != schema.Node_Which_const {
			t.Errorf("%s @%#x is a %v, not const; skipping", dn, test.constID, c.Which())
			continue
		}

		typ, err := c.Const().Type()
		if err != nil {
			t.Errorf("(%s @%#x).const.type: %v", dn, test.constID, err)
			continue
		}
		if typ.Which() != schema.Type_Which_structType {
			t.Errorf("(%s @%#x).const.type is a %v; want struct", dn, test.constID, typ.Which())
			continue
		}
		tid := typ.StructType().TypeId()

		v, err := c.Const().Value()
		if err != nil {
			t.Errorf("(%s @%#x).const.value: %v", dn, test.constID, err)
			continue
		}
		if v.Which() != schema.Value_Which_structValue {
			t.Errorf("(%s @%#x).const.value is a %v; want struct", dn, test.constID, v.Which())
			continue
		}
		sv, err := v.StructValuePtr()
		if err != nil {
			t.Errorf("(%s @%#x).const.value.struct: %v", dn, test.constID, err)
			continue
		}

		buf := new(bytes.Buffer)
		enc := NewEncoder(buf)
		enc.UseRegistry(reg)
		if err := enc.Encode(tid, sv.Struct()); err != nil {
			t.Errorf("Encode(%#x, (%s @%#x).const.value.struct): %v", tid, dn, test.constID, err)
			continue
		}
		if text := buf.String(); text != test.text {
			t.Errorf("Encode(%#x, (%s @%#x).const.value.struct) = %q; want %q", tid, dn, test.constID, text, test.text)
			continue
		}
	}
}

func TestEncodeList(t *testing.T) {
	tests := []struct {
		constID uint64
		text    string
	}{
		{0x90c9e81e6418df8e, `[(key = "foo", value = (void = void)), (key = "bar", value = (void = void))]`},
	}

	data, err := readTestFile("txt.capnp.out")
	if err != nil {
		t.Fatal(err)
	}
	reg := new(schemas.Registry)
	err = reg.Register(&schemas.Schema{
		Bytes: data,
		Nodes: []uint64{
			0x8df8bc5abdc060a6,
			0xd3602730c572a43b,
		},
	})
	if err != nil {
		t.Fatalf("Adding to registry: %v", err)
	}
	msg, err := capnp.Unmarshal(data)
	if err != nil {
		t.Fatal("Unmarshaling txt.capnp.out:", err)
	}
	req, err := schema.ReadRootCodeGeneratorRequest(msg)
	if err != nil {
		t.Fatal("Reading code generator request txt.capnp.out:", err)
	}
	nodes, err := req.Nodes()
	if err != nil {
		t.Fatal(err)
	}
	nodeMap := make(map[uint64]schema.Node, nodes.Len())
	for i := 0; i < nodes.Len(); i++ {
		n := nodes.At(i)
		nodeMap[n.Id()] = n
	}

	for _, test := range tests {
		c := nodeMap[test.constID]
		if !c.IsValid() {
			t.Errorf("Can't find node %#x; skipping", test.constID)
			continue
		}
		dn, _ := c.DisplayName()
		if c.Which() != schema.Node_Which_const {
			t.Errorf("%s @%#x is a %v, not const; skipping", dn, test.constID, c.Which())
			continue
		}

		typ, err := c.Const().Type()
		if err != nil {
			t.Errorf("(%s @%#x).const.type: %v", dn, test.constID, err)
			continue
		}
		if typ.Which() != schema.Type_Which_list {
			t.Errorf("(%s @%#x).const.type is a %v; want list", dn, test.constID, typ.Which())
			continue
		}
		etyp, err := typ.List().ElementType()
		if err != nil {
			t.Errorf("(%s @%#x).const.type.list.element_type: %v", dn, test.constID, err)
			continue
		}
		if etyp.Which() != schema.Type_Which_structType {
			t.Errorf("(%s @%#x).const.type is a %v; want struct", dn, test.constID, etyp.Which())
			continue
		}
		tid := etyp.StructType().TypeId()

		v, err := c.Const().Value()
		if err != nil {
			t.Errorf("(%s @%#x).const.value: %v", dn, test.constID, err)
			continue
		}
		if v.Which() != schema.Value_Which_list {
			t.Errorf("(%s @%#x).const.value is a %v; want list", dn, test.constID, v.Which())
			continue
		}
		lv, err := v.ListPtr()
		if err != nil {
			t.Errorf("(%s @%#x).const.value.list: %v", dn, test.constID, err)
			continue
		}

		buf := new(bytes.Buffer)
		enc := NewEncoder(buf)
		enc.UseRegistry(reg)
		if err := enc.EncodeList(tid, lv.List()); err != nil {
			t.Errorf("Encode(%#x, (%s @%#x).const.value.list): %v", tid, dn, test.constID, err)
			continue
		}
		if text := buf.String(); text != test.text {
			t.Errorf("Encode(%#x, (%s @%#x).const.value.list) = %q; want %q", tid, dn, test.constID, text, test.text)
			continue
		}
	}
}
