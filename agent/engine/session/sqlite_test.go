package session

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSQLiteStoreBehavior(t *testing.T) {
	testStoreBehavior(t, storeFixture{
		store: NewSQLiteStore(),
		// 直接 UPDATE sessions 行的 updated_at，控制 ListSessions 排序
		setUpdatedAt: func(t *testing.T, cwd, id, ts string) {
			t.Helper()
			db, err := sql.Open("sqlite", "file:"+filepath.Join(cwd, ".agent", "sessions.db")+"?_pragma=busy_timeout(5000)")
			require.NoError(t, err)
			defer db.Close()
			_, err = db.Exec(`UPDATE sessions SET updated_at = ? WHERE id = ?`, ts, id)
			require.NoError(t, err)
		},
	})
}

// 库文件落在 .agent/sessions.db；消息只 INSERT 不重写 (行数 = 累计消息数)
func TestSQLiteStoreLayout(t *testing.T) {
	forceDefaultEnv(t)
	cwd := t.TempDir()
	store := NewSQLiteStore()

	_, err := store.SaveSession(t.Context(), cwd, "s1", suiteMessages())
	require.NoError(t, err)
	_, err = store.SaveSession(t.Context(), cwd, "s1", suiteMessages()) // 追加同批
	require.NoError(t, err)

	db, err := sql.Open("sqlite", "file:"+store.dbPath(cwd)+"?_pragma=busy_timeout(5000)")
	require.NoError(t, err)
	defer db.Close()

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM messages WHERE session_id = 's1'`).Scan(&count))
	require.Equal(t, 4, count) // 2 + 2，只追加

	var meta int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&meta))
	require.Equal(t, 1, meta) // 元数据单行 upsert
}

// 消息行损坏 (非法 JSON) 跳过，不阻断其余消息
func TestSQLiteStoreCorruptRowSkipped(t *testing.T) {
	forceDefaultEnv(t)
	cwd := t.TempDir()
	store := NewSQLiteStore()

	_, err := store.SaveSession(t.Context(), cwd, "s1", suiteMessages())
	require.NoError(t, err)

	db, err := sql.Open("sqlite", "file:"+store.dbPath(cwd)+"?_pragma=busy_timeout(5000)")
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO messages (session_id, message) VALUES ('s1', '{not json')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO messages (session_id, message) VALUES ('s1', ?)`, `{"id":"m3","role":"user","parts":[{"type":"text","text":"after corrupt"}]}`)
	require.NoError(t, err)
	_ = db.Close()

	loaded, err := store.LoadSession(cwd, "s1")
	require.NoError(t, err)
	require.NotNil(t, loaded)
	require.Len(t, loaded.Messages, 3) // 损坏行跳过，前后消息保留
	require.Equal(t, "m3", loaded.Messages[2].ID)
}
