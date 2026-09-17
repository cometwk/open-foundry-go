package util

import (
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v5"
)

func BindAndValidate[T any](c *echo.Context, input *T) error {
	err := c.Bind(input)
	if err != nil {
		return err
	}
	if err = c.Validate(input); err != nil {
		return err
	}
	return nil
}

// 当请求参数不完整时，使用这个函数记录错误原因，然后返回 BadRequest 错误
func BadRequest(c *echo.Context, err error) error {
	ctx := c.Request().Context()
	slog.ErrorContext(ctx, "请求参数不完整", "url", c.Request().URL.String(), "error", err)
	return c.NoContent(http.StatusBadRequest)
}
