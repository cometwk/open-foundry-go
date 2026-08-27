package api

import (
	"context"
	"reflect"
	"testing"

	graphql "github.com/graph-gophers/graphql-go"
)

func TestRewriteStructFieldType_RecursiveObject(t *testing.T) {
	placeholder := reflect.TypeOf((*struct{})(nil))
	strT := reflect.TypeOf("")
	fn := reflect.FuncOf(nil, []reflect.Type{placeholder}, false)
	typ := reflect.StructOf([]reflect.StructField{
		{Name: "Name", Type: strT, Tag: `graphql:"name"`},
		{Name: "Child", Type: fn, Tag: `graphql:"child"`},
	})
	pt := reflect.PointerTo(typ)
	fn2 := reflect.FuncOf(nil, []reflect.Type{pt}, false)
	if err := rewriteStructFieldType(typ, 1, fn2); err != nil {
		t.Fatal(err)
	}

	v := reflect.New(typ)
	v.Elem().Field(0).Set(reflect.ValueOf("root"))
	childFn := reflect.MakeFunc(typ.Field(1).Type, func(in []reflect.Value) []reflect.Value {
		c := reflect.New(typ)
		c.Elem().Field(0).Set(reflect.ValueOf("kid"))
		return []reflect.Value{c}
	})
	v.Elem().Field(1).Set(childFn)

	sdl := `type Query { n: N } type N { name: String! child: N }`
	rootT := reflect.StructOf([]reflect.StructField{
		{Name: "N", Type: reflect.FuncOf(nil, []reflect.Type{pt}, false), Tag: `graphql:"n"`},
	})
	root := reflect.New(rootT)
	root.Elem().Field(0).Set(reflect.MakeFunc(rootT.Field(0).Type, func(in []reflect.Value) []reflect.Value {
		return []reflect.Value{v}
	}))
	sch, err := graphql.ParseSchema(sdl, root.Interface(), graphql.UseFieldResolvers())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	res := sch.Exec(context.Background(), `{ n { name child { name } } }`, "", nil)
	if len(res.Errors) > 0 {
		t.Fatalf("exec errors: %v", res.Errors)
	}
	if string(res.Data) != `{"n":{"name":"root","child":{"name":"kid"}}}` {
		t.Fatalf("data=%s", res.Data)
	}
}

func TestRewriteStructFieldType_RejectsNonStruct(t *testing.T) {
	if err := rewriteStructFieldType(reflect.TypeOf(""), 0, reflect.TypeOf(0)); err == nil {
		t.Fatal("expected error for non-struct")
	}
	typ := reflect.StructOf([]reflect.StructField{
		{Name: "Name", Type: reflect.TypeOf("")},
	})
	if err := rewriteStructFieldType(typ, 3, reflect.TypeOf("")); err == nil {
		t.Fatal("expected error for out-of-range index")
	}
}
