package api

import (
	"fmt"
	"reflect"
	"unsafe"
)

// abi.Type is 48 bytes on 64-bit gc; structType follows with Name + fields slice.
// rewriteStructFieldType is only used to make ObjectType @link/FK funcs return
// the same generated pointer type they live on (reflect.StructOf cannot express
// that cycle). A mismatch fails New rather than silently corrupting memory.
type abiTypeLayout struct {
	Size_       uintptr
	PtrBytes    uintptr
	Hash        uint32
	TFlag       uint8
	Align_      uint8
	FieldAlign_ uint8
	Kind_       uint8
	Equal       func(unsafe.Pointer, unsafe.Pointer) bool
	GCData      *byte
	Str         int32
	PtrToThis   int32
}

type abiNameLayout struct{ Bytes *byte }

type abiStructFieldLayout struct {
	Name   abiNameLayout
	Typ    unsafe.Pointer
	Offset uintptr
}

type abiStructTypeLayout struct {
	abiTypeLayout
	PkgPath abiNameLayout
	Fields  []abiStructFieldLayout
}

const abiKindStruct = 25

func typeData(t reflect.Type) unsafe.Pointer {
	return (*[2]unsafe.Pointer)(unsafe.Pointer(&t))[1]
}

func rewriteStructFieldType(st reflect.Type, index int, newType reflect.Type) error {
	if st == nil || st.Kind() != reflect.Struct {
		return fmt.Errorf("api: rewrite field type: not a struct")
	}
	if index < 0 || index >= st.NumField() {
		return fmt.Errorf("api: rewrite field type: index %d out of range", index)
	}
	if newType == nil {
		return fmt.Errorf("api: rewrite field type: nil replacement")
	}
	layout := (*abiStructTypeLayout)(typeData(st))
	if layout.Kind_ != abiKindStruct || len(layout.Fields) != st.NumField() {
		return fmt.Errorf("api: reflect struct layout drift (kind=%d n=%d want n=%d)", layout.Kind_, len(layout.Fields), st.NumField())
	}
	layout.Fields[index].Typ = typeData(newType)
	if st.Field(index).Type != newType {
		return fmt.Errorf("api: rewrite field type: %s still %s, want %s", st.Field(index).Name, st.Field(index).Type, newType)
	}
	return nil
}
