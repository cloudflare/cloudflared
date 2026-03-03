// Adapted from https://github.com/open-telemetry/opentelemetry-go/blob/cc43e01c27892252aac9a8f20da28cdde957a289/attribute/value.go
//
// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package attribute

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// Type describes the type of the data Value holds.
type Type int // redefines builtin Type.

// Value represents the value part in key-value pairs.
type Value struct {
	vtype    Type
	numeric  uint64
	stringly string
}

const (
	// INVALID is used for a Value with no value set.
	INVALID Type = iota
	// BOOL is a boolean Type Value.
	BOOL
	// INT64 is a 64-bit signed integral Type Value.
	INT64
	// FLOAT64 is a 64-bit floating point Type Value.
	FLOAT64
	// STRING is a string Type Value.
	STRING
	// UINT64 is a 64-bit unsigned integral Type Value.
	//
	// This type is intentionally not exposed through the Builder API.
	UINT64
)

// BoolValue creates a BOOL Value.
func BoolValue(v bool) Value {
	return Value{
		vtype:   BOOL,
		numeric: boolToRaw(v),
	}
}

// IntValue creates an INT64 Value.
func IntValue(v int) Value {
	return Int64Value(int64(v))
}

// Int64Value creates an INT64 Value.
func Int64Value(v int64) Value {
	return Value{
		vtype:   INT64,
		numeric: int64ToRaw(v),
	}
}

// Float64Value creates a FLOAT64 Value.
func Float64Value(v float64) Value {
	return Value{
		vtype:   FLOAT64,
		numeric: float64ToRaw(v),
	}
}

// StringValue creates a STRING Value.
func StringValue(v string) Value {
	return Value{
		vtype:    STRING,
		stringly: v,
	}
}

// Uint64Value creates a UINT64 Value.
//
// This constructor is intentionally not exposed through the Builder API.
func Uint64Value(v uint64) Value {
	return Value{
		vtype:   UINT64,
		numeric: v,
	}
}

// Type returns a type of the Value.
func (v Value) Type() Type {
	return v.vtype
}

// AsBool returns the bool value. Make sure that the Value's type is
// BOOL.
func (v Value) AsBool() bool {
	return rawToBool(v.numeric)
}

// AsInt64 returns the int64 value. Make sure that the Value's type is
// INT64.
func (v Value) AsInt64() int64 {
	return rawToInt64(v.numeric)
}

// AsFloat64 returns the float64 value. Make sure that the Value's
// type is FLOAT64.
func (v Value) AsFloat64() float64 {
	return rawToFloat64(v.numeric)
}

// AsString returns the string value. Make sure that the Value's type
// is STRING.
func (v Value) AsString() string {
	return v.stringly
}

// AsUint64 returns the uint64 value. Make sure that the Value's type is
// UINT64.
func (v Value) AsUint64() uint64 {
	return v.numeric
}

type unknownValueType struct{}

// AsInterface returns Value's data as interface{}.
func (v Value) AsInterface() interface{} {
	switch v.Type() {
	case BOOL:
		return v.AsBool()
	case INT64:
		return v.AsInt64()
	case FLOAT64:
		return v.AsFloat64()
	case STRING:
		return v.stringly
	case UINT64:
		return v.numeric
	}
	return unknownValueType{}
}

// String returns a string representation of Value's data.
func (v Value) String() string {
	switch v.Type() {
	case BOOL:
		return strconv.FormatBool(v.AsBool())
	case INT64:
		return strconv.FormatInt(v.AsInt64(), 10)
	case FLOAT64:
		return fmt.Sprint(v.AsFloat64())
	case STRING:
		return v.stringly
	case UINT64:
		return strconv.FormatUint(v.numeric, 10)
	default:
		return "unknown"
	}
}

// MarshalJSON returns the JSON encoding of the Value.
func (v Value) MarshalJSON() ([]byte, error) {
	var jsonVal struct {
		Value any    `json:"value"`
		Type  string `json:"type"`
	}
	jsonVal.Type = mapTypesToStr[v.Type()]
	jsonVal.Value = v.AsInterface()
	return json.Marshal(jsonVal)
}

func (t Type) String() string {
	switch t {
	case BOOL:
		return "bool"
	case INT64:
		return "int64"
	case FLOAT64:
		return "float64"
	case STRING:
		return "string"
	case UINT64:
		return "uint64"
	}
	return "invalid"
}

// mapTypesToStr is a map from attribute.Type to the primitive types the server understands.
// https://develop.sentry.dev/sdk/foundations/data-model/attributes/#primitive-types
var mapTypesToStr = map[Type]string{
	INVALID: "",
	BOOL:    "boolean",
	INT64:   "integer",
	FLOAT64: "double",
	STRING:  "string",
	UINT64:  "integer", // wire format: same "integer" type
}
