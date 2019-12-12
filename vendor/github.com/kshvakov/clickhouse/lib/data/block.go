package data

import (
	"database/sql/driver"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/kshvakov/clickhouse/lib/binary"
	"github.com/kshvakov/clickhouse/lib/column"
	wb "github.com/kshvakov/clickhouse/lib/writebuffer"
)

type offset [][]int

type Block struct {
	Values     [][]interface{}
	Columns    []column.Column
	NumRows    uint64
	NumColumns uint64
	offsets    []offset
	buffers    []*buffer
	info       blockInfo
}

func (block *Block) Copy() *Block {
	return &Block{
		Columns:    block.Columns,
		NumColumns: block.NumColumns,
		info:       block.info,
	}
}

func (block *Block) ColumnNames() []string {
	names := make([]string, 0, len(block.Columns))
	for _, column := range block.Columns {
		names = append(names, column.Name())
	}
	return names
}

func (block *Block) Read(serverInfo *ServerInfo, decoder *binary.Decoder) (err error) {
	if err = block.info.read(decoder); err != nil {
		return err
	}

	if block.NumColumns, err = decoder.Uvarint(); err != nil {
		return err
	}
	if block.NumRows, err = decoder.Uvarint(); err != nil {
		return err
	}
	block.Values = make([][]interface{}, block.NumColumns)
	if block.NumRows > 10 {
		for i := 0; i < int(block.NumColumns); i++ {
			block.Values[i] = make([]interface{}, 0, block.NumRows)
		}
	}
	for i := 0; i < int(block.NumColumns); i++ {
		var (
			value      interface{}
			columnName string
			columnType string
		)
		if columnName, err = decoder.String(); err != nil {
			return err
		}
		if columnType, err = decoder.String(); err != nil {
			return err
		}
		c, err := column.Factory(columnName, columnType, serverInfo.Timezone)
		if err != nil {
			return err
		}
		block.Columns = append(block.Columns, c)
		switch column := c.(type) {
		case *column.Array:
			if block.Values[i], err = column.ReadArray(decoder, int(block.NumRows)); err != nil {
				return err
			}
		case *column.Nullable:
			if block.Values[i], err = column.ReadNull(decoder, int(block.NumRows)); err != nil {
				return err
			}
		default:
			for row := 0; row < int(block.NumRows); row++ {
				if value, err = column.Read(decoder); err != nil {
					return err
				}
				block.Values[i] = append(block.Values[i], value)
			}
		}
	}
	return nil
}

func (block *Block) writeArray(column column.Column, value reflect.Value, num, level int) error {
	switch {
	case value.Kind() == reflect.Slice:
		if len(block.offsets[num]) < level {
			block.offsets[num] = append(block.offsets[num], []int{value.Len()})
		} else {
			block.offsets[num][level-1] = append(
				block.offsets[num][level-1],
				block.offsets[num][level-1][len(block.offsets[num][level-1])-1]+value.Len(),
			)
		}
		for i := 0; i < value.Len(); i++ {
			if err := block.writeArray(column, value.Index(i), num, level+1); err != nil {
				return err
			}
		}
	default:
		if err := column.Write(block.buffers[num].Column, value.Interface()); err != nil {
			return err
		}
	}
	return nil
}

func (block *Block) AppendRow(args []driver.Value) error {
	if len(block.Columns) != len(args) {
		return fmt.Errorf("block: expected %d arguments (columns: %s), got %d", len(block.Columns), strings.Join(block.ColumnNames(), ", "), len(args))
	}
	block.Reserve()
	{
		block.NumRows++
	}
	for num, c := range block.Columns {
		switch column := c.(type) {
		case *column.Array:
			value := reflect.ValueOf(args[num])
			if value.Kind() != reflect.Slice {
				return fmt.Errorf("unsupported Array(T) type [%T]", value.Interface())
			}
			if err := block.writeArray(c, value, num, 1); err != nil {
				return err
			}
		case *column.Nullable:
			if err := column.WriteNull(block.buffers[num].Offset, block.buffers[num].Column, args[num]); err != nil {
				return err
			}
		default:
			if err := column.Write(block.buffers[num].Column, args[num]); err != nil {
				return err
			}
		}
	}
	return nil
}

func (block *Block) Reserve() {
	if len(block.buffers) == 0 {
		block.buffers = make([]*buffer, len(block.Columns))
		block.offsets = make([]offset, len(block.Columns))
		for i := 0; i < len(block.Columns); i++ {
			var (
				offsetBuffer = wb.New(wb.InitialSize)
				columnBuffer = wb.New(wb.InitialSize)
			)
			block.buffers[i] = &buffer{
				Offset:       binary.NewEncoder(offsetBuffer),
				Column:       binary.NewEncoder(columnBuffer),
				offsetBuffer: offsetBuffer,
				columnBuffer: columnBuffer,
			}
		}
	}
}

func (block *Block) Reset() {
	block.NumRows = 0
	block.NumColumns = 0
	for _, buffer := range block.buffers {
		buffer.reset()
	}
	{
		block.offsets = nil
		block.buffers = nil
	}
}

func (block *Block) Write(serverInfo *ServerInfo, encoder *binary.Encoder) error {
	if err := block.info.write(encoder); err != nil {
		return err
	}
	encoder.Uvarint(block.NumColumns)
	encoder.Uvarint(block.NumRows)
	defer func() {
		block.NumRows = 0
		for i := range block.offsets {
			block.offsets[i] = offset{}
		}
	}()
	for i, column := range block.Columns {
		encoder.String(column.Name())
		encoder.String(column.CHType())
		if len(block.buffers) == len(block.Columns) {
			for _, offsets := range block.offsets[i] {
				for _, offset := range offsets {
					if err := encoder.UInt64(uint64(offset)); err != nil {
						return err
					}
				}
			}
			if _, err := block.buffers[i].WriteTo(encoder); err != nil {
				return err
			}
		}
	}
	return nil
}

type blockInfo struct {
	num1        uint64
	isOverflows bool
	num2        uint64
	bucketNum   int32
	num3        uint64
}

func (info *blockInfo) read(decoder *binary.Decoder) error {
	var err error
	if info.num1, err = decoder.Uvarint(); err != nil {
		return err
	}
	if info.isOverflows, err = decoder.Bool(); err != nil {
		return err
	}
	if info.num2, err = decoder.Uvarint(); err != nil {
		return err
	}
	if info.bucketNum, err = decoder.Int32(); err != nil {
		return err
	}
	if info.num3, err = decoder.Uvarint(); err != nil {
		return err
	}
	return nil
}

func (info *blockInfo) write(encoder *binary.Encoder) error {
	if err := encoder.Uvarint(1); err != nil {
		return err
	}
	if err := encoder.Bool(info.isOverflows); err != nil {
		return err
	}
	if err := encoder.Uvarint(2); err != nil {
		return err
	}
	if info.bucketNum == 0 {
		info.bucketNum = -1
	}
	if err := encoder.Int32(info.bucketNum); err != nil {
		return err
	}
	if err := encoder.Uvarint(0); err != nil {
		return err
	}
	return nil
}

type buffer struct {
	Offset       *binary.Encoder
	Column       *binary.Encoder
	offsetBuffer *wb.WriteBuffer
	columnBuffer *wb.WriteBuffer
}

func (buf *buffer) WriteTo(w io.Writer) (int64, error) {
	var size int64
	{
		ln, err := buf.offsetBuffer.WriteTo(w)
		if err != nil {
			return size, err
		}
		size += ln
	}
	{
		ln, err := buf.columnBuffer.WriteTo(w)
		if err != nil {
			return size, err
		}
		size += ln
	}
	return size, nil
}

func (buf *buffer) reset() {
	buf.offsetBuffer.Reset()
	buf.columnBuffer.Reset()
}
