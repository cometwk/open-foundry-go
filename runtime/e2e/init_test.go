package e2e_test

import "testing"

// TestInitGoldDB rebuilds mapped tables and loads seeds/simple.yaml, then
// leaves the database in place. It only runs when E2E_DB_MODE=init.
func TestInitGoldDB(t *testing.T) {
	if requireDBMode(t) != dbModeInit {
		t.Skip("set E2E_DB_MODE=init to rebuild MySQL tables and load library seed")
	}
	env := prepareGoldStorage(t, dbModeInit)
	t.Logf("storage backend = %s; seeded gold tenant (14 objects, 21 links)", env.Backend)
}
