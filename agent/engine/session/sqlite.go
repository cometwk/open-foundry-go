/**
 * SQLiteStore 单库实现 — .agent/sessions.db (modernc.org/sqlite, 纯 Go 无 CGO)
 *
 * 表结构:
 *   sessions (id, title, created_at, updated_at, cwd)                  — 元数据, 小行可重写
 *   messages (seq 自增, session_id, message JSON)                        — 一行一条 UIMessage, 只 INSERT
 *
 * 与 FileStore 语义一致: sessionId 必填、messages 增量追加、
 * createdAt/title 首次创建后稳定。消息数不持久化 (派生值)。
 */
package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	aisdk "github.com/grafana/ai-sdk"
	_ "modernc.org/sqlite"
)

// SQLiteStore 单库 .agent/sessions.db；按方法传入的 cwd 定位库文件
type SQLiteStore struct{}

// NewSQLiteStore 创建 SQLite Store
func NewSQLiteStore() *SQLiteStore { return &SQLiteStore{} }

func (s *SQLiteStore) dbPath(cwd string) string {
	return filepath.Join(cwd, ".agent", "sessions.db")
}

// open 打开 (必要时创建并迁移) cwd 对应的库；调用方负责 Close
func (s *SQLiteStore) open(cwd string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(s.dbPath(cwd)), 0o755); err != nil {
		return nil, err
	}
	// busy_timeout: 多请求并发写时等待而非立刻报 SQLITE_BUSY
	db, err := sql.Open("sqlite", "file:"+s.dbPath(cwd)+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	const schema = `
CREATE TABLE IF NOT EXISTS sessions (
	id         TEXT PRIMARY KEY,
	title      TEXT NOT NULL DEFAULT 'Untitled session',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	cwd        TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS messages (
	seq       INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
	message   TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, seq);
`
	_, err := db.Exec(schema)
	return err
}

// SaveSession 事务内: upsert 元数据行 + 逐条 INSERT 消息 (只追加)
func (s *SQLiteStore) SaveSession(ctx context.Context, cwd, sessionId string, messages []aisdk.UIMessage) (*Session, error) {
	if sessionId == "" {
		return nil, fmt.Errorf("sessionId is required")
	}
	db, err := s.open(cwd)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	now := utcNow()

	// 已有会话: 沿用 createdAt，title 保持稳定；新会话: 首次创建
	meta := nextMetadata(s.loadSessionMetadata(db, sessionId), sessionId, cwd, now, messages)
	systemPrompt := snapshotSystemPrompt(ctx, cwd, messages)

	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }() // Commit 后为 no-op

	if _, err := tx.Exec(`INSERT INTO sessions (id, title, created_at, updated_at, cwd)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET title = excluded.title, updated_at = excluded.updated_at`,
		meta.ID, meta.Title, meta.CreatedAt, meta.UpdatedAt, meta.Cwd); err != nil {
		return nil, err
	}
	for _, m := range messages {
		raw, err := json.Marshal(m)
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(`INSERT INTO messages (session_id, message) VALUES (?, ?)`, sessionId, string(raw)); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &Session{Metadata: meta, Messages: messages, SystemPrompt: systemPrompt}, nil
}

// loadSessionMetadata 只读元数据行，供增量更新用；不存在返回 nil
func (s *SQLiteStore) loadSessionMetadata(db *sql.DB, sessionId string) *SessionMetadata {
	var meta SessionMetadata
	err := db.QueryRow(`SELECT id, title, created_at, updated_at, cwd FROM sessions WHERE id = ?`, sessionId).
		Scan(&meta.ID, &meta.Title, &meta.CreatedAt, &meta.UpdatedAt, &meta.Cwd)
	if err != nil {
		return nil
	}
	return &meta
}

// ListSessions 列出所有会话元数据 (最新在前)，无会话返回 (nil, nil)；
// updatedAt 为同一 ISO 格式，字典序即时序；单个损坏行跳过不中断列举
func (s *SQLiteStore) ListSessions(cwd string) ([]SessionMetadata, error) {
	db, err := s.open(cwd)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`SELECT id, title, created_at, updated_at, cwd FROM sessions ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []SessionMetadata
	for rows.Next() {
		var meta SessionMetadata
		if err := rows.Scan(&meta.ID, &meta.Title, &meta.CreatedAt, &meta.UpdatedAt, &meta.Cwd); err != nil {
			continue // skip corrupt rows
		}
		sessions = append(sessions, meta)
	}
	return sessions, rows.Err()
}

// LoadSession 加载指定会话，会话不存在返回 (nil, nil)，读取失败返回 (nil, err)；
// 元数据来自 sessions 行，消息按 seq 顺序来自 messages 行 (损坏行跳过)
func (s *SQLiteStore) LoadSession(cwd, sessionId string) (*Session, error) {
	db, err := s.open(cwd)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var session Session
	err = db.QueryRow(`SELECT id, title, created_at, updated_at, cwd FROM sessions WHERE id = ?`, sessionId).
		Scan(&session.Metadata.ID, &session.Metadata.Title, &session.Metadata.CreatedAt,
			&session.Metadata.UpdatedAt, &session.Metadata.Cwd)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil // 会话不存在不是错误
	}
	if err != nil {
		return nil, err
	}

	rows, err := db.Query(`SELECT message FROM messages WHERE session_id = ? ORDER BY seq`, sessionId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			continue // skip corrupt rows
		}
		var m aisdk.UIMessage
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			continue // skip corrupt rows
		}
		session.Messages = append(session.Messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &session, nil
}

// LoadSessionByIndex 按 ListSessions 的顺序 (最新在前) 加载会话，越界返回 (nil, nil)
func (s *SQLiteStore) LoadSessionByIndex(cwd string, idx int) (*Session, error) {
	return loadSessionByIndex(s, cwd, idx)
}
