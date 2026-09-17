package testutil

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"

	"github.com/labstack/echo/v5"
	pkglog "github.com/openfoundry/lib/xlog"
)

// ResponseRecorder 包装了 httptest.ResponseRecorder 以提供额外的辅助方法
type ResponseRecorder struct {
	*httptest.ResponseRecorder
}

// BodyJson 将 JSON 响应体解析为 map[string]interface{}
func (r *ResponseRecorder) BodyJson() (map[string]interface{}, error) {
	var response map[string]interface{}
	if err := json.Unmarshal(r.Body.Bytes(), &response); err != nil {
		return nil, err
	}
	return response, nil
}

// BodyArrayJson 将 JSON 响应体解析为 []map[string]interface{}
func (r *ResponseRecorder) BodyArrayJson() ([]map[string]interface{}, error) {
	var response []map[string]interface{}
	if err := json.Unmarshal(r.Body.Bytes(), &response); err != nil {
		return nil, err
	}
	return response, nil
}

// sessionMiddleware 将请求 ID 注入到 context 中
func sessionMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			reqid := c.Response().Header().Get(echo.HeaderXRequestID)
			if reqid == "" {
				reqid = "test-req-id" // 确保测试时 reqid 不为空
			}
			req := c.Request()
			ctx := pkglog.WithRequestID(req.Context(), reqid)
			req = req.WithContext(ctx)
			c.SetRequest(req)
			return next(c)
		}
	}
}

// NewRequest 针对给定的 echo 实例执行一个模拟的 HTTP 请求。
// 这是推荐的测试处理程序的方法，因为它会执行完整的中间件链。
func NewRequest(e *echo.Echo, method, path string, body io.Reader) *ResponseRecorder {
	return NewRequestWithHeader(e, method, path, body, nil)
}
func NewRequestWithHeader(e *echo.Echo, method, path string, body io.Reader, header http.Header) *ResponseRecorder {
	req := httptest.NewRequest(method, path, body)
	rec := httptest.NewRecorder()

	if body != nil {
		// 对于有请求体的请求，默认设置 Content-Type 为 JSON。
		// 如果需要，调用者可以覆盖它。
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	}
	// 设置 header
	for k, v := range header {
		for _, vv := range v {
			req.Header.Add(k, vv)
		}
	}
	e.ServeHTTP(rec, req)
	return &ResponseRecorder{rec}
}

// Get 执行一个带查询参数的 GET 请求
func Get(e *echo.Echo, path string, queryParams url.Values) *ResponseRecorder {
	if queryParams != nil {
		path = path + "?" + queryParams.Encode()
	}
	return NewRequest(e, http.MethodGet, path, nil)
}

// Post 执行一个带 JSON 字符串主体的 POST 请求
func Post(e *echo.Echo, path string, jsonBody string) *ResponseRecorder {
	return NewRequest(e, http.MethodPost, path, strings.NewReader(jsonBody))
}

func PostWithHeader(e *echo.Echo, path string, jsonBody string, header http.Header) *ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(jsonBody))

	return NewRequestWithHeader(e, http.MethodPost, path, req.Body, header)
}

// Put 执行一个带 JSON 字符串主体的 PUT 请求
func Put(e *echo.Echo, path string, jsonBody string) *ResponseRecorder {
	return NewRequest(e, http.MethodPut, path, strings.NewReader(jsonBody))
}

// Delete 执行一个 DELETE 请求
func Delete(e *echo.Echo, path string) *ResponseRecorder {
	return NewRequest(e, http.MethodDelete, path, nil)
}

// PostForm 执行一个带表单数据的 POST 请求
func PostForm(e *echo.Echo, path string, form url.Values) *ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return &ResponseRecorder{rec}
}

// PutForm 执行一个带表单数据的 PUT 请求
func PutForm(e *echo.Echo, path string, form url.Values) *ResponseRecorder {
	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(form.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return &ResponseRecorder{rec}
}

func PrintRoutes(e *echo.Echo) {
	// 打印所有路由
	routes := e.Router().Routes()

	sort.SliceStable(routes, func(i, j int) bool {
		return routes[i].Path < routes[j].Path
	})
	sb := strings.Builder{}

	for i, v := range routes {
		arr := strings.Split(v.Name, "/")
		fn := arr[len(arr)-1]
		if fn != "v4.glob..func1" {
			sb.WriteString(
				fmt.Sprintf("\n%4d %-6s %-42s %s", i, v.Method, v.Path, fn),
			)
		}
	}
	slog.Info(sb.String())
}
