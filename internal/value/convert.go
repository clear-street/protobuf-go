// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package value provides functionality for wrapping Go values to implement
// protoreflect values.
package value

import (
	"fmt"
	"reflect"

	papi "github.com/golang/protobuf/protoapi"
	pref "github.com/golang/protobuf/v2/reflect/protoreflect"
)

// Unwrapper unwraps the value to the underlying value.
// This is implemented by List and Map.
type Unwrapper interface {
	ProtoUnwrap() interface{}
}

var (
	boolType    = reflect.TypeOf(bool(false))
	int32Type   = reflect.TypeOf(int32(0))
	int64Type   = reflect.TypeOf(int64(0))
	uint32Type  = reflect.TypeOf(uint32(0))
	uint64Type  = reflect.TypeOf(uint64(0))
	float32Type = reflect.TypeOf(float32(0))
	float64Type = reflect.TypeOf(float64(0))
	stringType  = reflect.TypeOf(string(""))
	bytesType   = reflect.TypeOf([]byte(nil))

	enumIfaceV2    = reflect.TypeOf((*pref.ProtoEnum)(nil)).Elem()
	messageIfaceV1 = reflect.TypeOf((*papi.Message)(nil)).Elem()
	messageIfaceV2 = reflect.TypeOf((*pref.ProtoMessage)(nil)).Elem()

	byteType = reflect.TypeOf(byte(0))
)

// NewConverter matches a Go type with a protobuf kind and returns a Converter
// that converts between the two. NewConverter panics if it unable to provide a
// conversion between the two. The Converter methods also panic when they are
// called on incorrect Go types.
//
// This matcher deliberately supports a wider range of Go types than what
// protoc-gen-go historically generated to be able to automatically wrap some
// v1 messages generated by other forks of protoc-gen-go.
func NewConverter(t reflect.Type, k pref.Kind) Converter {
	return NewLegacyConverter(t, k, nil)
}

// LegacyWrapper is a set of wrapper methods that wraps legacy v1 Go types
// to implement the v2 reflection APIs.
type (
	LegacyWrapper interface {
		EnumOf(interface{}) LegacyEnum
		EnumTypeOf(interface{}) pref.EnumType

		MessageOf(interface{}) LegacyMessage
		MessageTypeOf(interface{}) pref.MessageType

		ExtensionTypeOf(pref.ExtensionDescriptor, interface{}) pref.ExtensionType

		// TODO: Remove these eventually. See the TODOs in protoapi.
		ExtensionDescFromType(pref.ExtensionType) *papi.ExtensionDesc
		ExtensionTypeFromDesc(*papi.ExtensionDesc) pref.ExtensionType
	}

	LegacyEnum = interface {
		pref.Enum
		ProtoUnwrap() interface{}
	}

	LegacyMessage = interface {
		pref.Message
		ProtoUnwrap() interface{}
	}
)

// NewLegacyConverter is identical to NewConverter,
// but supports wrapping legacy v1 messages to implement the v2 message API
// using the provided LegacyWrapper.
func NewLegacyConverter(t reflect.Type, k pref.Kind, w LegacyWrapper) Converter {
	switch k {
	case pref.BoolKind:
		if t.Kind() == reflect.Bool {
			return makeScalarConverter(t, boolType)
		}
	case pref.Int32Kind, pref.Sint32Kind, pref.Sfixed32Kind:
		if t.Kind() == reflect.Int32 {
			return makeScalarConverter(t, int32Type)
		}
	case pref.Int64Kind, pref.Sint64Kind, pref.Sfixed64Kind:
		if t.Kind() == reflect.Int64 {
			return makeScalarConverter(t, int64Type)
		}
	case pref.Uint32Kind, pref.Fixed32Kind:
		if t.Kind() == reflect.Uint32 {
			return makeScalarConverter(t, uint32Type)
		}
	case pref.Uint64Kind, pref.Fixed64Kind:
		if t.Kind() == reflect.Uint64 {
			return makeScalarConverter(t, uint64Type)
		}
	case pref.FloatKind:
		if t.Kind() == reflect.Float32 {
			return makeScalarConverter(t, float32Type)
		}
	case pref.DoubleKind:
		if t.Kind() == reflect.Float64 {
			return makeScalarConverter(t, float64Type)
		}
	case pref.StringKind:
		if t.Kind() == reflect.String || (t.Kind() == reflect.Slice && t.Elem() == byteType) {
			return makeScalarConverter(t, stringType)
		}
	case pref.BytesKind:
		if t.Kind() == reflect.String || (t.Kind() == reflect.Slice && t.Elem() == byteType) {
			return makeScalarConverter(t, bytesType)
		}
	case pref.EnumKind:
		// Handle v2 enums, which must satisfy the proto.Enum interface.
		if t.Kind() != reflect.Ptr && t.Implements(enumIfaceV2) {
			et := reflect.Zero(t).Interface().(pref.ProtoEnum).ProtoReflect().Type()
			return Converter{
				PBValueOf: func(v reflect.Value) pref.Value {
					if v.Type() != t {
						panic(fmt.Sprintf("invalid type: got %v, want %v", v.Type(), t))
					}
					e := v.Interface().(pref.ProtoEnum)
					return pref.ValueOf(e.ProtoReflect().Number())
				},
				GoValueOf: func(v pref.Value) reflect.Value {
					rv := reflect.ValueOf(et.New(v.Enum()))
					if rv.Type() != t {
						panic(fmt.Sprintf("invalid type: got %v, want %v", rv.Type(), t))
					}
					return rv
				},
				EnumType: et,
			}
		}

		// Handle v1 enums, which we identify as simply a named int32 type.
		if w != nil && t.PkgPath() != "" && t.Kind() == reflect.Int32 {
			et := w.EnumTypeOf(reflect.Zero(t).Interface())
			return Converter{
				PBValueOf: func(v reflect.Value) pref.Value {
					if v.Type() != t {
						panic(fmt.Sprintf("invalid type: got %v, want %v", v.Type(), t))
					}
					return pref.ValueOf(pref.EnumNumber(v.Int()))
				},
				GoValueOf: func(v pref.Value) reflect.Value {
					return reflect.ValueOf(v.Enum()).Convert(t)
				},
				EnumType: et,
				IsLegacy: true,
			}
		}
	case pref.MessageKind, pref.GroupKind:
		// Handle v2 messages, which must satisfy the proto.Message interface.
		if t.Kind() == reflect.Ptr && t.Implements(messageIfaceV2) {
			mt := reflect.Zero(t).Interface().(pref.ProtoMessage).ProtoReflect().Type()
			return Converter{
				PBValueOf: func(v reflect.Value) pref.Value {
					if v.Type() != t {
						panic(fmt.Sprintf("invalid type: got %v, want %v", v.Type(), t))
					}
					return pref.ValueOf(v.Interface())
				},
				GoValueOf: func(v pref.Value) reflect.Value {
					rv := reflect.ValueOf(v.Message().Interface())
					if rv.Type() != t {
						panic(fmt.Sprintf("invalid type: got %v, want %v", rv.Type(), t))
					}
					return rv
				},
				MessageType: mt,
			}
		}

		// Handle v1 messages, which we need to wrap as a v2 message.
		if w != nil && t.Kind() == reflect.Ptr && t.Implements(messageIfaceV1) {
			mt := w.MessageTypeOf(reflect.Zero(t).Interface())
			return Converter{
				PBValueOf: func(v reflect.Value) pref.Value {
					if v.Type() != t {
						panic(fmt.Sprintf("invalid type: got %v, want %v", v.Type(), t))
					}
					return pref.ValueOf(w.MessageOf(v.Interface()))
				},
				GoValueOf: func(v pref.Value) reflect.Value {
					rv := reflect.ValueOf(v.Message().(Unwrapper).ProtoUnwrap())
					if rv.Type() != t {
						panic(fmt.Sprintf("invalid type: got %v, want %v", rv.Type(), t))
					}
					return rv
				},
				MessageType: mt,
				IsLegacy:    true,
			}
		}
	}
	panic(fmt.Sprintf("invalid Go type %v for protobuf kind %v", t, k))
}

func makeScalarConverter(goType, pbType reflect.Type) Converter {
	return Converter{
		PBValueOf: func(v reflect.Value) pref.Value {
			if v.Type() != goType {
				panic(fmt.Sprintf("invalid type: got %v, want %v", v.Type(), goType))
			}
			if goType.Kind() == reflect.String && pbType.Kind() == reflect.Slice && v.Len() == 0 {
				return pref.ValueOf([]byte(nil)) // ensure empty string is []byte(nil)
			}
			return pref.ValueOf(v.Convert(pbType).Interface())
		},
		GoValueOf: func(v pref.Value) reflect.Value {
			rv := reflect.ValueOf(v.Interface())
			if rv.Type() != pbType {
				panic(fmt.Sprintf("invalid type: got %v, want %v", rv.Type(), pbType))
			}
			if pbType.Kind() == reflect.String && goType.Kind() == reflect.Slice && rv.Len() == 0 {
				return reflect.Zero(goType) // ensure empty string is []byte(nil)
			}
			return rv.Convert(goType)
		},
	}
}

// Converter provides functions for converting to/from Go reflect.Value types
// and protobuf protoreflect.Value types.
type Converter struct {
	PBValueOf   func(reflect.Value) pref.Value
	GoValueOf   func(pref.Value) reflect.Value
	EnumType    pref.EnumType
	MessageType pref.MessageType
	IsLegacy    bool
}
