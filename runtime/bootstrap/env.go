package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

func LoadEnv(envFile string) error {
	if envFile != "" {
		if err := godotenv.Load(envFile); err != nil {
			return fmt.Errorf("加载 %s 文件失败: %v", envFile, err)
		}
		return nil
	}

	// 尝试多个可能的路径
	candidates := []string{
		expandHome("~/.foundry/env"),
		".env",
		"../.env",
		"../../.env",
		"../../.env",
		"../../../.env",
		"../../../../.env",
		"../../../../../.env",
	}

	for _, path := range candidates {
		if err := godotenv.Load(path); err == nil {
			fullPath, err := filepath.Abs(path)
			if err != nil {
				return fmt.Errorf("获取绝对路径失败: %v", err)
			}
			fmt.Printf("加载 env 文件成功: %s\n", fullPath)
			return nil
		}
	}

	return fmt.Errorf("env 文件未找到，请检查环境变量 FOUNDRY_CONFIG 或配置文件路径")
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
	return expandHome(baseDir)
}

// expandHome 用于将 ~/开头的路径替换为当前用户的真实 Home 目录
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path // 如果获取失败，降级返回原路径
	}
	return filepath.Join(home, path[2:])
}
