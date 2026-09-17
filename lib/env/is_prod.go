//go:build prod

package env

import (
	"os"
)

// -tag !dev 编译时将 dev 设置为 false
func init() {
	os.Setenv("DEV", "false")
}
