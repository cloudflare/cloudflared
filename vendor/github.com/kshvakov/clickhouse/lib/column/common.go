package column

import (
	"fmt"
	"reflect"
	"time"
)

type ErrUnexpectedType struct {
	Column Column
	T      interface{}
}

func (err *ErrUnexpectedType) Error() string {
	return fmt.Sprintf("%s: unexpected type %T", err.Column, err.T)
}

var baseTypes = map[interface{}]reflect.Value{
	int8(0):     reflect.ValueOf(int8(0)),
	int16(0):    reflect.ValueOf(int16(0)),
	int32(0):    reflect.ValueOf(int32(0)),
	int64(0):    reflect.ValueOf(int64(0)),
	uint8(0):    reflect.ValueOf(uint8(0)),
	uint16(0):   reflect.ValueOf(uint16(0)),
	uint32(0):   reflect.ValueOf(uint32(0)),
	uint64(0):   reflect.ValueOf(uint64(0)),
	float32(0):  reflect.ValueOf(float32(0)),
	float64(0):  reflect.ValueOf(float64(0)),
	string(""):  reflect.ValueOf(string("")),
	time.Time{}: reflect.ValueOf(time.Time{}),
}

type base struct {
	name, chType string
	valueOf      reflect.Value
}

func (base *base) Name() string {
	return base.name
}

func (base *base) CHType() string {
	return base.chType
}

func (base *base) ScanType() reflect.Type {
	return base.valueOf.Type()
}

func (base *base) defaultValue() interface{} {
	return base.valueOf.Interface()
}

func (base *base) String() string {
	return fmt.Sprintf("%s (%s)", base.name, base.chType)
}
