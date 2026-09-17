package serve

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTrimStrings_StringPointer(t *testing.T) {
	val := "  hello  "
	trimStrings(&val)
	assert.Equal(t, "hello", val)
}

func TestTrimStrings_StructNested(t *testing.T) {
	type inner struct {
		Note string
		keep string
	}
	type outer struct {
		Name  string
		Inner inner
		Tags  []string
	}

	payload := outer{
		Name:  "  name  ",
		Inner: inner{Note: "  note  ", keep: "  keep  "},
		Tags:  []string{"  a  ", "b ", " c"},
	}

	trimStrings(&payload)

	assert.Equal(t, "name", payload.Name)
	assert.Equal(t, "note", payload.Inner.Note)
	assert.Equal(t, "  keep  ", payload.Inner.keep)
	assert.Equal(t, []string{"a", "b", "c"}, payload.Tags)
}

func TestTrimStrings_ArrayAndNil(t *testing.T) {
	type sample struct {
		Values [2]string
	}
	payload := sample{Values: [2]string{"  x", "y  "}}

	trimStrings(&payload)
	trimStrings(nil)

	assert.Equal(t, [2]string{"x", "y"}, payload.Values)
}
