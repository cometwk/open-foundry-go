package env

import (
	"fmt"
	"os"
	"testing"
)

func TestGetString(t *testing.T) {
	t.Run("存在的环境变量", func(t *testing.T) {
		fmt.Printf("NAME: %s\n", os.Getenv("NAME"))
	})

	t.Run("默认DEV=true", func(t *testing.T) {
		fmt.Printf("DEV: %s\n", os.Getenv("DEV"))
	})
}
