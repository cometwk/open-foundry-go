//go:build !prod

package env

import (
	"fmt"
	"os"
)

// 默认是开发环境
// -tag dev 编译时将 dev 设置为 true
func init() {
	os.Setenv("DEV", "true")
	fmt.Println("DEV MODE is enabled")
	fmt.Println()
}
