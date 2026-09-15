package testdb_test

import (
	"context"
	"testing"

	"github.com/openfoundry/runtime/internal/testdb"
)

func TestSomething(t *testing.T) {
	ctx := context.Background()

	db, cleanup, err := testdb.SetupMySQL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	// test...
	db.Exec("SELECT 1")
}
