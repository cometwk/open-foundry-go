package orm

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	jsoniter "github.com/json-iterator/go"
)

var TZLocation = time.Local

func TestDemo1(t *testing.T) {
	type X struct {
		Abc int `json:"Field"`
	}

	jsonStr := `{"Field":"123"}`

	var x X
	err := jsoniter.Unmarshal([]byte(jsonStr), &x)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Printf("Parsed struct: %+v\n", x)
}

func TestX2Y(t *testing.T) {
	type testCase struct {
		name     string
		src      any
		destType any
		want     any
		wantErr  bool
	}

	tests := []testCase{
		{
			name:     "string to time.Time",
			src:      "2024-03-15 10:20:30",
			destType: new(time.Time),
			want:     time.Date(2024, 3, 15, 10, 20, 30, 0, TZLocation),
			wantErr:  false,
		},
		{
			name:     "string to int",
			src:      "123",
			destType: new(int),
			want:     123,
			wantErr:  false,
		},
		{
			name:     "int to bool",
			src:      1,
			destType: new(bool),
			want:     true,
			wantErr:  false,
		},
		{
			name:     "string to bool",
			src:      "true",
			destType: new(bool),
			want:     true,
			wantErr:  false,
		},
		{
						name:     "invalid string to int",
			src:      "not a number",
			destType: new(int),
			want:     0,
			wantErr:  true, // jsoniter unmarshal to int returns an error for invalid string
		},
		{
			name: "map to struct",
			src: map[string]any{
				"name": "张三",
				"age":  25,
			},
			destType: new(struct {
				Name string `json:"name"`
				Age  int    `json:"age"`
			}),
			want: &struct {
				Name string `json:"name"`
				Age  int    `json:"age"`
			}{
				Name: "张三",
				Age:  25,
			},
			wantErr: false,
		},
		{
			name: "nested map to struct",
			src: map[string]any{
				"user": map[string]any{
					"name": "李四",
					"age":  30,
				},
			},
			destType: new(struct {
				User struct {
					Name string `json:"name"`
					Age  int    `json:"age"`
				} `json:"user"`
			}),
			want: &struct {
				User struct {
					Name string `json:"name"`
					Age  int    `json:"age"`
				} `json:"user"`
			}{
				User: struct {
					Name string `json:"name"`
					Age  int    `json:"age"`
				}{
					Name: "李四",
					Age:  30,
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := X2Y(tt.src, tt.destType)
			if (err != nil) != tt.wantErr {
				t.Errorf("X2Y() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return // If error is expected, no need to check value
			}

			got := reflect.ValueOf(tt.destType).Elem().Interface()

			if !reflect.DeepEqual(got, tt.want) {
				// Special case for struct pointer comparison
				if reflect.ValueOf(tt.want).Kind() == reflect.Ptr {
					if reflect.DeepEqual(reflect.ValueOf(got).Interface(), reflect.ValueOf(tt.want).Elem().Interface()) {
						return
					}
				}
				t.Errorf("X2Y() got = %#v (%T), want %#v (%T)", got, got, tt.want, tt.want)
			}
		})
	}
}

func TestMapToValue(t *testing.T) {
	// Test case 1: Basic type conversion from map
	t.Run("basic types from map", func(t *testing.T) {
		type Person struct {
			Birthday time.Time `json:"birthday"`
			Yes      bool      `json:"yes"`
			Num      int       `json:"num"`
		}

		m := map[string]any{
			"birthday": "2024-12-31 16:08:24",
			"yes":      0,
			"num":      "123",
		}

		var p Person
		err := MapToValue(m, &p)
		if err != nil {
			t.Fatalf("MapToValue() failed: %+v", err)
		}

		expectedBirthday, _ := time.ParseInLocation("2006-01-02 15:04:05", "2024-12-31 16:08:24", TZLocation)
		if p.Num != 123 || p.Yes != false || !p.Birthday.Equal(expectedBirthday) {
			t.Errorf("MapToValue() got = %+v, want num=123, yes=false, birthday=%v", p, expectedBirthday)
		}
	})

	// Test case 2: Single value extraction
	t.Run("single value extraction", func(t *testing.T) {
		m := map[string]any{"age": 30}
		var age int
		err := MapToValue(m, &age)
		if err != nil {
			t.Fatalf("MapToValue() failed for single value: %+v", err)
		}
		if age != 30 {
			t.Errorf("MapToValue() single value got = %d, want 30", age)
		}
	})
}

func TestMapToSlice(t *testing.T) {
	type User struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	t.Run("map slice to struct slice", func(t *testing.T) {
		src := []map[string]any{
			{"name": "张三", "age": 25},
			{"name": "李四", "age": 30},
		}

		var users []User
		err := MapToSlice(src, &users)
		if err != nil {
			t.Errorf("MapToSlice() error = %v", err)
			return
		}

		want := []User{
			{Name: "张三", Age: 25},
			{Name: "李四", Age: 30},
		}

		if !reflect.DeepEqual(users, want) {
			t.Errorf("MapToSlice() = %+v, want %+v", users, want)
		}
	})

	t.Run("error cases", func(t *testing.T) {
		src := []map[string]any{{"name": "test"}}
		var invalidDestUser User // Non-slice destination
		if err := MapToSlice(src, &invalidDestUser); err == nil {
			t.Error("MapToSlice() should return error for non-slice destination")
		}

		var invalidDestSlice []User // Non-pointer destination
		if err := MapToSlice(src, invalidDestSlice); err == nil {
			t.Error("MapToSlice() should return error for non-pointer destination")
		}
	})
}
