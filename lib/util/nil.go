package util

import "reflect"

func IsNil(v any) bool {
	if v == nil {
		return true
	}
	val := reflect.ValueOf(v)
	switch val.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Slice, reflect.Map, reflect.Chan, reflect.Func:
		return val.IsNil()
	default:
		return false
	}
}

func IsNotNil(v any) bool {
	return !IsNil(v)
}
