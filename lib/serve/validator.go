package serve

import (
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/labstack/echo/v5"
)

// 自定义校验器
type customValidator0 struct {
	validate *validator.Validate
}

func NewCustomValidator() *customValidator0 {
	validate := validator.New()

	custom(validate)

	return &customValidator0{validate: validate}
}

func (cv *customValidator0) Validate(i interface{}) error {
	if err := cv.validate.Struct(i); err != nil {
		if errs, ok := err.(validator.ValidationErrors); ok {
			var errsStr string
			for _, e := range errs {
				errsStr += translateError(e) + ","
			}
			return echo.NewHTTPError(http.StatusBadRequest, errsStr)
		}

		// Optionally, you could return the error to give each route more control over the status code
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	return nil
}

// 自定义校验器
func custom(validate *validator.Validate) {
	validate.RegisterValidation("mobile", mobileValidator)
}

func mobileValidator(fl validator.FieldLevel) bool {
	mobile := fl.Field().String()
	re := regexp.MustCompile(`^1[3-9]\d{9}$`)
	return re.MatchString(mobile)
}

// 翻译错误信息
func translateError(e validator.FieldError) string {
	switch e.Tag() {
	case "required":
		return fmt.Sprintf("%s 为必填项", e.Field())
	case "email":
		return fmt.Sprintf("%s 格式不正确", e.Field())
	case "gte":
		return fmt.Sprintf("%s 必须大于等于 %s", e.Field(), e.Param())
	default:
		return fmt.Sprintf("%s 校验失败", e.Field())
	}
}

// 统一处理 trim
type customBinder struct{}

func (cb *customBinder) Bind(c *echo.Context, i interface{}) error {
	db := new(echo.DefaultBinder)
	if err := db.Bind(c, i); err != nil {
		return err
	}
	// 若 i 为 string，或 struct 且成员为 string，则进行 trim 操作
	trimStrings(i)
	return nil
}

func NewCustomBinder() echo.Binder {
	return &customBinder{}
}

func trimStrings(i interface{}) {
	if i == nil {
		return
	}
	trimValue(reflect.ValueOf(i))
}

func trimValue(v reflect.Value) {
	if !v.IsValid() {
		return
	}

	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			return
		}
		trimValue(v.Elem())
	case reflect.String:
		if v.CanSet() {
			v.SetString(strings.TrimSpace(v.String()))
		}
	case reflect.Struct:
		for idx := 0; idx < v.NumField(); idx++ {
			trimValue(v.Field(idx))
		}
	case reflect.Slice, reflect.Array:
		for idx := 0; idx < v.Len(); idx++ {
			trimValue(v.Index(idx))
		}
	}
}
