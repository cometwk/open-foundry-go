package orm

import (
	"reflect"
	"strconv"
	"time"
	"unsafe"

	jsoniter "github.com/json-iterator/go"
	"github.com/pkg/errors"
)

// 定义时间格式
const (
	iso8601Format   = time.RFC3339          // ISO 8601 格式
	localTimeFormat = "2006-01-02 15:04:05" // xorm 默认时间格式
)

func init() {
	// 自定义时间解码器
	jsoniter.RegisterTypeDecoderFunc("time.Time", func(ptr unsafe.Pointer, iter *jsoniter.Iterator) {
		str := iter.ReadString()
		loc := time.Local // 使用本地时区
		// TODO: 需要从 context 中获取 db 的 TZLocation
		// if db.TZLocation != nil {
		// 	loc = db.TZLocation
		// }

		// 尝试按 ISO 8601 格式解析
		if t, err := time.Parse(iso8601Format, str); err == nil {
			*(*time.Time)(ptr) = t
			return
		}
		// 尝试按本地时间格式解析
		if t, err := time.ParseInLocation(localTimeFormat, str, loc); err == nil {
			*(*time.Time)(ptr) = t
			return
		}
		// 解析失败
		iter.ReportError("customTimeDecoder", "invalid time format: "+str)
	})
	// 注册自定义的布尔值解码器
	jsoniter.RegisterTypeDecoderFunc("bool", func(ptr unsafe.Pointer, iter *jsoniter.Iterator) {
		switch iter.WhatIsNext() {
		case jsoniter.NumberValue:
			// 数字转布尔值：0为false，非0为true
			*(*bool)(ptr) = iter.ReadFloat64() != 0
		case jsoniter.BoolValue:
			*(*bool)(ptr) = iter.ReadBool()
		case jsoniter.StringValue:
			// 字符串转布尔值：支持 "true"/"false" 和 "1"/"0"
			str := iter.ReadString()
			*(*bool)(ptr) = str == "true" || str == "1"
		default:
			iter.ReportError("bool", "invalid bool value")
		}
	})

	jsoniter.RegisterTypeDecoderFunc("int", func(ptr unsafe.Pointer, iter *jsoniter.Iterator) {
		if iter.WhatIsNext() == jsoniter.StringValue {
			// 将字符串解析为整数
			str := iter.ReadString()
			val, err := strconv.Atoi(str)
			if err != nil {
				iter.ReportError("int decoder", "invalid integer: "+str)
				return
			}
			*((*int)(ptr)) = val
		} else {
			*((*int)(ptr)) = iter.ReadInt()
		}
	})
}

// StructToMap 将任意结构体转换为 map[string]any
// 使用 json tag 作为 map 的键名
func StructToMap(obj any) (map[string]any, error) {
	result := make(map[string]any)

	// 将结构体转为 JSON 字节
	jsonBytes, err := jsoniter.Marshal(obj)
	if err != nil {
		return nil, err
	}

	// 将 JSON 字节解析为 map
	if err := jsoniter.Unmarshal(jsonBytes, &result); err != nil {
		return nil, err
	}

	return result, nil
}

func MapToValue(m map[string]any, dest any) error {
	// 判断 dest 是否为 struct
	v := reflect.ValueOf(dest)
	if v.Kind() != reflect.Ptr {
		return errors.New("dest 必须是指针")
	}
	var src any = m
	if v.Elem().Kind() != reflect.Struct {
		// 如果 map 长度大于 1，返回错误
		if len(m) != 1 {
			return errors.New("查询字段数目不是 1")
		}
		// 获取第一个字段的值
		for _, val := range m {
			src = val
		}
	}
	return X2Y(src, dest)
}

// StructToSlice 将任意结构体转换为 []map[string]any
// 使用 json tag 作为 map 的键名
func StructToMapSlice(obj any) ([]map[string]any, error) {
	result := make([]map[string]any, 0)

	// 将结构体转为 JSON 字节
	jsonBytes, err := jsoniter.Marshal(obj)
	if err != nil {
		return nil, err
	}

	// 将 JSON 字节解析为 map
	if err := jsoniter.Unmarshal(jsonBytes, &result); err != nil {
		return nil, err
	}

	return result, nil
}

func MapToSlice(m []map[string]any, dest any) error {
	// 检查 dest 是否为切片
	v := reflect.ValueOf(dest)
	if v.Kind() != reflect.Ptr {
		return errors.New("dest 必须是指针")
	}
	if v.Elem().Kind() != reflect.Slice {
		return errors.New("dest 必须是切片")
	}
	obj := dest

	jsonBytes, err := jsoniter.Marshal(m)
	if err != nil {
		return err
	}

	return jsoniter.Unmarshal(jsonBytes, obj)
}

// X2Y 通过 JSON 序列化和反序列化实现任意类型之间的转换
// x: 源值
// y: 目标指针
func X2Y(x any, y any) error {
	// 检查 y 是否为指针
	destValue := reflect.ValueOf(y)
	if destValue.Kind() != reflect.Ptr {
		return errors.New("目标参数 y 必须是指针类型")
	}

	// 对于复杂类型（结构体、map、切片等），使用标准的 Marshal/Unmarshal
	// 还能处理 time.Time 类型等
	data, err := jsoniter.Marshal(x)
	if err != nil {
		return errors.Wrap(err, "序列化源值失败")
	}

	if err := jsoniter.Unmarshal(data, y); err != nil {
		return errors.Wrap(err, "反序列化到目标类型失败")
	}

	return nil
}
