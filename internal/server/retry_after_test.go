package server

import (
	"testing"
	"time"
)

// TestRetryAfterSeconds_RoundsUp is the audit L-27 guard.
func TestRetryAfterSeconds_RoundsUp(t *testing.T) {
	cases := map[time.Duration]int{
		0:                        1,
		200 * time.Millisecond:   1,
		1 * time.Second:          1,
		1900 * time.Millisecond:  2,
		2001 * time.Millisecond:  3,
		3 * time.Second:          3,
		59500 * time.Millisecond: 60,
	}
	for d, want := range cases {
		if got := retryAfterSeconds(d); got != want {
			t.Errorf("retryAfterSeconds(%v) = %d, want %d", d, got, want)
		}
	}
}
