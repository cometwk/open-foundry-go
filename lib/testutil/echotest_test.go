package testutil

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
)

// mockUser 是一个用于演示的简单结构体
type mockUser struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// setupEcho 创建一个新的 echo 实例并注册模拟的处理程序和中间件。
// 这模拟了一个真实应用的路由配置过程。
func setupEcho() *echo.Echo {
	e := echo.New()

	// 使用 sessionMiddleware，就像在真实应用中一样。
	// 真实应用可能还会有日志、认证等更多中间件。
	e.Use(sessionMiddleware())

	// --- 注册处理程序 ---

	// GET /user/:id - 根据 ID 获取用户的处理程序
	e.GET("/user/:id", func(c *echo.Context) error {
		id := c.Param("id")
		if id == "1" {
			return c.JSON(http.StatusOK, mockUser{ID: 1, Name: "John Doe"})
		}
		return c.JSON(http.StatusNotFound, map[string]string{"error": "User not found"})
	})

	// GET /users - 获取用户列表（可选名称过滤）的处理程序
	e.GET("/users", func(c *echo.Context) error {
		nameFilter := c.QueryParam("name")
		users := []mockUser{
			{ID: 1, Name: "John Doe"},
			{ID: 2, Name: "Jane Doe"},
		}
		if nameFilter != "" {
			var filteredUsers []mockUser
			for _, user := range users {
				if user.Name == nameFilter {
					filteredUsers = append(filteredUsers, user)
				}
			}
			return c.JSON(http.StatusOK, filteredUsers)
		}
		return c.JSON(http.StatusOK, users)
	})

	// POST /user - 创建新用户的处理程序
	e.POST("/user", func(c *echo.Context) error {
		var u mockUser
		if err := c.Bind(&u); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		}
		// 在真实的处理程序中，您会在这里将用户保存到数据库。
		// 这里我们仅返回带有新 ID 的已创建用户。
		u.ID = 100
		return c.JSON(http.StatusCreated, u)
	})

	return e
}

// TestUsage 演示了如何使用测试辅助函数。
func TestUsage(t *testing.T) {
	// 步骤 1: 设置包含所有路由和中间件的 echo 实例。
	e := setupEcho()

	// 步骤 2: 使用辅助函数执行请求并获取响应记录器。
	t.Run("GET /user/:id - 成功", func(t *testing.T) {
		// 执行 GET 请求
		rec := Get(e, "/user/1", nil)

		// 断言 HTTP 状态码
		assert.Equal(t, http.StatusOK, rec.Code)

		// 使用 BodyJson 辅助方法解析响应体
		body, err := rec.BodyJson()
		assert.NoError(t, err)

		// 断言响应体的内容
		// 注意: json.Unmarshal 会将数字解析为 float64
		assert.Equal(t, float64(1), body["id"])
		assert.Equal(t, "John Doe", body["name"])
		fmt.Printf("GET /user/1 响应: Code=%d, Body=%s\n", rec.Code, rec.Body.String())
	})

	t.Run("GET /user/:id - 未找到", func(t *testing.T) {
		rec := Get(e, "/user/99", nil)
		assert.Equal(t, http.StatusNotFound, rec.Code)

		body, err := rec.BodyJson()
		assert.NoError(t, err)
		assert.Equal(t, "User not found", body["error"])
		fmt.Printf("GET /user/99 响应: Code=%d, Body=%s\n", rec.Code, rec.Body.String())
	})

	t.Run("GET /users - 带查询参数", func(t *testing.T) {
		// 创建查询参数
		params := url.Values{}
		params.Add("name", "Jane Doe")

		rec := Get(e, "/users", params)
		assert.Equal(t, http.StatusOK, rec.Code)

		// 对于返回 JSON 数组的响应，使用 BodyArrayJson
		body, err := rec.BodyArrayJson()
		assert.NoError(t, err)

		assert.Len(t, body, 1)
		assert.Equal(t, float64(2), body[0]["id"])
		assert.Equal(t, "Jane Doe", body[0]["name"])
		fmt.Printf("GET /users?name=Jane+Doe 响应: Code=%d, Body=%s\n", rec.Code, rec.Body.String())
	})

	t.Run("POST /user - 创建用户", func(t *testing.T) {
		// 定义 POST 请求的 JSON 主体
		jsonBody := `{"name": "New User"}`

		rec := Post(e, "/user", jsonBody)
		assert.Equal(t, http.StatusCreated, rec.Code)

		body, err := rec.BodyJson()
		assert.NoError(t, err)
		assert.Equal(t, float64(100), body["id"])
		assert.Equal(t, "New User", body["name"])
		fmt.Printf("POST /user 响应: Code=%d, Body=%s\n", rec.Code, rec.Body.String())
	})
}
