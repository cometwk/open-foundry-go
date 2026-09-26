/**
 * File Change Tracker — 对标 Claude Code 的文件历史追踪
 *
 * CC: FileHistoryManager 记录每次文件修改，支持 undo/revert
 * 我们: 简化版 — 记录修改列表，在 /cost 中展示
 *
 * 集成方式: file_edit/file_write 工具在 execute 后调用 TrackFileChange()
 *
 * 与 TS 的差异: TS 的模块级数组在 JS 单线程下天然安全，
 * Go 中工具可能并发执行，因此用 sync.Mutex 保护包级状态。
 */
package engine

import (
	"strconv"
	"strings"
	"sync"
	"time"
)

// FileAction 文件修改动作
type FileAction string

const (
	FileActionEdit   FileAction = "edit"
	FileActionWrite  FileAction = "write"
	FileActionCreate FileAction = "create"
)

// FileChange 一次文件修改记录
type FileChange struct {
	Path      string
	Action    FileAction
	Timestamp int64 // UnixMilli，对应 Date.now()
}

var (
	changesMu sync.Mutex
	changes   []FileChange // 内存中的文件修改记录 (session-scoped)
)

// TrackFileChange 记录一次文件修改
func TrackFileChange(path string, action FileAction) {
	changesMu.Lock()
	defer changesMu.Unlock()
	changes = append(changes, FileChange{Path: path, Action: action, Timestamp: time.Now().UnixMilli()})
}

// GetFileChanges 返回修改记录的副本 (对应 TS 的 [...changes])
func GetFileChanges() []FileChange {
	changesMu.Lock()
	defer changesMu.Unlock()
	out := make([]FileChange, len(changes))
	copy(out, changes)
	return out
}

// GetFileChangeSummary 汇总修改: 按文件分组 (保持首次出现的顺序)，
// 含 create 的文件标记 "+"，否则 "~"
func GetFileChangeSummary() string {
	changesMu.Lock()
	defer changesMu.Unlock()
	if len(changes) == 0 {
		return "No files modified"
	}

	// TS 的 Map 保持插入顺序，Go 用 keys 切片 + 索引模拟
	byFile := make(map[string][]FileAction)
	var order []string
	for _, c := range changes {
		if _, ok := byFile[c.Path]; !ok {
			order = append(order, c.Path)
		}
		byFile[c.Path] = append(byFile[c.Path], c.Action)
	}

	lines := make([]string, 0, len(order))
	for _, path := range order {
		actions := byFile[path]
		marker := "~"
		for _, a := range actions { // actions.includes("create")
			if a == FileActionCreate {
				marker = "+"
				break
			}
		}
		names := make([]string, len(actions))
		for i, a := range actions {
			names[i] = string(a)
		}
		lines = append(lines, "  "+marker+" "+path+" ("+strings.Join(names, ", ")+")")
	}

	return strconv.Itoa(len(changes)) + " change(s) across " + strconv.Itoa(len(byFile)) + " file(s):\n" + strings.Join(lines, "\n")
}

// ClearFileChanges 清空修改记录
func ClearFileChanges() {
	changesMu.Lock()
	defer changesMu.Unlock()
	changes = nil
}
