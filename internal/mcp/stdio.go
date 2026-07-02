package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

// maxStdioFrameBytes caps one newline-delimited MCP frame — some image
// content blocks get chunky, but past this the frame is rejected.
const maxStdioFrameBytes = 10 * 1024 * 1024

// ServeStdio reads line-delimited JSON-RPC requests from r, dispatches
// each through s, and writes the JSON-RPC responses to w followed by a
// newline. It returns when r reaches EOF or a read error occurs. Writes
// are serialized so concurrent notifications (future streaming work)
// cannot interleave with regular responses.
//
// An over-long frame earns a -32700 parse error and the loop CONTINUES —
// previously bufio.Scanner hit ErrTooLong and the whole process exited,
// killing the session for every later request (round-10 R10-22).
func ServeStdio(s *Server, r io.Reader, w io.Writer) error {
	br := bufio.NewReaderSize(r, 64*1024)

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

	for {
		frame, tooLong, err := readStdioFrame(br, maxStdioFrameBytes)
		if tooLong {
			resp, _ := json.Marshal(newError(nil, ErrParseError,
				fmt.Sprintf("frame exceeds %d bytes", maxStdioFrameBytes), nil))
			if werr := write(resp); werr != nil {
				return werr
			}
		} else {
			line := bytes.TrimSpace(frame)
			if len(line) > 0 {
				out, herr := s.HandleBytes(line)
				if herr != nil {
					return fmt.Errorf("handler error: %w", herr)
				}
				if out != nil {
					if werr := write(out); werr != nil {
						return werr
					}
				}
				// Deliver any queued server-initiated notifications as their
				// own JSON-RPC frames — stdio is bidirectional by nature, and
				// previously the queue was never drained here so an emitted
				// list_changed was invisible to stdio clients (round-10
				// R10-17).
				for _, n := range s.DrainNotifications() {
					if werr := write(wireNotification(n)); werr != nil {
						return werr
					}
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

// readStdioFrame reads one newline-delimited frame of at most max bytes. When the
// line is longer, the remainder is discarded and tooLong reported so the
// caller can answer with a parse error and keep serving. The returned error
// is the underlying read error (io.EOF at end of input), never
// bufio.ErrBufferFull.
func readStdioFrame(br *bufio.Reader, max int) (frame []byte, tooLong bool, err error) {
	var buf []byte
	for {
		chunk, err := br.ReadSlice('\n')
		if len(buf)+len(chunk) > max {
			// Swallow the rest of the over-long line, then report.
			for err == bufio.ErrBufferFull {
				_, err = br.ReadSlice('\n')
			}
			return nil, true, err
		}
		buf = append(buf, chunk...)
		if err == bufio.ErrBufferFull {
			continue
		}
		return buf, false, err
	}
}
