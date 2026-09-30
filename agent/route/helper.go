package route

import (
	"github.com/labstack/echo/v5"
	"github.com/openfoundry/lib/env"
)

const cwdKey = "cwd"

// const chatIdKey = "id"

// GetCwd 获取当前工作目录
func GetAgentCwd(c *echo.Context) string {
	v, err := ReadSession(c, cwdKey)
	if err != nil || v == nil {
		return env.BaseDir()
	}
	return v.(string)
}

// func GetChatId(c *echo.Context) (string, error) {
// 	v, err := ReadSession(c, chatIdKey)
// 	if err != nil {
// 		return "", err
// 	}
// 	if v == nil || v == "" {
// 		return "", errors.New("chat id not found")
// 	}
// 	return v.(string), nil
// }

// func SetChatId(c *echo.Context, id string) error {
// 	return UpdateSession(c, chatIdKey, id)
// }
