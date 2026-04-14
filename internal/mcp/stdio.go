package mcp

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"sync"
)

// ServeStdio reads line-delimited JSON-RPC requests from r, dispatches
// each through s, and writes the JSON-RPC responses to w followed by a
// newline. It returns when r reaches EOF or a read error occurs. Writes
// are serialized so concurrent notifications (future streaming work)
// cannot interleave with regular responses.
func ServeStdio(s *Server, r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	// Allow up to 10 MiB per MCP frame — some image content blocks get chunky.
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	var writeMu sync.Mutex
	write := func(payload []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		if _, err := w.Write(payload); err != nil {
			return err
		}
		_, err := w.Write([]byte{'\n'})
		return err
	}

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		out, err := s.HandleBytes(line)
		if err != nil {
			return fmt.Errorf("handler error: %w", err)
		}
		if out == nil {
			continue
		}
		if err := write(out); err != nil {
			return err
		}
	}
	return scanner.Err()
}
