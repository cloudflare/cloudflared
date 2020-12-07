package syntax

import (
	"bytes"
	"fmt"
	"reflect"
	"runtime"
	"sync"
)

func Unmarshal(data []byte, v interface{}) (int, error) {
	// Check for well-formedness.
	// Avoids filling out half a data structure
	// before discovering a JSON syntax error.
	d := decodeState{}
	d.Write(data)
	return d.unmarshal(v)
}

// Unmarshaler is the interface implemented by types that can
// unmarshal a TLS description of themselves.  Note that unlike the
// JSON unmarshaler interface, it is not known a priori how much of
// the input data will be consumed.  So the Unmarshaler must state
// how much of the input data it consumed.
type Unmarshaler interface {
	UnmarshalTLS([]byte) (int, error)
}

type decodeState struct {
	bytes.Buffer
}

func (d *decodeState) unmarshal(v interface{}) (read int, err error) {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(runtime.Error); ok {
				panic(r)
			}
			if s, ok := r.(string); ok {
				panic(s)
			}
			err = r.(error)
		}
	}()

	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return 0, fmt.Errorf("Invalid unmarshal target (non-pointer or nil)")
	}

	read = d.value(rv)
	return read, nil
}

func (e *decodeState) value(v reflect.Value) int {
	return valueDecoder(v)(e, v, fieldOptions{})
}

type decoderFunc func(e *decodeState, v reflect.Value, opts fieldOptions) int

func valueDecoder(v reflect.Value) decoderFunc {
	return typeDecoder(v.Type().Elem())
}

var decoderCache sync.Map // map[reflect.Type]decoderFunc

func typeDecoder(t reflect.Type) decoderFunc {
	if fi, ok := decoderCache.Load(t); ok {
		return fi.(decoderFunc)
	}

	// XXX(RLB): Wait group based support for recursive types omitted

	// Compute the real decoder and replace the indirect func with it.
	f := newTypeDecoder(t)
	decoderCache.Store(t, f)
	return f
}

var (
	unmarshalerType = reflect.TypeOf(new(Unmarshaler)).Elem()
	uint8Type       = reflect.TypeOf(uint8(0))
)

func newTypeDecoder(t reflect.Type) decoderFunc {
	var dec decoderFunc
	if t.Kind() != reflect.Ptr && reflect.PtrTo(t).Implements(unmarshalerType) {
		dec = unmarshalerDecoder
	} else {
		switch t.Kind() {
		case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			dec = uintDecoder
		case reflect.Array:
			dec = newArrayDecoder(t)
		case reflect.Slice:
			dec = newSliceDecoder(t)
		case reflect.Map:
			dec = newMapDecoder(t)
		case reflect.Struct:
			dec = newStructDecoder(t)
		case reflect.Ptr:
			dec = newPointerDecoder(t)
		default:
			panic(fmt.Errorf("Unsupported type (%s)", t))
		}
	}

	if reflect.PtrTo(t).Implements(validatorType) {
		dec = newValidatorDecoder(dec)
	}

	return dec
}

///// Specific decoders below

func omitDecoder(d *decodeState, v reflect.Value, opts fieldOptions) int {
	return 0
}

//////////

func unmarshalerDecoder(d *decodeState, v reflect.Value, opts fieldOptions) int {
	um, ok := v.Interface().(Unmarshaler)
	if !ok {
		panic(fmt.Errorf("Non-Unmarshaler passed to unmarshalerEncoder"))
	}

	read, err := um.UnmarshalTLS(d.Bytes())
	if err != nil {
		panic(err)
	}

	if read > d.Len() {
		panic(fmt.Errorf("Invalid return value from UnmarshalTLS"))
	}

	d.Next(read)
	return read
}

//////////

func newValidatorDecoder(raw decoderFunc) decoderFunc {
	return func(d *decodeState, v reflect.Value, opts fieldOptions) int {
		read := raw(d, v, opts)

		val, ok := v.Interface().(Validator)
		if !ok {
			panic(fmt.Errorf("Non-Validator passed to validatorDecoder"))
		}

		if err := val.ValidForTLS(); err != nil {
			panic(fmt.Errorf("Decoded invalid TLS value: %v", err))
		}

		return read
	}
}

//////////

func uintDecoder(d *decodeState, v reflect.Value, opts fieldOptions) int {
	if opts.varint {
		return varintDecoder(d, v, opts)
	}

	uintLen := int(v.Elem().Type().Size())
	buf := d.Next(uintLen)
	if len(buf) != uintLen {
		panic(fmt.Errorf("Insufficient data to read uint"))
	}

	return setUintFromBuffer(v, buf)
}

func varintDecoder(d *decodeState, v reflect.Value, opts fieldOptions) int {
	l, val := readVarint(d)

	uintLen := int(v.Elem().Type().Size())
	if uintLen < l {
		panic(fmt.Errorf("Uint too small to fit varint: %d < %d", uintLen, l))
	}

	v.Elem().SetUint(val)

	return l
}

func readVarint(d *decodeState) (int, uint64) {
	// Read the first octet and decide the size of the presented varint
	first := d.Next(1)
	if len(first) != 1 {
		panic(fmt.Errorf("Insufficient data to read varint length"))
	}

	twoBits := uint(first[0] >> 6)
	varintLen := 1 << twoBits

	rest := d.Next(varintLen - 1)
	if len(rest) != varintLen-1 {
		panic(fmt.Errorf("Insufficient data to read varint"))
	}

	buf := append(first, rest...)
	buf[0] &= 0x3f

	return len(buf), decodeUintFromBuffer(buf)
}

func decodeUintFromBuffer(buf []byte) uint64 {
	val := uint64(0)
	for _, b := range buf {
		val = (val << 8) + uint64(b)
	}

	return val
}

func setUintFromBuffer(v reflect.Value, buf []byte) int {
	v.Elem().SetUint(decodeUintFromBuffer(buf))
	return len(buf)
}

//////////

type arrayDecoder struct {
	elemDec decoderFunc
}

func (ad *arrayDecoder) decode(d *decodeState, v reflect.Value, opts fieldOptions) int {
	n := v.Elem().Type().Len()
	read := 0
	for i := 0; i < n; i += 1 {
		read += ad.elemDec(d, v.Elem().Index(i).Addr(), opts)
	}
	return read
}

func newArrayDecoder(t reflect.Type) decoderFunc {
	dec := &arrayDecoder{typeDecoder(t.Elem())}
	return dec.decode
}

//////////

func decodeLength(d *decodeState, opts fieldOptions) (int, int) {
	read := 0
	length := 0
	switch {
	case opts.omitHeader:
		read = 0
		length = d.Len()

	case opts.varintHeader:
		var length64 uint64
		read, length64 = readVarint(d)
		length = int(length64)

	case opts.headerSize > 0:
		lengthBytes := d.Next(int(opts.headerSize))
		if len(lengthBytes) != int(opts.headerSize) {
			panic(fmt.Errorf("Not enough data to read header"))
		}
		read = len(lengthBytes)
		length = int(decodeUintFromBuffer(lengthBytes))

	default:
		panic(fmt.Errorf("Cannot decode a slice without a header length"))
	}

	// Check that the length is OK
	if opts.maxSize > 0 && length > opts.maxSize {
		panic(fmt.Errorf("Length of vector exceeds declared max"))
	}
	if length < opts.minSize {
		panic(fmt.Errorf("Length of vector below declared min"))
	}

	return read, length
}

//////////

type sliceDecoder struct {
	elementType reflect.Type
	elementDec  decoderFunc
}

func (sd *sliceDecoder) decode(d *decodeState, v reflect.Value, opts fieldOptions) int {
	// Determine the length of the vector
	read, length := decodeLength(d, opts)

	// Decode elements
	elemData := d.Next(length)
	if len(elemData) != length {
		panic(fmt.Errorf("Not enough data to read elements"))
	}

	// For opaque values, we can return a reference instead of making a new slice
	if v.Elem().Type().Elem() == uint8Type {
		v.Elem().Set(reflect.ValueOf(elemData))
		return read + length
	}

	// For other values, we need to decode the raw data
	elemBuf := &decodeState{}
	elemBuf.Write(elemData)
	elems := []reflect.Value{}
	for elemBuf.Len() > 0 {
		elem := reflect.New(sd.elementType)
		read += sd.elementDec(elemBuf, elem, opts)
		elems = append(elems, elem)
	}

	v.Elem().Set(reflect.MakeSlice(v.Elem().Type(), len(elems), len(elems)))
	for i := 0; i < len(elems); i += 1 {
		v.Elem().Index(i).Set(elems[i].Elem())
	}
	return read
}

func newSliceDecoder(t reflect.Type) decoderFunc {
	dec := &sliceDecoder{
		elementType: t.Elem(),
		elementDec:  typeDecoder(t.Elem()),
	}
	return dec.decode
}

//////////

type mapDecoder struct {
	keyType reflect.Type
	valType reflect.Type
	keyDec  decoderFunc
	valDec  decoderFunc
}

func (md mapDecoder) decode(d *decodeState, v reflect.Value, opts fieldOptions) int {
	// Determine the length of the data
	read, length := decodeLength(d, opts)

	// Decode key/value pairs
	elemData := d.Next(length)
	if len(elemData) != length {
		panic(fmt.Errorf("Not enough data to read elements"))
	}

	mapType := reflect.MapOf(md.keyType, md.valType)
	v.Elem().Set(reflect.MakeMap(mapType))

	nullOpts := fieldOptions{}
	elemBuf := &decodeState{}
	elemBuf.Write(elemData)
	for elemBuf.Len() > 0 {
		key := reflect.New(md.keyType)
		read += md.keyDec(elemBuf, key, nullOpts)

		val := reflect.New(md.valType)
		read += md.valDec(elemBuf, val, nullOpts)

		v.Elem().SetMapIndex(key.Elem(), val.Elem())
	}

	return read
}

func newMapDecoder(t reflect.Type) decoderFunc {
	md := mapDecoder{
		keyType: t.Key(),
		valType: t.Elem(),
		keyDec:  typeDecoder(t.Key()),
		valDec:  typeDecoder(t.Elem()),
	}

	return md.decode
}

//////////

type structDecoder struct {
	fieldOpts []fieldOptions
	fieldDecs []decoderFunc
}

func (sd *structDecoder) decode(d *decodeState, v reflect.Value, opts fieldOptions) int {
	read := 0
	for i := range sd.fieldDecs {
		read += sd.fieldDecs[i](d, v.Elem().Field(i).Addr(), sd.fieldOpts[i])
	}
	return read
}

func newStructDecoder(t reflect.Type) decoderFunc {
	n := t.NumField()
	sd := structDecoder{
		fieldOpts: make([]fieldOptions, n),
		fieldDecs: make([]decoderFunc, n),
	}

	for i := 0; i < n; i += 1 {
		f := t.Field(i)

		tag := f.Tag.Get("tls")
		opts := parseTag(tag)

		if !opts.ValidForType(f.Type) {
			panic(fmt.Errorf("Tags invalid for field type"))
		}

		sd.fieldOpts[i] = opts
		if sd.fieldOpts[i].omit {
			sd.fieldDecs[i] = omitDecoder
		} else {
			sd.fieldDecs[i] = typeDecoder(f.Type)
		}
	}

	return sd.decode
}

//////////

type pointerDecoder struct {
	base decoderFunc
}

func (pd *pointerDecoder) decode(d *decodeState, v reflect.Value, opts fieldOptions) int {
	readBase := 0
	if opts.optional {
		readBase = 1
		flag := d.Next(1)
		switch flag[0] {
		case optionalFlagAbsent:
			indir := v.Elem()
			indir.Set(reflect.Zero(indir.Type()))
			return 1

		case optionalFlagPresent:
			// No action; continue as normal

		default:
			panic(fmt.Errorf("Invalid flag byte for optional: [%x]", flag))
		}
	}

	v.Elem().Set(reflect.New(v.Elem().Type().Elem()))
	return readBase + pd.base(d, v.Elem(), opts)
}

func newPointerDecoder(t reflect.Type) decoderFunc {
	baseDecoder := typeDecoder(t.Elem())
	pd := pointerDecoder{base: baseDecoder}
	return pd.decode
}
