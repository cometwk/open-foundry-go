package engine

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFileTrackerTrackAndGet(t *testing.T) {
	ClearFileChanges()
	t.Cleanup(ClearFileChanges)

	before := time.Now().UnixMilli()
	TrackFileChange("a.go", FileActionEdit)
	TrackFileChange("b.go", FileActionWrite)
	TrackFileChange("a.go", FileActionEdit)
	after := time.Now().UnixMilli()

	got := GetFileChanges()
	require.Len(t, got, 3)
	require.Equal(t, "a.go", got[0].Path)
	require.Equal(t, FileActionEdit, got[0].Action)
	require.GreaterOrEqual(t, got[0].Timestamp, before)
	require.LessOrEqual(t, got[0].Timestamp, after)

	// 返回副本: 修改返回值不影响内部状态
	got[0].Path = "mutated"
	require.Equal(t, "a.go", GetFileChanges()[0].Path)
}

func TestFileTrackerSummary(t *testing.T) {
	ClearFileChanges()
	t.Cleanup(ClearFileChanges)

	// 空
	require.Equal(t, "No files modified", GetFileChangeSummary())

	// 按文件分组，保持首次出现顺序；含 create 标 "+"，否则 "~"
	TrackFileChange("cmd/main.go", FileActionEdit)
	TrackFileChange("internal/api/new.go", FileActionWrite)
	TrackFileChange("cmd/main.go", FileActionEdit)
	TrackFileChange("internal/api/new.go", FileActionCreate)

	require.Equal(t,
		"4 change(s) across 2 file(s):\n"+
			"  ~ cmd/main.go (edit, edit)\n"+
			"  + internal/api/new.go (write, create)",
		GetFileChangeSummary())
}

func TestFileTrackerClear(t *testing.T) {
	TrackFileChange("x.go", FileActionEdit)
	ClearFileChanges()

	require.Empty(t, GetFileChanges())
	require.Equal(t, "No files modified", GetFileChangeSummary())

	// 清空后可继续记录
	TrackFileChange("y.go", FileActionCreate)
	require.Len(t, GetFileChanges(), 1)
}

// TestFileTrackerConcurrent 并发读写验证 (go test -race 下验证锁的正确性)
func TestFileTrackerConcurrent(t *testing.T) {
	ClearFileChanges()
	t.Cleanup(ClearFileChanges)

	const goroutines, perG = 8, 100
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				TrackFileChange(fmt.Sprintf("f-%d.go", g), FileActionEdit)
				_ = GetFileChangeSummary()
			}
		}(g)
	}
	wg.Wait()

	require.Len(t, GetFileChanges(), goroutines*perG)
}
