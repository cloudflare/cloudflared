//go:build !purego
// +build !purego

package ws

import (
	"reflect"
	"unsafe"
)

func strToBytes(str string) (bts []byte) {
	s := (*reflect.StringHeader)(unsafe.Pointer(&str))
	b := (*reflect.SliceHeader)(unsafe.Pointer(&bts))
	b.Data = s.Data
	b.Len = s.Len
	b.Cap = s.Len
	return bts
}

func btsToString(bts []byte) (str string) {
	return *(*string)(unsafe.Pointer(&bts))
}
