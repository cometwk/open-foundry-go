package util

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMergeShouldOverride(t *testing.T) {
	/*
		const a = {
			name: 'app',
			db: { host: 'localhost', port: 3306 },
		}
		const b = {
			db: { host: '127.0.0.1' },
		}
		const c = { ...a, ...b }
		// => { name: 'app', db: { host: '127.0.0.1' } }
	*/
	type DBConfig struct {
		Host string
		Port int
	}
	type AppConfig struct {
		Name string `json:"name,omitempty"`
		DB   DBConfig
	}

	a := AppConfig{
		Name: "App",
		DB:   DBConfig{Host: "localhost", Port: 3306},
	}
	b := AppConfig{
		DB: DBConfig{Host: "127.0.0.1"}, // Port 是零值
	}

	err := Spread(&b, a)
	assert.NoError(t, err)
	assert.Equal(t, "App", b.Name)
	assert.Equal(t, "127.0.0.1", b.DB.Host)
	assert.Equal(t, 0, b.DB.Port)
}

func TestIsEmpty(t *testing.T) {
	assert.True(t, isEmpty(nil))
	assert.True(t, isEmpty(""))
	assert.True(t, isEmpty([]int{}))
	assert.True(t, isEmpty(0))
	assert.True(t, isEmpty(false))
	assert.True(t, isEmpty(struct{}{}))
	assert.True(t, isEmpty(struct{ Name string }{}))
	assert.False(t, isEmpty(map[string]int{}))
	assert.False(t, isEmpty(struct{ Name string }{"A"}))
}

func TestDeepClone(t *testing.T) {
	type TestStruct struct {
		Name    string
		Age     int
		Scores  []float64
		Details map[string]interface{}
		Time    time.Time
	}
	t.Run("struct", func(t *testing.T) {
		// 测试结构体的深拷贝
		original := TestStruct{
			Name:    "李四",
			Age:     30,
			Scores:  []float64{90.0, 85.0},
			Details: map[string]interface{}{"city": "上海", "score": 95},
			Time:    time.Date(2024, 3, 15, 14, 30, 0, 0, time.UTC),
		}

		cloned, err := DeepClone(original)
		assert.NoError(t, err)
		assert.Equal(t, original, cloned)

		// 修改克隆后的对象，确保不影响原对象
		cloned.Scores[0] = 100.0
		cloned.Details["city"] = "广州"
		assert.NotEqual(t, original.Scores[0], cloned.Scores[0])
		assert.NotEqual(t, original.Details["city"], cloned.Details["city"])

		// MustDeepClone
		cloned2 := MustDeepClone(original)
		cloned2.Scores[0] = 102.0
		cloned2.Details["city"] = "广州"
		assert.NotEqual(t, original.Scores[0], cloned2.Scores[0])
		assert.NotEqual(t, original.Details["city"], cloned2.Details["city"])

		// 若指针
		cloned3, err := DeepClone(&original)
		assert.NoError(t, err)
		assert.Equal(t, original, *cloned3)
	})

	t.Run("slice", func(t *testing.T) {
		// 测试切片的深拷贝
		original := []TestStruct{
			{
				Name:   "王五",
				Age:    28,
				Scores: []float64{92.0, 88.0},
			},
			{
				Name:   "赵六",
				Age:    35,
				Scores: []float64{95.0, 90.0},
			},
		}

		cloned, err := DeepClone(original)
		assert.NoError(t, err)
		assert.Equal(t, original, cloned)

		// 修改克隆后的切片，确保不影响原切片
		cloned[0].Scores[0] = 100.0
		assert.NotEqual(t, original[0].Scores[0], cloned[0].Scores[0])
	})

	t.Run("map", func(t *testing.T) {
		// 测试 map 的深拷贝
		original := map[string]TestStruct{
			"person1": {
				Name:   "张三",
				Age:    25,
				Scores: []float64{90.0, 85.0},
			},
			"person2": {
				Name:   "李四",
				Age:    30,
				Scores: []float64{95.0, 88.0},
			},
		}

		cloned, err := DeepClone(original)
		assert.NoError(t, err)
		assert.Equal(t, original, cloned)

		// 修改克隆后的 map，确保不影响原 map
		cloned["person1"].Scores[0] = 100.0
		assert.NotEqual(t, original["person1"].Scores[0], cloned["person1"].Scores[0])
	})
}

func TestMapToStruct(t *testing.T) {
	t.Run("基本类型转换", func(t *testing.T) {
		type User struct {
			Name string `json:"name"`
			Age  int    `json:"age"`
		}
		m := map[string]any{"name": "张三", "age": 18}
		var u User
		err := MapToStruct(m, &u)
		assert.NoError(t, err)
		assert.Equal(t, "张三", u.Name)
		assert.Equal(t, 18, u.Age)
	})

	t.Run("map缺少字段", func(t *testing.T) {
		type User struct {
			Name string `json:"name"`
			Age  int    `json:"age"`
		}
		m2 := map[string]any{"name": "李四"}
		u2 := User{}
		err := MapToStruct(m2, &u2) // 注意这里应该是 u2
		assert.NoError(t, err)
		assert.Equal(t, "李四", u2.Name)
		assert.Equal(t, 0, u2.Age) // 缺失的字段应为零值
	})

	t.Run("嵌套结构体", func(t *testing.T) {
		type Address struct {
			City    string `json:"city"`
			ZipCode string `json:"zip_code"`
		}
		type Person struct {
			Name    string  `json:"name"`
			Age     int     `json:"age"`
			Address Address `json:"address"`
		}

		m := map[string]any{
			"name": "王五",
			"age":  30,
			"address": map[string]any{
				"city":     "上海",
				"zip_code": "200000",
			},
		}
		var p Person
		err := MapToStruct(m, &p)
		assert.NoError(t, err)
		assert.Equal(t, "王五", p.Name)
		assert.Equal(t, 30, p.Age)
		assert.Equal(t, "上海", p.Address.City)
		assert.Equal(t, "200000", p.Address.ZipCode)
	})

	t.Run("嵌套结构体指针", func(t *testing.T) {
		type AddressPtr struct {
			City    string `json:"city"`
			ZipCode string `json:"zip_code"`
		}
		type PersonPtr struct {
			Name    string      `json:"name"`
			Age     int         `json:"age"`
			Address *AddressPtr `json:"address"` // 指针类型
		}

		m := map[string]any{
			"name": "赵六",
			"age":  40,
			"address": map[string]any{
				"city":     "广州",
				"zip_code": "510000",
			},
		}
		var pp PersonPtr
		err := MapToStruct(m, &pp)
		assert.NoError(t, err)
		assert.Equal(t, "赵六", pp.Name)
		assert.Equal(t, 40, pp.Age)
		assert.NotNil(t, pp.Address)
		assert.Equal(t, "广州", pp.Address.City)
		assert.Equal(t, "510000", pp.Address.ZipCode)
	})

	t.Run("嵌套结构体指针为nil", func(t *testing.T) {
		type AddressPtr struct {
			City    string `json:"city"`
			ZipCode string `json:"zip_code"`
		}
		type PersonPtr struct {
			Name    string      `json:"name"`
			Age     int         `json:"age"`
			Address *AddressPtr `json:"address"` // 指针类型
		}

		m := map[string]any{
			"name": "钱七",
			"age":  50,
			// "address" 字段缺失或为nil
		}
		var pp PersonPtr
		err := MapToStruct(m, &pp)
		assert.NoError(t, err)
		assert.Equal(t, "钱七", pp.Name)
		assert.Equal(t, 50, pp.Age)
		assert.Nil(t, pp.Address) // 缺失的嵌套指针字段应为nil
	})
}

func TestStructToMap(t *testing.T) {
	type User struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	u := User{Name: "王五", Age: 20}
	m := StructToMap(u)
	assert.Equal(t, "王五", m["name"])
	assert.Equal(t, 20, m["age"])

	// 测试指针
	up := &User{Name: "赵六", Age: 22}
	m2 := StructToMap(up)
	assert.Equal(t, "赵六", m2["name"])
	assert.Equal(t, 22, m2["age"])

	// 测试嵌套结构体
	type Address struct {
		City    string `json:"city"`
		ZipCode string `json:"zip_code"`
	}
	type Person struct {
		Name    string  `json:"name"`
		Age     int     `json:"age"`
		Address Address `json:"address"`
	}

	p := Person{
		Name: "钱七",
		Age:  30,
		Address: Address{
			City:    "北京",
			ZipCode: "100000",
		},
	}
	mp := StructToMap(p)
	assert.Equal(t, "钱七", mp["name"])
	assert.Equal(t, 30, mp["age"])
	assert.Contains(t, mp, "address")
	nestedAddress, ok := mp["address"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "北京", nestedAddress["city"])
	assert.Equal(t, "100000", nestedAddress["zip_code"])

	// 测试嵌套结构体指针
	pp := &Person{
		Name: "孙八",
		Age:  35,
		Address: Address{
			City:    "上海",
			ZipCode: "200000",
		},
	}
	mpp := StructToMap(pp)
	assert.Equal(t, "孙八", mpp["name"])
	assert.Equal(t, 35, mpp["age"])
	assert.Contains(t, mpp, "address")
	nestedAddressPtr, ok := mpp["address"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "上海", nestedAddressPtr["city"])
	assert.Equal(t, "200000", nestedAddressPtr["zip_code"])
}

func TestSpread(t *testing.T) {
	type Cfg struct {
		A string `json:"a"`
		B int    `json:"b"`
		C string `json:"c"`
	}
	c1 := Cfg{A: "a1", B: 1, C: "c1"}
	c2 := Cfg{A: "a2"}
	c3 := Cfg{B: 2, C: "c3"}

	var dest Cfg
	err := Spread(&dest, c1, c2, c3)
	assert.NoError(t, err)
	// Spread 逻辑: 后面的覆盖前面的
	assert.Equal(t, "a2", dest.A)
	assert.Equal(t, 2, dest.B)
	assert.Equal(t, "c3", dest.C)

	// 测试 dest 非零值
	dest2 := Cfg{A: "x", B: 9, C: "y"}
	err = Spread(&dest2, c1)
	assert.NoError(t, err)
	// dest2 的非零值不会被 c1 覆盖
	assert.Equal(t, "x", dest2.A)
	assert.Equal(t, 9, dest2.B)
	assert.Equal(t, "y", dest2.C)
}

func TestStructToMap_type(t *testing.T) {
	t.Run("time", func(t *testing.T) {
		// 测试时间类型转换
		type User struct {
			Name string    `json:"name"`
			Age  int       `json:"age"`
			Time time.Time `json:"time"`
		}
		value := time.Date(2024, 1, 1, 12, 34, 56, 123000000, time.UTC)
		u := &User{Name: "王五", Age: 20, Time: value}
		m := StructToMap(u)
		assert.Equal(t, "王五", m["name"])
		assert.Equal(t, 20.0, m["age"])
		assert.Equal(t, "2024-01-01T12:34:56.123Z", m["time"])

		u2 := User{}
		err := MapToStruct(m, &u2)
		assert.NoError(t, err)
		// fmt.Printf("u2: %+v\n", u2)
		assert.Equal(t, "王五", u2.Name)
		assert.Equal(t, 20, u2.Age)
		assert.Equal(t, "2024-01-01T12:34:56.123Z", u2.Time.Format(time.RFC3339Nano))

		// 要求 time 也支持 RFC3339
		m["time"] = value.Format(time.RFC3339)
		err = MapToStruct(m, &u2)
		assert.NoError(t, err)
		// fmt.Printf("u2: %+v\n", u2)
		assert.Equal(t, "王五", u2.Name)
		assert.Equal(t, 20, u2.Age)
		assert.Equal(t, "2024-01-01T12:34:56Z", u2.Time.Format(time.RFC3339))
	})
}
