// Package text supports marshaling Cap'n Proto messages as text based on a schema.
package text

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"strconv"

	"zombiezen.com/go/capnproto2"
	"zombiezen.com/go/capnproto2/internal/nodemap"
	"zombiezen.com/go/capnproto2/internal/schema"
	"zombiezen.com/go/capnproto2/internal/strquote"
	"zombiezen.com/go/capnproto2/schemas"
)

// Marker strings.
const (
	voidMarker       = "void"
	interfaceMarker  = "<external capability>"
	anyPointerMarker = "<opaque pointer>"
)

// Marshal returns the text representation of a struct.
func Marshal(typeID uint64, s capnp.Struct) (string, error) {
	buf := new(bytes.Buffer)
	if err := NewEncoder(buf).Encode(typeID, s); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// MarshalList returns the text representation of a struct list.
func MarshalList(typeID uint64, l capnp.List) (string, error) {
	buf := new(bytes.Buffer)
	if err := NewEncoder(buf).EncodeList(typeID, l); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// An Encoder writes the text format of Cap'n Proto messages to an output stream.
type Encoder struct {
	w     indentWriter
	tmp   []byte
	nodes nodemap.Map
}

// NewEncoder returns a new encoder that writes to w.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: indentWriter{w: w}}
}

// UseRegistry changes the registry that the encoder consults for
// schemas from the default registry.
func (enc *Encoder) UseRegistry(reg *schemas.Registry) {
	enc.nodes.UseRegistry(reg)
}

// SetIndent sets string to indent each level with.
// An empty string disables indentation.
func (enc *Encoder) SetIndent(indent string) {
	enc.w.indentPerLevel = indent
}

// Encode writes the text representation of s to the stream.
func (enc *Encoder) Encode(typeID uint64, s capnp.Struct) error {
	if enc.w.err != nil {
		return enc.w.err
	}
	err := enc.marshalStruct(typeID, s)
	if err != nil {
		return err
	}
	return enc.w.err
}

// EncodeList writes the text representation of struct list l to the stream.
func (enc *Encoder) EncodeList(typeID uint64, l capnp.List) error {
	_, seg, _ := capnp.NewMessage(capnp.SingleSegment(nil))
	typ, _ := schema.NewRootType(seg)
	typ.SetStructType()
	typ.StructType().SetTypeId(typeID)
	return enc.marshalList(typ, l)
}

func (enc *Encoder) marshalBool(v bool) {
	if v {
		enc.w.WriteString("true")
	} else {
		enc.w.WriteString("false")
	}
}

func (enc *Encoder) marshalInt(i int64) {
	enc.tmp = strconv.AppendInt(enc.tmp[:0], i, 10)
	enc.w.Write(enc.tmp)
}

func (enc *Encoder) marshalUint(i uint64) {
	enc.tmp = strconv.AppendUint(enc.tmp[:0], i, 10)
	enc.w.Write(enc.tmp)
}

func (enc *Encoder) marshalFloat32(f float32) {
	enc.tmp = strconv.AppendFloat(enc.tmp[:0], float64(f), 'g', -1, 32)
	enc.w.Write(enc.tmp)
}

func (enc *Encoder) marshalFloat64(f float64) {
	enc.tmp = strconv.AppendFloat(enc.tmp[:0], f, 'g', -1, 64)
	enc.w.Write(enc.tmp)
}

func (enc *Encoder) marshalText(t []byte) {
	enc.tmp = strquote.Append(enc.tmp[:0], t)
	enc.w.Write(enc.tmp)
}

func needsEscape(b byte) bool {
	return b < 0x20 || b >= 0x7f
}

func hexDigit(b byte) byte {
	const digits = "0123456789abcdef"
	return digits[b]
}

func (enc *Encoder) marshalStruct(typeID uint64, s capnp.Struct) error {
	n, err := enc.nodes.Find(typeID)
	if err != nil {
		return err
	}
	if !n.IsValid() || n.Which() != schema.Node_Which_structNode {
		return fmt.Errorf("cannot find struct type %#x", typeID)
	}
	var discriminant uint16
	if n.StructNode().DiscriminantCount() > 0 {
		discriminant = s.Uint16(capnp.DataOffset(n.StructNode().DiscriminantOffset() * 2))
	}
	fields := codeOrderFields(n.StructNode())
	if len(fields) == 0 {
		enc.w.WriteString("()")
		return nil
	}
	enc.w.WriteByte('(')
	enc.w.Indent()
	enc.w.NewLine()
	first := true
	for _, f := range fields {
		if !(f.Which() == schema.Field_Which_slot || f.Which() == schema.Field_Which_group) {
			continue
		}
		if dv := f.DiscriminantValue(); !(dv == schema.Field_noDiscriminant || dv == discriminant) {
			continue
		}
		if !first {
			enc.w.WriteByte(',')
			enc.w.NewLineOrSpace()
		}
		first = false
		name, err := f.NameBytes()
		if err != nil {
			return err
		}
		enc.w.Write(name)
		enc.w.WriteString(" = ")
		switch f.Which() {
		case schema.Field_Which_slot:
			if err := enc.marshalFieldValue(s, f); err != nil {
				return err
			}
		case schema.Field_Which_group:
			if err := enc.marshalStruct(f.Group().TypeId(), s); err != nil {
				return err
			}
		}
	}
	enc.w.NewLine()
	enc.w.Unindent()
	enc.w.WriteByte(')')
	return nil
}

func (enc *Encoder) marshalFieldValue(s capnp.Struct, f schema.Field) error {
	typ, err := f.Slot().Type()
	if err != nil {
		return err
	}
	dv, err := f.Slot().DefaultValue()
	if err != nil {
		return err
	}
	if dv.IsValid() && int(typ.Which()) != int(dv.Which()) {
		name, _ := f.Name()
		return fmt.Errorf("marshal field %s: default value is a %v, want %v", name, dv.Which(), typ.Which())
	}
	switch typ.Which() {
	case schema.Type_Which_void:
		enc.w.WriteString(voidMarker)
	case schema.Type_Which_bool:
		v := s.Bit(capnp.BitOffset(f.Slot().Offset()))
		d := dv.Bool()
		enc.marshalBool(!d && v || d && !v)
	case schema.Type_Which_int8:
		v := s.Uint8(capnp.DataOffset(f.Slot().Offset()))
		d := uint8(dv.Int8())
		enc.marshalInt(int64(int8(v ^ d)))
	case schema.Type_Which_int16:
		v := s.Uint16(capnp.DataOffset(f.Slot().Offset() * 2))
		d := uint16(dv.Int16())
		enc.marshalInt(int64(int16(v ^ d)))
	case schema.Type_Which_int32:
		v := s.Uint32(capnp.DataOffset(f.Slot().Offset() * 4))
		d := uint32(dv.Int32())
		enc.marshalInt(int64(int32(v ^ d)))
	case schema.Type_Which_int64:
		v := s.Uint64(capnp.DataOffset(f.Slot().Offset() * 8))
		d := uint64(dv.Int64())
		enc.marshalInt(int64(v ^ d))
	case schema.Type_Which_uint8:
		v := s.Uint8(capnp.DataOffset(f.Slot().Offset()))
		d := dv.Uint8()
		enc.marshalUint(uint64(v ^ d))
	case schema.Type_Which_uint16:
		v := s.Uint16(capnp.DataOffset(f.Slot().Offset() * 2))
		d := dv.Uint16()
		enc.marshalUint(uint64(v ^ d))
	case schema.Type_Which_uint32:
		v := s.Uint32(capnp.DataOffset(f.Slot().Offset() * 4))
		d := dv.Uint32()
		enc.marshalUint(uint64(v ^ d))
	case schema.Type_Which_uint64:
		v := s.Uint64(capnp.DataOffset(f.Slot().Offset() * 8))
		d := dv.Uint64()
		enc.marshalUint(v ^ d)
	case schema.Type_Which_float32:
		v := s.Uint32(capnp.DataOffset(f.Slot().Offset() * 4))
		d := math.Float32bits(dv.Float32())
		enc.marshalFloat32(math.Float32frombits(v ^ d))
	case schema.Type_Which_float64:
		v := s.Uint64(capnp.DataOffset(f.Slot().Offset() * 8))
		d := math.Float64bits(dv.Float64())
		enc.marshalFloat64(math.Float64frombits(v ^ d))
	case schema.Type_Which_structType:
		p, err := s.Ptr(uint16(f.Slot().Offset()))
		if err != nil {
			return err
		}
		if !p.IsValid() {
			p, _ = dv.StructValuePtr()
		}
		return enc.marshalStruct(typ.StructType().TypeId(), p.Struct())
	case schema.Type_Which_data:
		p, err := s.Ptr(uint16(f.Slot().Offset()))
		if err != nil {
			return err
		}
		if !p.IsValid() {
			b, _ := dv.Data()
			enc.marshalText(b)
			return nil
		}
		enc.marshalText(p.Data())
	case schema.Type_Which_text:
		p, err := s.Ptr(uint16(f.Slot().Offset()))
		if err != nil {
			return err
		}
		if !p.IsValid() {
			b, _ := dv.TextBytes()
			enc.marshalText(b)
			return nil
		}
		enc.marshalText(p.TextBytes())
	case schema.Type_Which_list:
		elem, err := typ.List().ElementType()
		if err != nil {
			return err
		}
		p, err := s.Ptr(uint16(f.Slot().Offset()))
		if err != nil {
			return err
		}
		if !p.IsValid() {
			p, _ = dv.ListPtr()
		}
		return enc.marshalList(elem, p.List())
	case schema.Type_Which_enum:
		v := s.Uint16(capnp.DataOffset(f.Slot().Offset() * 2))
		d := dv.Uint16()
		return enc.marshalEnum(typ.Enum().TypeId(), v^d)
	case schema.Type_Which_interface:
		enc.w.WriteString(interfaceMarker)
	case schema.Type_Which_anyPointer:
		enc.w.WriteString(anyPointerMarker)
	default:
		return fmt.Errorf("unknown field type %v", typ.Which())
	}
	return nil
}

func codeOrderFields(s schema.Node_structNode) []schema.Field {
	list, _ := s.Fields()
	n := list.Len()
	fields := make([]schema.Field, n)
	for i := 0; i < n; i++ {
		f := list.At(i)
		fields[f.CodeOrder()] = f
	}
	return fields
}

func (enc *Encoder) marshalList(elem schema.Type, l capnp.List) error {
	writeListItems := func(writeItem func(i int) error) error {
		if l.Len() == 0 {
			_, err := enc.w.WriteString("[]")
			return err
		}
		enc.w.WriteByte('[')
		enc.w.Indent()
		enc.w.NewLine()
		for i := 0; i < l.Len(); i++ {
			err := writeItem(i)
			if err != nil {
				return err
			}
			if i == l.Len()-1 {
				enc.w.NewLine()
			} else {
				enc.w.WriteByte(',')
				enc.w.NewLineOrSpace()
			}
		}
		enc.w.Unindent()
		enc.w.WriteByte(']')
		return nil
	}
	writeListItemsN := func(writeItem func(i int) (int, error)) error {
		return writeListItems(func(i int) error {
			_, err := writeItem(i)
			return err
		})
	}
	switch elem.Which() {
	case schema.Type_Which_void:
		return writeListItemsN(func(_ int) (int, error) {
			return enc.w.WriteString("void")
		})
	case schema.Type_Which_bool:
		p := capnp.BitList{List: l}
		return writeListItemsN(func(i int) (int, error) {
			if p.At(i) {
				return enc.w.WriteString("true")
			} else {
				return enc.w.WriteString("false")
			}
		})
	case schema.Type_Which_int8:
		p := capnp.Int8List{List: l}
		return writeListItemsN(func(i int) (int, error) {
			return enc.w.WriteString(strconv.FormatInt(int64(p.At(i)), 10))
		})
	case schema.Type_Which_int16:
		p := capnp.Int16List{List: l}
		return writeListItemsN(func(i int) (int, error) {
			return enc.w.WriteString(strconv.FormatInt(int64(p.At(i)), 10))
		})
	case schema.Type_Which_int32:
		p := capnp.Int32List{List: l}
		return writeListItemsN(func(i int) (int, error) {
			return enc.w.WriteString(strconv.FormatInt(int64(p.At(i)), 10))
		})
	case schema.Type_Which_int64:
		p := capnp.Int64List{List: l}
		return writeListItemsN(func(i int) (int, error) {
			return enc.w.WriteString(strconv.FormatInt(p.At(i), 10))
		})
	case schema.Type_Which_uint8:
		p := capnp.UInt8List{List: l}
		return writeListItemsN(func(i int) (int, error) {
			return enc.w.WriteString(strconv.FormatUint(uint64(p.At(i)), 10))
		})
	case schema.Type_Which_uint16:
		p := capnp.UInt16List{List: l}
		return writeListItemsN(func(i int) (int, error) {
			return enc.w.WriteString(strconv.FormatUint(uint64(p.At(i)), 10))
		})
	case schema.Type_Which_uint32:
		p := capnp.UInt32List{List: l}
		return writeListItemsN(func(i int) (int, error) {
			return enc.w.WriteString(strconv.FormatUint(uint64(p.At(i)), 10))
		})
	case schema.Type_Which_uint64:
		p := capnp.UInt64List{List: l}
		return writeListItemsN(func(i int) (int, error) {
			return enc.w.WriteString(strconv.FormatUint(p.At(i), 10))
		})
	case schema.Type_Which_float32:
		p := capnp.Float32List{List: l}
		return writeListItemsN(func(i int) (int, error) {
			return enc.w.WriteString(strconv.FormatFloat(float64(p.At(i)), 'g', -1, 32))
		})
	case schema.Type_Which_float64:
		p := capnp.Float64List{List: l}
		return writeListItemsN(func(i int) (int, error) {
			return enc.w.WriteString(strconv.FormatFloat(p.At(i), 'g', -1, 64))
		})
	case schema.Type_Which_data:
		p := capnp.DataList{List: l}
		return writeListItemsN(func(i int) (int, error) {
			s, err := p.At(i)
			if err != nil {
				return enc.w.WriteString("<error>")
			}
			buf := strquote.Append(nil, s)
			return enc.w.Write(buf)
		})
	case schema.Type_Which_text:
		p := capnp.TextList{List: l}
		return writeListItemsN(func(i int) (int, error) {
			s, err := p.BytesAt(i)
			if err != nil {
				return enc.w.WriteString("<error>")
			}
			buf := strquote.Append(nil, s)
			return enc.w.Write(buf)
		})
	case schema.Type_Which_structType:
		return writeListItems(func(i int) error {
			return enc.marshalStruct(elem.StructType().TypeId(), l.Struct(i))
		})
	case schema.Type_Which_list:
		ee, err := elem.List().ElementType()
		if err != nil {
			return err
		}
		return writeListItems(func(i int) error {
			p, err := capnp.PointerList{List: l}.PtrAt(i)
			if err != nil {
				return err
			}
			return enc.marshalList(ee, p.List())
		})
	case schema.Type_Which_enum:
		il := capnp.UInt16List{List: l}
		typ := elem.Enum().TypeId()
		// TODO(light): only search for node once
		return writeListItems(func(i int) error {
			return enc.marshalEnum(typ, il.At(i))
		})
	case schema.Type_Which_interface:
		return writeListItemsN(func(_ int) (int, error) {
			return enc.w.WriteString(interfaceMarker)
		})
	case schema.Type_Which_anyPointer:
		return writeListItemsN(func(_ int) (int, error) {
			return enc.w.WriteString(anyPointerMarker)
		})
	default:
		return fmt.Errorf("unknown list type %v", elem.Which())
	}
}

func (enc *Encoder) marshalEnum(typ uint64, val uint16) error {
	n, err := enc.nodes.Find(typ)
	if err != nil {
		return err
	}
	if n.Which() != schema.Node_Which_enum {
		return fmt.Errorf("marshaling enum of type @%#x: type is not an enum", typ)
	}
	enums, err := n.Enum().Enumerants()
	if err != nil {
		return err
	}
	if int(val) >= enums.Len() {
		enc.marshalUint(uint64(val))
		return nil
	}
	name, err := enums.At(int(val)).NameBytes()
	if err != nil {
		return err
	}
	enc.w.Write(name)
	return nil
}

// indentWriter is helper for writing indented text
type indentWriter struct {
	w   io.Writer
	err error

	// indentPerLevel is a string to prepend to a line for every level of indentation.
	indentPerLevel string

	// current indent level
	currentIndent int

	// hasLineContent is true when we have written something on the current line.
	hasLineContent bool
}

func (iw *indentWriter) beforeWrite() {
	if iw.err != nil {
		return
	}
	if len(iw.indentPerLevel) > 0 && !iw.hasLineContent {
		iw.hasLineContent = true
		for i := 0; i < iw.currentIndent; i++ {
			_, err := iw.w.Write([]byte(iw.indentPerLevel))
			if err != nil {
				iw.err = err
				return
			}
		}
	}
}

func (iw *indentWriter) Write(p []byte) (int, error) {
	iw.beforeWrite()
	if iw.err != nil {
		return 0, iw.err
	}
	var n int
	n, iw.err = iw.w.Write(p)
	return n, iw.err
}

func (iw *indentWriter) WriteString(s string) (int, error) {
	iw.beforeWrite()
	if iw.err != nil {
		return 0, iw.err
	}
	var n int
	n, iw.err = io.WriteString(iw.w, s)
	return n, iw.err
}

func (iw *indentWriter) WriteByte(b byte) error {
	iw.beforeWrite()
	if iw.err != nil {
		return iw.err
	}
	if bw, ok := iw.w.(io.ByteWriter); ok {
		iw.err = bw.WriteByte(b)
	} else {
		_, iw.err = iw.w.Write([]byte{b})
	}
	return iw.err
}

func (iw *indentWriter) Indent() {
	iw.currentIndent++
}

func (iw *indentWriter) Unindent() {
	iw.currentIndent--
}

func (iw *indentWriter) NewLine() {
	if len(iw.indentPerLevel) > 0 && iw.hasLineContent {
		if iw.err != nil {
			return
		}
		if bw, ok := iw.w.(io.ByteWriter); ok {
			iw.err = bw.WriteByte('\n')
		} else {
			_, iw.err = iw.w.Write([]byte{'\n'})
		}
		iw.hasLineContent = false
	}
}

func (iw *indentWriter) NewLineOrSpace() {
	if len(iw.indentPerLevel) > 0 {
		iw.NewLine()
	} else {
		iw.WriteByte(' ')
	}
}
