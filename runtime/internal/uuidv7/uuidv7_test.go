package uuidv7

import (
	"encoding/hex"
	"fmt"
	"regexp"
	"sync"
	"testing"
	"time"
)

// uuidv7Regex matches the RFC 9562 textual layout: version 7 in the
// high nibble of byte 6, variant 10xx in the high bits of byte 8.
var uuidv7Regex = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNew_Layout(t *testing.T) {
	for i := 0; i < 32; i++ {
		id := New()
		if !uuidv7Regex.MatchString(id) {
			t.Fatalf("New() = %q, want UUIDv7 textual layout", id)
		}
	}
}

func TestNew_Unique(t *testing.T) {
	const n = 10000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		id := New()
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate UUIDv7 after %d iterations: %s", i, id)
		}
		seen[id] = struct{}{}
	}
}

func TestNew_MonotonicTimestamp(t *testing.T) {
	// UUIDv7 encodes a 48-bit millisecond timestamp in the most-significant
	// 12 hex characters. Across back-to-back calls the prefix must not
	// decrease.
	prev, err := timestampHex(New())
	if err != nil {
		t.Fatalf("timestampHex: %v", err)
	}
	for i := 0; i < 100; i++ {
		id := New()
		ts, err := timestampHex(id)
		if err != nil {
			t.Fatalf("timestampHex(%s): %v", id, err)
		}
		if ts < prev {
			t.Fatalf("UUIDv7 timestamp regressed: prev=%s curr=%s (id=%s)", prev, ts, id)
		}
		prev = ts
	}
}

func TestNew_CrossGoroutineUnique(t *testing.T) {
	const workers = 8
	const perWorker = 2000
	var mu sync.Mutex
	seen := make(map[string]struct{}, workers*perWorker)
	done := make(chan struct{})
	for w := 0; w < workers; w++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for i := 0; i < perWorker; i++ {
				id := New()
				mu.Lock()
				if _, dup := seen[id]; dup {
					mu.Unlock()
					t.Errorf("duplicate UUIDv7 concurrent: %s", id)
					return
				}
				seen[id] = struct{}{}
				mu.Unlock()
			}
		}()
	}
	for w := 0; w < workers; w++ {
		<-done
	}
}

// TestNew_TimestampMatchesNow confirms the timestamp prefix decodes back
// to a value close to wall-clock now. Tolerates the millisecond window
// between the call and the assertion.
func TestNew_TimestampMatchesNow(t *testing.T) {
	before := time.Now().UnixMilli()
	id := New()
	after := time.Now().UnixMilli()
	ts, err := ParseTimestamp(id)
	if err != nil {
		t.Fatalf("ParseTimestamp(%s) err: %v", id, err)
	}
	if ts < before-1 || ts > after+1 {
		t.Fatalf("UUIDv7 timestamp %d outside [%d,%d] window", ts, before, after)
	}
}

// hexOnly strips the hyphen separators from a UUID string and returns the
// lowercased hex digits.
func hexOnly(id string) (string, error) {
	clean := make([]byte, 0, 32)
	for _, ch := range id {
		switch {
		case ch >= '0' && ch <= '9', ch >= 'a' && ch <= 'f':
			clean = append(clean, byte(ch))
		case ch == '-':
			// skip
		default:
			return "", fmt.Errorf("unexpected char %q in UUID %s", ch, id)
		}
	}
	if len(clean) != 32 {
		return "", fmt.Errorf("uuid %s has %d hex digits, want 32", id, len(clean))
	}
	return string(clean), nil
}

// timestampHex returns the leading 12 hex characters of a UUIDv7,
// corresponding to the 48-bit Unix-millisecond timestamp.
func timestampHex(id string) (string, error) {
	raw, err := hexOnly(id)
	if err != nil {
		return "", err
	}
	return raw[:12], nil
}

// ParseTimestamp decodes a UUIDv7's first 12 hex chars into a 48-bit
// Unix-millisecond timestamp.
func ParseTimestamp(id string) (int64, error) {
	prefix, err := timestampHex(id)
	if err != nil {
		return 0, err
	}
	b, err := hex.DecodeString(prefix)
	if err != nil {
		return 0, err
	}
	var ts int64
	for _, by := range b {
		ts = (ts << 8) | int64(by)
	}
	return ts, nil
}
