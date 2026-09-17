package util

import "time"

var currentTime *time.Time

// SetNowFunc 设置 NowFunc 的值
// 注意只在Test中使用
func SetCurrentTime(curr *time.Time) {
	currentTime = curr
}
func ClearCurrentTime() {
	currentTime = nil
}

func GetCurrentTime() time.Time {
	if currentTime != nil {
		return *currentTime
	}
	return time.Now()
}

func GetCurrentTimePtr() *time.Time {
	if currentTime != nil {
		return currentTime
	}
	t := time.Now()
	return &t
}
