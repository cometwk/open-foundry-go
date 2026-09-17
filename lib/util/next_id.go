package util

import (
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/openfoundry/lib/env"
)

var (
	// 将seq和last打包成一个uint64：高54位存储时间戳，低10位存储seq
	seqState uint64
	hostId   string = "0"
	// 为了测试方便，允许 mock 时间函数
	timeFunc = func() int64 { return time.Now().UnixMilli() }
)

const (
	seqBits   = 10
	timeShift = seqBits
	seqMask   = (1 << seqBits) - 1 // 1023, which is 0b1111111111
)

// 从打包的状态中提取last和seq
func unpackSeqState(state uint64) (last int64, seq int64) {
	// 只存储相对于基准时间的毫秒数（相对于2024年1月1日）
	baseTime := int64(1704067200000) // 2024-01-01 00:00:00 UTC in milliseconds
	last = int64(state>>timeShift) + baseTime
	seq = int64(state & seqMask)
	return
}

// 将last和seq打包成uint64
func packSeqState(last int64, seq int64) uint64 {
	// 存储相对时间戳，减少位数需求
	baseTime := int64(1704067200000) // 2024-01-01 00:00:00 UTC in milliseconds
	if last < baseTime {
		// Clock moved backwards before base time, this is not supported.
		panic(fmt.Sprintf("clock moved backwards before base time: %d", last))
	}
	relativeTime := last - baseTime
	return (uint64(relativeTime) << timeShift) | uint64(seq)
}

func nextSeq0(nowMs int64) int64 {
	for {
		oldState := atomic.LoadUint64(&seqState)
		oldLast, oldSeq := unpackSeqState(oldState)

		var newLast, newSeq int64

		if nowMs < oldLast {
			// Clock moved backwards, reject to generate ID and wait for clock to catch up
			return -1
		}

		if nowMs == oldLast {
			newSeq = oldSeq + 1
			// 防止序列号溢出，如果超过999则等待下一毫秒
			if newSeq > 999 {
				return -1
			}
			newLast = oldLast
		} else {
			newLast = nowMs
			newSeq = 0
		}

		newState := packSeqState(newLast, newSeq)

		// 使用CAS操作确保原子性
		if atomic.CompareAndSwapUint64(&seqState, oldState, newState) {
			return newSeq
		}
		// CAS失败，重试
	}
}

var metric_sleep_count int64

func GetSleepCount() int64 {
	return atomic.LoadInt64(&metric_sleep_count)
}

func nextSeq() (int64, int64) {
	nowMs := timeFunc()
	for {
		seq := nextSeq0(nowMs)
		if seq != -1 {
			return nowMs, seq
		}
		time.Sleep(time.Millisecond)
		// 获取新的时间戳，确保时间推进
		nowMs = timeFunc()

		atomic.AddInt64(&metric_sleep_count, 1)
	}
}

func GetNextSeq() (int64, int64) {
	return nextSeq()
}

func NextId0() string {
	return NextId("")
}

// format2Digits formats an integer to 2-digit string with leading zeros
// Optimized to avoid string concatenation overhead
func format2Digits(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// format3Digits formats an integer to 3-digit string with leading zeros
// Optimized to avoid string concatenation overhead
func format3Digits(n int) string {
	if n < 10 {
		return "00" + strconv.Itoa(n)
	}
	if n < 100 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// NextId generates a unique ID string.
// The format is: YYMMDDHHmmssSSS(prefix)(hostId)(seq)
// Example: 240101123000123myprefix0001
func NextId(prefix string) string {
	nowMs, seq := nextSeq()

	// 使用单一时间点确保一致性
	now := time.UnixMilli(nowMs)

	// 使用 strings.Builder 进行高效的字符串拼接，避免 fmt.Sprintf 的性能开销
	var builder strings.Builder
	// 预分配足够的容量：15位时间戳 + prefix + hostId + 3位seq
	builder.Grow(15 + len(prefix) + len(hostId) + 3)

	// YYMMDDHHmmssSSS
	builder.WriteString(format2Digits(int(now.Year() % 100)))
	builder.WriteString(format2Digits(int(now.Month())))
	builder.WriteString(format2Digits(now.Day()))
	builder.WriteString(format2Digits(now.Hour()))
	builder.WriteString(format2Digits(now.Minute()))
	builder.WriteString(format2Digits(now.Second()))
	builder.WriteString(format3Digits(int(nowMs % 1000)))

	// prefix + hostId + seq
	builder.WriteString(prefix)
	builder.WriteString(hostId)
	builder.WriteString(format3Digits(int(seq)))

	return builder.String()
}

func SetHostId(id string) {
	hostId = id
}

func init() {
	hostId = env.String("HOST_ID", "")
	if len(hostId) == 0 {
		hostId = "0"
	}
}
