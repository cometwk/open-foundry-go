package util

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/fatih/structs"
	"github.com/mitchellh/mapstructure"
)

// MapToStruct: deep decode map 和 struct 互转, 但是要求struct的tag是json
// MapToStruct: 使用泛型和自定义钩子来增强 map 到 struct 的转换。
// T 是目标结构体的类型。
// result 必须是 *T 类型的指针。
func MapToStruct(m any, result any) error {
	// 创建一个包含时间解析钩子的解码器配置
	config := &mapstructure.DecoderConfig{
		Result:           result,
		TagName:          "json",
		WeaklyTypedInput: true,
		DecodeHook: mapstructure.ComposeDecodeHookFunc(
			stringToTimeHookFunc(time.RFC3339Nano),
		),
		// (可选) 增加严格模式：如果 map 中有 struct 中不存在的字段，则报错。
		// ErrorUnused: true,
	}

	decoder, err := mapstructure.NewDecoder(config)
	if err != nil {
		return err
	}
	return decoder.Decode(m)
}

// StructToMap: deep encode struct to map, 但是要求struct的tag是json
// 性能测试: 10000次耗时: 7.09725ms
// @deprecated
func StructToMapFast(input any) map[string]any {
	s := structs.New(input)
	s.TagName = "json"
	return s.Map()
}

// StructToMap 使用 encoding/json 实现，功能完整，支持 time.Time 格式化和 omitempty。
// 性能相对较低，但对于API序列化等场景是更正确的选择。
// 使用泛型约束输入，并增加了对非 struct 类型的运行时检查。
func StructToMap[T any](input T) map[string]any {
	// 运行时检查，确保输入是 struct 或指向 struct 的指针
	val := reflect.ValueOf(input)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		// return nil, fmt.Errorf("StructToMap: input type %T is not a struct", input)
		panic(fmt.Sprintf("StructToMap: input type %T is not a struct", input))

	}

	// 1. Marshal to JSON bytes
	data, err := json.Marshal(input)
	if err != nil {
		// return nil, fmt.Errorf("StructToMap: failed to marshal struct to json: %w", err)
		panic(fmt.Sprintf("StructToMap: failed to marshal struct to json: %v", err))
	}

	// 2. Unmarshal JSON bytes to a map
	var resultMap map[string]any
	if err := json.Unmarshal(data, &resultMap); err != nil {
		// return nil, fmt.Errorf("StructToMap: failed to unmarshal json to map: %w", err)
		panic(fmt.Sprintf("StructToMap: failed to unmarshal json to map: %v", err))
	}

	return resultMap
}

// 性能测试: 10000次耗时: 75.961542ms
func MapToStruct_old[T any](m map[string]any, dest *T) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	err = json.Unmarshal(data, dest)
	if err != nil {
		return err
	}
	return nil
}

// 类似 ts spread operator 的展开操作符
// dest = {...source1, ...source2, ...dest}
func Spread[T any](dest *T, source ...any) error {
	r := make(map[string]any)

	// 处理多个 source
	for _, src := range source {
		s := StructToMap(src)
		shallowCopy(r, s) // 浅合并到目标map
	}

	// 处理 dest
	d := StructToMap(dest)

	shallowCopy(r, d) // 浅合并
	// fmt.Printf("r=%v\n", r)
	return MapToStruct(r, dest)
}

// 深度拷贝
func DeepClone[T any](src T) (T, error) {
	var dst T
	if reflect.ValueOf(src).IsZero() {
		return dst, nil
	}

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	dec := gob.NewDecoder(&buf)

	if err := enc.Encode(src); err != nil {
		return dst, fmt.Errorf("encode error: %w", err)
	}

	if err := dec.Decode(&dst); err != nil {
		return dst, fmt.Errorf("decode error: %w", err)
	}

	return dst, nil
}

func MustDeepClone[T any](src T) T {
	dst, err := DeepClone(src)
	if err != nil {
		panic(err)
	}
	return dst
}

func shallowCopy[M1 ~map[K]V, M2 ~map[K]V, K comparable, V any](dst M1, src M2) {
	for k, v := range src {
		if isEmpty(v) {
			continue
		}
		dst[k] = v
	}
}

func isEmpty(x any) bool {
	if x == nil {
		return true
	}

	v := reflect.ValueOf(x)

	switch v.Kind() {
	case reflect.String, reflect.Array, reflect.Slice:
		return v.Len() == 0
	case reflect.Map: // 按 ts spread 的逻辑，{} 属于非空
		return false
	case reflect.Ptr, reflect.Interface:
		return v.IsNil()
	case reflect.Struct:
		// 判断是否是零值结构体
		return reflect.DeepEqual(x, reflect.Zero(v.Type()).Interface())
	default:
		// 判断基础类型的零值（int==0, bool==false 等）
		return reflect.DeepEqual(x, reflect.Zero(v.Type()).Interface())
	}
}

// stringToTimeHookFunc 是一个 mapstructure 的解码钩子，用于将特定格式的字符串解析为 time.Time
func stringToTimeHookFunc(layout string) mapstructure.DecodeHookFunc {
	// 这个函数返回一个闭包，该闭包才是真正的钩子函数
	return func(f reflect.Type, t reflect.Type, data any) (any, error) {
		// 检查输入数据是否是字符串，以及目标类型是否是 time.Time
		if f.Kind() != reflect.String || t != reflect.TypeOf(time.Time{}) {
			return data, nil // 如果不满足条件，则原样返回，让 mapstructure 继续处理
		}

		// 执行字符串到 time.Time 的转换
		return time.Parse(layout, data.(string))
	}
}
