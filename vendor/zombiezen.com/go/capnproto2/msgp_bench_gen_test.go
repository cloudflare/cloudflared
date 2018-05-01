// +build msgpbench

package capnp_test

// NOTE: THIS FILE WAS PRODUCED BY THE
// MSGP CODE GENERATION TOOL (github.com/tinylib/msgp)
// DO NOT EDIT

import (
	"github.com/tinylib/msgp/msgp"
)

// DecodeMsg implements msgp.Decodable
func (z *Event) DecodeMsg(dc *msgp.Reader) (err error) {
	var field []byte
	_ = field
	var zxvk uint32
	zxvk, err = dc.ReadMapHeader()
	if err != nil {
		return
	}
	for zxvk > 0 {
		zxvk--
		field, err = dc.ReadMapKeyPtr()
		if err != nil {
			return
		}
		switch msgp.UnsafeString(field) {
		case "Name":
			z.Name, err = dc.ReadString()
			if err != nil {
				return
			}
		case "BirthDay":
			z.BirthDay, err = dc.ReadTime()
			if err != nil {
				return
			}
		case "Phone":
			z.Phone, err = dc.ReadString()
			if err != nil {
				return
			}
		case "Siblings":
			z.Siblings, err = dc.ReadInt()
			if err != nil {
				return
			}
		case "Spouse":
			z.Spouse, err = dc.ReadBool()
			if err != nil {
				return
			}
		case "Money":
			z.Money, err = dc.ReadFloat64()
			if err != nil {
				return
			}
		default:
			err = dc.Skip()
			if err != nil {
				return
			}
		}
	}
	return
}

// EncodeMsg implements msgp.Encodable
func (z *Event) EncodeMsg(en *msgp.Writer) (err error) {
	// map header, size 6
	// write "Name"
	err = en.Append(0x86, 0xa4, 0x4e, 0x61, 0x6d, 0x65)
	if err != nil {
		return err
	}
	err = en.WriteString(z.Name)
	if err != nil {
		return
	}
	// write "BirthDay"
	err = en.Append(0xa8, 0x42, 0x69, 0x72, 0x74, 0x68, 0x44, 0x61, 0x79)
	if err != nil {
		return err
	}
	err = en.WriteTime(z.BirthDay)
	if err != nil {
		return
	}
	// write "Phone"
	err = en.Append(0xa5, 0x50, 0x68, 0x6f, 0x6e, 0x65)
	if err != nil {
		return err
	}
	err = en.WriteString(z.Phone)
	if err != nil {
		return
	}
	// write "Siblings"
	err = en.Append(0xa8, 0x53, 0x69, 0x62, 0x6c, 0x69, 0x6e, 0x67, 0x73)
	if err != nil {
		return err
	}
	err = en.WriteInt(z.Siblings)
	if err != nil {
		return
	}
	// write "Spouse"
	err = en.Append(0xa6, 0x53, 0x70, 0x6f, 0x75, 0x73, 0x65)
	if err != nil {
		return err
	}
	err = en.WriteBool(z.Spouse)
	if err != nil {
		return
	}
	// write "Money"
	err = en.Append(0xa5, 0x4d, 0x6f, 0x6e, 0x65, 0x79)
	if err != nil {
		return err
	}
	err = en.WriteFloat64(z.Money)
	if err != nil {
		return
	}
	return
}

// MarshalMsg implements msgp.Marshaler
func (z *Event) MarshalMsg(b []byte) (o []byte, err error) {
	o = msgp.Require(b, z.Msgsize())
	// map header, size 6
	// string "Name"
	o = append(o, 0x86, 0xa4, 0x4e, 0x61, 0x6d, 0x65)
	o = msgp.AppendString(o, z.Name)
	// string "BirthDay"
	o = append(o, 0xa8, 0x42, 0x69, 0x72, 0x74, 0x68, 0x44, 0x61, 0x79)
	o = msgp.AppendTime(o, z.BirthDay)
	// string "Phone"
	o = append(o, 0xa5, 0x50, 0x68, 0x6f, 0x6e, 0x65)
	o = msgp.AppendString(o, z.Phone)
	// string "Siblings"
	o = append(o, 0xa8, 0x53, 0x69, 0x62, 0x6c, 0x69, 0x6e, 0x67, 0x73)
	o = msgp.AppendInt(o, z.Siblings)
	// string "Spouse"
	o = append(o, 0xa6, 0x53, 0x70, 0x6f, 0x75, 0x73, 0x65)
	o = msgp.AppendBool(o, z.Spouse)
	// string "Money"
	o = append(o, 0xa5, 0x4d, 0x6f, 0x6e, 0x65, 0x79)
	o = msgp.AppendFloat64(o, z.Money)
	return
}

// UnmarshalMsg implements msgp.Unmarshaler
func (z *Event) UnmarshalMsg(bts []byte) (o []byte, err error) {
	var field []byte
	_ = field
	var zbzg uint32
	zbzg, bts, err = msgp.ReadMapHeaderBytes(bts)
	if err != nil {
		return
	}
	for zbzg > 0 {
		zbzg--
		field, bts, err = msgp.ReadMapKeyZC(bts)
		if err != nil {
			return
		}
		switch msgp.UnsafeString(field) {
		case "Name":
			z.Name, bts, err = msgp.ReadStringBytes(bts)
			if err != nil {
				return
			}
		case "BirthDay":
			z.BirthDay, bts, err = msgp.ReadTimeBytes(bts)
			if err != nil {
				return
			}
		case "Phone":
			z.Phone, bts, err = msgp.ReadStringBytes(bts)
			if err != nil {
				return
			}
		case "Siblings":
			z.Siblings, bts, err = msgp.ReadIntBytes(bts)
			if err != nil {
				return
			}
		case "Spouse":
			z.Spouse, bts, err = msgp.ReadBoolBytes(bts)
			if err != nil {
				return
			}
		case "Money":
			z.Money, bts, err = msgp.ReadFloat64Bytes(bts)
			if err != nil {
				return
			}
		default:
			bts, err = msgp.Skip(bts)
			if err != nil {
				return
			}
		}
	}
	o = bts
	return
}

// Msgsize returns an upper bound estimate of the number of bytes occupied by the serialized message
func (z *Event) Msgsize() (s int) {
	s = 1 + 5 + msgp.StringPrefixSize + len(z.Name) + 9 + msgp.TimeSize + 6 + msgp.StringPrefixSize + len(z.Phone) + 9 + msgp.IntSize + 7 + msgp.BoolSize + 6 + msgp.Float64Size
	return
}
