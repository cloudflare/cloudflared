package syntax

import (
	"bytes"
	"fmt"
	"reflect"
	"runtime"
	"sort"
	"sync"
)

func Marshal(v interface{}) ([]byte, error) {
	e := &encodeState{}
	err := e.marshal(v, fieldOptions{})
	if err != nil {
		return nil, err
	}
	return e.Bytes(), nil
}

// Marshaler is the interface implemented by types that
// have a defined TLS encoding.
type Marshaler interface {
	MarshalTLS() ([]byte, error)
}

type encodeState struct {
	bytes.Buffer
}

func (e *encodeState) marshal(v interface{}, opts fieldOptions) (err error) {
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
	e.reflectValue(reflect.ValueOf(v), opts)
	return nil
}

func (e *encodeState) reflectValue(v reflect.Value, opts fieldOptions) {
	valueEncoder(v)(e, v, opts)
}

type encoderFunc func(e *encodeState, v reflect.Value, opts fieldOptions)

func valueEncoder(v reflect.Value) encoderFunc {
	if !v.IsValid() {
		panic(fmt.Errorf("Cannot encode an invalid value"))
	}
	return typeEncoder(v.Type())
}

var encoderCache sync.Map // map[reflect.Type]encoderFunc

func typeEncoder(t reflect.Type) encoderFunc {
	if fi, ok := encoderCache.Load(t); ok {
		return fi.(encoderFunc)
	}

	// XXX(RLB): Wait group based support for recursive types omitted

	// Compute the real encoder and replace the indirect func with it.
	f := newTypeEncoder(t)
	encoderCache.Store(t, f)
	return f
}

var (
	marshalerType = reflect.TypeOf(new(Marshaler)).Elem()
)

func newTypeEncoder(t reflect.Type) encoderFunc {
	var enc encoderFunc
	if t.Implements(marshalerType) {
		enc = marshalerEncoder
	} else {
		switch t.Kind() {
		case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			enc = uintEncoder
		case reflect.Array:
			enc = newArrayEncoder(t)
		case reflect.Slice:
			enc = newSliceEncoder(t)
		case reflect.Struct:
			enc = newStructEncoder(t)
		case reflect.Map:
			enc = newMapEncoder(t)
		case reflect.Ptr:
			enc = newPointerEncoder(t)
		default:
			panic(fmt.Errorf("Unsupported type (%s)", t))
		}
	}

	if t.Implements(validatorType) {
		enc = newValidatorEncoder(enc)
	}

	return enc
}

///// Specific encoders below

func omitEncoder(e *encodeState, v reflect.Value, opts fieldOptions) {
	// This space intentionally left blank
}

//////////

func marshalerEncoder(e *encodeState, v reflect.Value, opts fieldOptions) {
	if v.Kind() == reflect.Ptr && v.IsNil() && !opts.optional {
		panic(fmt.Errorf("Cannot encode nil pointer"))
	}

	if v.Kind() == reflect.Ptr && opts.optional {
		if v.IsNil() {
			writeUint(e, uint64(optionalFlagAbsent), 1)
			return
		}

		writeUint(e, uint64(optionalFlagPresent), 1)
	}

	m, ok := v.Interface().(Marshaler)
	if !ok {
		panic(fmt.Errorf("Non-Marshaler passed to marshalerEncoder"))
	}

	b, err := m.MarshalTLS()
	if err == nil {
		_, err = e.Write(b)
	}

	if err != nil {
		panic(err)
	}
}

//////////

func newValidatorEncoder(raw encoderFunc) encoderFunc {
	return func(e *encodeState, v reflect.Value, opts fieldOptions) {
		if v.Kind() == reflect.Ptr && v.IsNil() {
			// Cannot validate nil values; just pass through to encoder
			raw(e, v, opts)
			return
		}

		val, ok := v.Interface().(Validator)
		if !ok {
			panic(fmt.Errorf("Non-Validator passed to validatorEncoder"))
		}

		if err := val.ValidForTLS(); err != nil {
			panic(fmt.Errorf("Invalid TLS value: %v", err))
		}

		raw(e, v, opts)
	}
}

//////////

func uintEncoder(e *encodeState, v reflect.Value, opts fieldOptions) {
	if opts.varint {
		varintEncoder(e, v, opts)
		return
	}

	writeUint(e, v.Uint(), int(v.Type().Size()))
}

func varintEncoder(e *encodeState, v reflect.Value, opts fieldOptions) {
	writeVarint(e, v.Uint())
}

func writeVarint(e *encodeState, u uint64) {
	if (u >> 62) > 0 {
		panic(fmt.Errorf("uint value is too big for varint"))
	}

	var varintLen int
	for _, len := range []uint{1, 2, 4, 8} {
		if u < (uint64(1) << (8*len - 2)) {
			varintLen = int(len)
			break
		}
	}

	twoBits := map[int]uint64{1: 0x00, 2: 0x01, 4: 0x02, 8: 0x03}[varintLen]
	shift := uint(8*varintLen - 2)
	writeUint(e, u|(twoBits<<shift), varintLen)
}

func writeUint(e *encodeState, u uint64, len int) {
	for i := 0; i < len; i += 1 {
		e.WriteByte(byte(u >> uint(8*(len-i-1))))
	}
}

//////////

type arrayEncoder struct {
	elemEnc encoderFunc
}

func (ae *arrayEncoder) encode(e *encodeState, v reflect.Value, opts fieldOptions) {
	n := v.Len()
	for i := 0; i < n; i += 1 {
		ae.elemEnc(e, v.Index(i), opts)
	}
}

func newArrayEncoder(t reflect.Type) encoderFunc {
	enc := &arrayEncoder{typeEncoder(t.Elem())}
	return enc.encode
}

//////////

func encodeLength(e *encodeState, n int, opts fieldOptions) {
	if opts.maxSize > 0 && n > opts.maxSize {
		panic(fmt.Errorf("Encoded length more than max [%d > %d]", n, opts.maxSize))
	}
	if n < opts.minSize {
		panic(fmt.Errorf("Encoded length less than min [%d < %d]", n, opts.minSize))
	}

	switch {
	case opts.omitHeader:
		// None.

	case opts.varintHeader:
		writeVarint(e, uint64(n))

	case opts.headerSize > 0:
		if n>>uint(8*opts.headerSize) > 0 {
			panic(fmt.Errorf("Encoded length too long for header length [%d, %d]", n, opts.headerSize))
		}

		writeUint(e, uint64(n), int(opts.headerSize))

	default:
		panic(fmt.Errorf("Cannot encode a slice without a header length"))
	}
}

//////////

type sliceEncoder struct {
	ae *arrayEncoder
}

func (se *sliceEncoder) encode(e *encodeState, v reflect.Value, opts fieldOptions) {
	arrayState := &encodeState{}
	se.ae.encode(arrayState, v, opts)

	encodeLength(e, arrayState.Len(), opts)
	e.Write(arrayState.Bytes())
}

func newSliceEncoder(t reflect.Type) encoderFunc {
	enc := &sliceEncoder{&arrayEncoder{typeEncoder(t.Elem())}}
	return enc.encode
}

//////////

type structEncoder struct {
	fieldOpts []fieldOptions
	fieldEncs []encoderFunc
}

func (se *structEncoder) encode(e *encodeState, v reflect.Value, opts fieldOptions) {
	for i := range se.fieldEncs {
		se.fieldEncs[i](e, v.Field(i), se.fieldOpts[i])
	}
}

func newStructEncoder(t reflect.Type) encoderFunc {
	n := t.NumField()
	se := structEncoder{
		fieldOpts: make([]fieldOptions, n),
		fieldEncs: make([]encoderFunc, n),
	}

	for i := 0; i < n; i += 1 {
		f := t.Field(i)
		tag := f.Tag.Get("tls")
		opts := parseTag(tag)

		if !opts.ValidForType(f.Type) {
			panic(fmt.Errorf("Tags invalid for field type"))
		}

		se.fieldOpts[i] = opts
		if opts.omit {
			se.fieldEncs[i] = omitEncoder
		} else {
			se.fieldEncs[i] = typeEncoder(f.Type)
		}
	}

	return se.encode
}

//////////

type mapEncoder struct {
	keyEnc encoderFunc
	valEnc encoderFunc
}

type encMap struct {
	keyEncs [][]byte
	valEncs [][]byte
}

func (em encMap) Len() int { return len(em.keyEncs) }

func (em *encMap) Swap(i, j int) {
	em.keyEncs[i], em.keyEncs[j] = em.keyEncs[j], em.keyEncs[i]
	em.valEncs[i], em.valEncs[j] = em.valEncs[j], em.valEncs[i]
}

func (em encMap) Less(i, j int) bool {
	return bytes.Compare(em.keyEncs[i], em.keyEncs[j]) < 0
}

func (em encMap) Size() int {
	size := 0
	for i := range em.keyEncs {
		size += len(em.keyEncs[i]) + len(em.valEncs[i])
	}
	return size
}

func (em encMap) Encode(e *encodeState) {
	for i := range em.keyEncs {
		e.Write(em.keyEncs[i])
		e.Write(em.valEncs[i])
	}
}

func (me *mapEncoder) encode(e *encodeState, v reflect.Value, opts fieldOptions) {
	enc := &encMap{
		keyEncs: make([][]byte, v.Len()),
		valEncs: make([][]byte, v.Len()),
	}
	nullOpts := fieldOptions{}
	it := v.MapRange()
	for i := 0; i < enc.Len() && it.Next(); i++ {
		keyState := &encodeState{}
		me.keyEnc(keyState, it.Key(), nullOpts)
		enc.keyEncs[i] = keyState.Bytes()

		valState := &encodeState{}
		me.valEnc(valState, it.Value(), nullOpts)
		enc.valEncs[i] = valState.Bytes()
	}

	sort.Sort(enc)

	encodeLength(e, enc.Size(), opts)
	enc.Encode(e)
}

func newMapEncoder(t reflect.Type) encoderFunc {
	me := mapEncoder{
		keyEnc: typeEncoder(t.Key()),
		valEnc: typeEncoder(t.Elem()),
	}

	return me.encode
}

//////////

type pointerEncoder struct {
	base encoderFunc
}

func (pe pointerEncoder) encode(e *encodeState, v reflect.Value, opts fieldOptions) {
	if v.IsNil() && !opts.optional {
		panic(fmt.Errorf("Cannot encode nil pointer"))
	}

	if opts.optional {
		if v.IsNil() {
			writeUint(e, uint64(optionalFlagAbsent), 1)
			return
		}

		writeUint(e, uint64(optionalFlagPresent), 1)
	}

	pe.base(e, v.Elem(), opts)
}

func newPointerEncoder(t reflect.Type) encoderFunc {
	baseEncoder := typeEncoder(t.Elem())
	pe := pointerEncoder{base: baseEncoder}
	return pe.encode
}
