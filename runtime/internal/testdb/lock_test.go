package testdb

import "testing"

func TestDSNLockReentersSameT(t *testing.T) {
	acquire(t)
	acquire(t)
	release(t)
	release(t)
}

func TestDSNLockReleaseMismatchPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on mismatched release")
		}
	}()
	release(t)
}
