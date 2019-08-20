package data

import (
	"fmt"
	"reflect"
	"time"

	"github.com/kshvakov/clickhouse/lib/binary"
)

func (block *Block) WriteDate(c int, v time.Time) error {
	_, offset := v.Zone()
	nday := (v.Unix() + int64(offset)) / 24 / 3600
	return block.buffers[c].Column.UInt16(uint16(nday))
}

func (block *Block) WriteDateTime(c int, v time.Time) error {
	return block.buffers[c].Column.UInt32(uint32(v.Unix()))
}

func (block *Block) WriteBool(c int, v bool) error {
	if v {
		return block.buffers[c].Column.UInt8(1)
	}
	return block.buffers[c].Column.UInt8(0)
}

func (block *Block) WriteInt8(c int, v int8) error {
	return block.buffers[c].Column.Int8(v)
}

func (block *Block) WriteInt16(c int, v int16) error {
	return block.buffers[c].Column.Int16(v)
}

func (block *Block) WriteInt32(c int, v int32) error {
	return block.buffers[c].Column.Int32(v)
}

func (block *Block) WriteInt64(c int, v int64) error {
	return block.buffers[c].Column.Int64(v)
}

func (block *Block) WriteUInt8(c int, v uint8) error {
	return block.buffers[c].Column.UInt8(v)
}

func (block *Block) WriteUInt16(c int, v uint16) error {
	return block.buffers[c].Column.UInt16(v)
}

func (block *Block) WriteUInt32(c int, v uint32) error {
	return block.buffers[c].Column.UInt32(v)
}

func (block *Block) WriteUInt64(c int, v uint64) error {
	return block.buffers[c].Column.UInt64(v)
}

func (block *Block) WriteFloat32(c int, v float32) error {
	return block.buffers[c].Column.Float32(v)
}

func (block *Block) WriteFloat64(c int, v float64) error {
	return block.buffers[c].Column.Float64(v)
}

func (block *Block) WriteBytes(c int, v []byte) error {
	if err := block.buffers[c].Column.Uvarint(uint64(len(v))); err != nil {
		return err
	}
	if _, err := block.buffers[c].Column.Write(v); err != nil {
		return err
	}
	return nil
}

func (block *Block) WriteString(c int, v string) error {
	if err := block.buffers[c].Column.Uvarint(uint64(len(v))); err != nil {
		return err
	}
	if _, err := block.buffers[c].Column.Write(binary.Str2Bytes(v)); err != nil {
		return err
	}
	return nil
}

func (block *Block) WriteFixedString(c int, v []byte) error {
	return block.Columns[c].Write(block.buffers[c].Column, v)
}

func (block *Block) WriteArray(c int, v interface{}) error {
	value := reflect.ValueOf(v)
	if value.Kind() != reflect.Slice {
		return fmt.Errorf("unsupported Array(T) type [%T]", value.Interface())
	}
	return block.writeArray(block.Columns[c], value, c, 1)
}
