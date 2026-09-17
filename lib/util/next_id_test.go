package util

import (
	"fmt"
	"regexp"
	"sync"
	"testing"
	"time"
)

func TestPack(t *testing.T) {

	ms := time.Now().UnixMilli()
	println(ms, 123)
	pack := packSeqState(ms, 123)
	println(pack)
	last, seq := unpackSeqState(pack)
	println(last, seq)
}
func TestNextId(t *testing.T) {
	prefix := "p"
	hostId := "h"
	SetHostId(hostId)

	println(NextId(prefix))
	println(NextId(prefix))
}

// TestNextId_Correctness tests for uniqueness, monotonicity, and format of the generated IDs.
// It also implicitly tests the sequence number rollover by generating more than 1000 IDs in a tight loop.
func TestNextId_Correctness(t *testing.T) {
	prefix := "p"
	hostId := "h"
	SetHostId(hostId)

	count := 2900
	ids := make([]string, count)
	idSet := make(map[string]bool, count)

	for i := 0; i < count; i++ {
		id := NextId(prefix)
		ids[i] = id

		if _, exists := idSet[id]; exists {
			t.Fatalf("Duplicate ID generated: %s", id)
		}
		idSet[id] = true
	}

	println(metric_sleep_count)

	// Check monotonicity
	for i := 1; i < count; i++ {
		if ids[i-1] >= ids[i] {
			t.Fatalf("IDs are not monotonically increasing: %s >= %s", ids[i-1], ids[i])
		}
	}

	// Check format of the last ID
	lastID := ids[count-1]
	// The format string is: YYMMDDHHMMSS(3) + prefix + hostId + seq(3)
	// Example: 240102103050123ph001
	regexPattern := fmt.Sprintf(`^\d{15}%s%s\d{3}$`, regexp.QuoteMeta(prefix), regexp.QuoteMeta(hostId))
	re := regexp.MustCompile(regexPattern)

	if !re.MatchString(lastID) {
		t.Fatalf("ID format is incorrect. ID: %s, Pattern: %s", lastID, regexPattern)
	}
}

// TestNextId_Concurrency tests that NextId is safe to be called from multiple goroutines concurrently
// and that all generated IDs are unique.
func TestNextId_Concurrency(t *testing.T) {
	SetHostId("conc")
	numGoroutines := 100
	idsPerGoroutine := 200
	totalIds := numGoroutines * idsPerGoroutine
	idChan := make(chan string, totalIds)
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < idsPerGoroutine; j++ {
				idChan <- NextId("cg")
			}
		}()
	}

	wg.Wait()
	close(idChan)

	idSet := make(map[string]bool, totalIds)
	for id := range idChan {
		if _, exists := idSet[id]; exists {
			t.Fatalf("Duplicate ID generated in concurrent test: %s", id)
		}
		idSet[id] = true
	}

	if len(idSet) != totalIds {
		t.Fatalf("Expected %d unique IDs, but got %d", totalIds, len(idSet))
	}
}

// TestNextId_ClockBackwards tests that the generator waits if the clock moves backwards.
func TestNextId_ClockBackwards(t *testing.T) {
	// 备份原始的 timeFunc
	origTimeFunc := timeFunc
	defer func() { timeFunc = origTimeFunc }()

	// 模拟时间
	var mockTime int64 = 1716964000000 // 任意起始时间

	// 设置 mock 函数
	// 注意：由于 NextId 内部可能会 sleep，我们需要让 mockTime 能够推进
	// 或者在测试中控制它
	mu := sync.Mutex{}
	timeFunc = func() int64 {
		mu.Lock()
		defer mu.Unlock()
		return mockTime
	}

	// 1. 生成第一个 ID，确立 base time
	id1 := NextId("cb")
	fmt.Printf("ID1: %s\n", id1)

	// 2. 模拟时钟回拨 500ms
	mu.Lock()
	mockTime -= 500
	mu.Unlock()

	// 3. 尝试生成第二个 ID
	// 由于时钟回拨，内部循环会检测到并等待（sleep）
	// 为了让测试能结束，我们需要在另一个 goroutine 中把时间推回去
	done := make(chan struct{})
	go func() {
		// 生成 ID，应该会阻塞直到时间追上
		id2 := NextId("cb")
		fmt.Printf("ID2: %s\n", id2)
		close(done)
	}()

	// 模拟时间流逝，直到追上 id1 的时间
	// 每次推进 100ms
	for i := 0; i < 10; i++ {
		select {
		case <-done:
			// 提前结束（不应该发生，因为时间还没追上）
			if i < 5 {
				t.Error("NextId returned too early, clock backwards check failed?")
			}
			return
		default:
			time.Sleep(50 * time.Millisecond) // 真实时间等待
			mu.Lock()
			mockTime += 100 // 模拟时间推进
			fmt.Printf("Mock time advanced to: %d\n", mockTime)
			mu.Unlock()
		}
	}

	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("NextId timed out waiting for clock to catch up")
	}
}
