package testutil

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func MustNextId(path string) string {
	// get filename from path
	filename := filepath.Base(path)
	filename = strings.TrimSuffix(filename, filepath.Ext(filename))
	id, err := NextId(path)
	if err != nil {
		panic(err)
	}
	return filename + id
}

func NextId(path string) (string, error) {
	// 1. 读文件
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// 第一次，初始化为 1
			err = os.WriteFile(path, []byte("1"), 0644)
			return "1", err
		}
		return "0", err
	}

	// 2. 解析
	id, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return "", err
	}

	// 3. 自增并写回
	next := id + 1
	err = os.WriteFile(path, []byte(strconv.FormatInt(next, 10)), 0644)
	if err != nil {
		return "", err
	}

	return strconv.FormatInt(next, 10), nil
}
