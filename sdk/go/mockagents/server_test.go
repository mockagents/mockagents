package mockagents

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestLogBufferConcurrentWritersAndReaders(t *testing.T) {
	var logs logBuffer
	const writers, lines = 8, 500
	var wg sync.WaitGroup
	for writer := 0; writer < writers; writer++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for line := 0; line < lines; line++ {
				_, _ = fmt.Fprintf(&logs, "writer-%d-line-%d\n", id, line)
			}
		}(writer)
	}
	for i := 0; i < 100; i++ {
		_ = logs.String()
	}
	wg.Wait()
	got := strings.Split(strings.TrimSpace(logs.String()), "\n")
	if len(got) != writers*lines {
		t.Fatalf("captured %d lines, want %d", len(got), writers*lines)
	}
}

func TestLogBufferReset(t *testing.T) {
	var logs logBuffer
	_, _ = logs.Write([]byte("old run"))
	logs.Reset()
	_, _ = logs.Write([]byte("new run"))
	if got := logs.String(); got != "new run" {
		t.Fatalf("Logs() = %q", got)
	}
}

func TestLogBufferRetainsBoundedTail(t *testing.T) {
	var logs logBuffer
	payload := strings.Repeat("x", maxCapturedLogBytes+100)
	_, _ = logs.Write([]byte(payload))
	if got := len(logs.String()); got != maxCapturedLogBytes {
		t.Fatalf("captured %d bytes", got)
	}
}
