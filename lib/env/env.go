package env

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

func init() {
	// 尝试多个可能的路径
	paths := []string{
		".env",
		"../.env",
		"../../.env",
		"../../.env",
		"../../../.env",
		"../../../../.env",
		"../../../../../.env",
	}

	for _, path := range paths {
		if err := godotenv.Load(path); err == nil {
			fullPath, err := filepath.Abs(path)
			if err != nil {
				panic(fmt.Sprintf("获取绝对路径失败: %v", err))
			}
			fmt.Printf("加载 .env 文件成功: %s => %s\n", path, fullPath)
			return
		}
	}

	panic("加载 .env 文件失败")
}

func Check(key string, description string) {
	value := os.Getenv(key)
	if value == "" {
		panic(fmt.Sprintf("环境变量 %s 不能为空: %s", key, description))
	}
}

func String(key string, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

func MustString(key string) string {
	value := os.Getenv(key)
	if value == "" {
		panic(fmt.Sprintf("环境变量 %s 不能为空", key))
	}
	return value
}

func Int(key string, defaultValue int) int {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	i, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}
	return i
}

func Bool(key string, defaultValue bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value == "true"
}

func MustBool(key string) bool {
	value := os.Getenv(key)
	if value == "" {
		panic(fmt.Sprintf("环境变量 %s 不能为空", key))
	}
	return value == "true"
}

func IsDev() bool {
	return os.Getenv("DEV") == "true"
}

func IsProd() bool {
	return !IsDev()
}

func IsDebug() bool {
	return Bool("DEBUG", false)
}

// 将相对路径转换为相对于 BASE_DIR 的绝对路径
func DirPath(key string, defaultValue string) string {
	dir := String(key, defaultValue)
	if filepath.IsAbs(dir) {
		panic(fmt.Sprintf("环境变量 %s 不能为绝对路径: %s", key, dir))
	}
	return filepath.Join(BaseDir(), dir)
}

func MustDirPath(key string) string {
	dir := String(key, "")
	if dir == "" {
		panic(fmt.Sprintf("环境变量 %s 不能为空", key))
	}
	return DirPath(key, dir)
}

func BaseDir() string {
	baseDir := String("BASE_DIR", ".")
	return tildeExpand(baseDir)
}

// 展开 ~(tilde) 字符，例如 ~/log 展开为 $HOME/log
func tildeExpand(p string) string {
	usr, _ := user.Current()
	dir := usr.HomeDir

	if p == "~" {
		p = dir
	} else if strings.HasPrefix(p, "~/") {
		p = filepath.Join(dir, p[2:])
	}
	return p
}
