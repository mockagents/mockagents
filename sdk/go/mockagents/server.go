package mockagents

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// ServerOptions configures a subprocess-based mock server.
type ServerOptions struct {
	AgentsDir  string
	Port       int    // 0 = auto-pick a free port
	BinaryPath string // empty = auto-detect via FindBinary (env vars, ./mockagents, PATH)
	LogLevel   string // debug, info, warn, error (default: warn)
}

// Server manages a mockagents binary subprocess.
type Server struct {
	AgentsDir  string
	Port       int
	BinaryPath string
	LogLevel   string

	mu   sync.Mutex
	cmd  *exec.Cmd
	done chan error
	logs logBuffer
}

// NewServer builds a Server (without starting it).
func NewServer(opts ServerOptions) (*Server, error) {
	agentsDir := opts.AgentsDir
	if agentsDir == "" {
		agentsDir = "./agents"
	}
	binary := opts.BinaryPath
	if binary == "" {
		binary = FindBinary()
	}
	logLevel := opts.LogLevel
	if logLevel == "" {
		logLevel = "warn"
	}
	return &Server{
		AgentsDir:  agentsDir,
		Port:       opts.Port,
		BinaryPath: binary,
		LogLevel:   logLevel,
	}, nil
}

// URL returns the base URL the server is listening on. Only valid after
// Start has allocated a port. It uses 127.0.0.1, not localhost: the binary
// binds IPv4 only, and localhost can resolve to ::1 first on dual-stack hosts.
func (s *Server) URL() string {
	return fmt.Sprintf("http://127.0.0.1:%d", s.Port)
}

// Client returns a Client pre-configured for this server.
func (s *Server) Client() *Client {
	return NewClient(ClientOptions{BaseURL: s.URL()})
}

// Logs returns everything captured on the subprocess's stdout+stderr
// since the last Start call.
func (s *Server) Logs() string {
	return s.logs.String()
}

// IsRunning reports whether the subprocess is alive.
func (s *Server) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd == nil || s.cmd.Process == nil || s.done == nil {
		return false
	}
	select {
	case <-s.done:
		return false
	default:
		return true
	}
}

// Start spawns the subprocess, picking a free port if Port was zero, and
// blocks until /api/v1/health responds 200 or the timeout elapses. If the
// subprocess exits first (a bad agents dir, a port collision) Start fails
// immediately with the exit status and the captured logs instead of waiting
// out the timeout.
func (s *Server) Start(ctx context.Context, timeout time.Duration) error {
	s.mu.Lock()
	if s.cmd != nil {
		select {
		case <-s.done:
			s.cmd, s.done = nil, nil
		default:
			s.mu.Unlock()
			return errors.New("server already started")
		}
	}

	if s.Port == 0 {
		port, err := FindFreePort()
		if err != nil {
			s.mu.Unlock()
			return fmt.Errorf("finding free port: %w", err)
		}
		s.Port = port
	}

	args := []string{
		"start",
		"--port", fmt.Sprintf("%d", s.Port),
		"--agents-dir", s.AgentsDir,
		"--log-level", s.LogLevel,
	}
	cmd := exec.Command(s.BinaryPath, args...)
	s.logs.Reset()
	cmd.Stdout = &s.logs
	cmd.Stderr = &s.logs

	if err := cmd.Start(); err != nil {
		s.mu.Unlock()
		return fmt.Errorf("starting mockagents binary %q: %w", s.BinaryPath, err)
	}
	s.cmd = cmd
	s.done = make(chan error, 1)
	done := s.done
	s.mu.Unlock()
	go func() { done <- cmd.Wait(); close(done) }()

	if err := waitForHealth(ctx, s.URL(), timeout, done); err != nil {
		// Tear down on failed startup so callers don't leak processes.
		_ = s.Stop(5 * time.Second)
		return fmt.Errorf("server did not become ready within %s: %w\nlogs:\n%s", timeout, err, s.Logs())
	}
	return nil
}

// Stop sends os.Interrupt (SIGINT) on Unix, or kills the process outright on
// Windows where os/exec cannot deliver a portable interrupt, then waits up to
// the given timeout for the process to exit, falling back to Kill. Safe to
// call on an un-started or already-stopped server.
func (s *Server) Stop(timeout time.Duration) error {
	s.mu.Lock()
	cmd := s.cmd
	done := s.done
	s.mu.Unlock()
	if cmd == nil || cmd.Process == nil || done == nil {
		return nil
	}
	select {
	case <-done:
		s.mu.Lock()
		if s.cmd == cmd {
			s.cmd, s.done = nil, nil
		}
		s.mu.Unlock()
		return nil
	default:
	}

	// On Unix, SIGINT for a graceful shutdown (the binary handles it like
	// SIGTERM); on Windows, Kill is the only portable option via os/exec.
	if runtime.GOOS == "windows" {
		_ = cmd.Process.Kill()
	} else {
		_ = cmd.Process.Signal(os.Interrupt)
	}

	select {
	case <-done:
		s.mu.Lock()
		if s.cmd == cmd {
			s.cmd, s.done = nil, nil
		}
		s.mu.Unlock()
		return nil
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		<-done
		s.mu.Lock()
		if s.cmd == cmd {
			s.cmd, s.done = nil, nil
		}
		s.mu.Unlock()
		return fmt.Errorf("server did not exit within %s, killed", timeout)
	}
}

// FindFreePort asks the kernel for an unused TCP port.
func FindFreePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

// FindBinary locates the mockagents binary. It honors MOCKAGENTS_BINARY, then
// MOCKAGENTS_BIN (the same names, in the same order, as the Python SDK and
// the npx launcher), then ./mockagents in the working directory, then falls
// back to the bare binary name, which PATH lookup handles at spawn.
//
// Parent directories are deliberately not searched: in a nested checkout
// that silently ran whatever stale binary sat a few levels up. Point one of
// the env vars at a monorepo build instead.
func FindBinary() string {
	for _, key := range []string{"MOCKAGENTS_BINARY", "MOCKAGENTS_BIN"} {
		if env := os.Getenv(key); env != "" && isRegularFile(env) {
			return env
		}
	}
	name := "mockagents"
	if runtime.GOOS == "windows" {
		name = "mockagents.exe"
	}
	if cwd, err := os.Getwd(); err == nil {
		if candidate := filepath.Join(cwd, name); isRegularFile(candidate) {
			return candidate
		}
	}
	return name
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// waitForHealth polls /api/v1/health until it returns 200, the timeout
// elapses, or exited yields (the subprocess died before becoming ready).
func waitForHealth(ctx context.Context, baseURL string, timeout time.Duration, exited <-chan error) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		select {
		case err := <-exited:
			return processExitedError(err)
		default:
		}
		reqCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		req, _ := http.NewRequestWithContext(reqCtx, http.MethodGet, baseURL+"/api/v1/health", nil)
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			return nil
		}
		if resp != nil {
			resp.Body.Close()
		}
		if err != nil {
			lastErr = err
		}
		select {
		case err := <-exited:
			return processExitedError(err)
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(75 * time.Millisecond):
		}
	}
	if lastErr == nil {
		lastErr = errors.New("timeout")
	}
	return lastErr
}

func processExitedError(waitErr error) error {
	if waitErr == nil {
		return errors.New("server process exited before becoming ready (exit status 0)")
	}
	return fmt.Errorf("server process exited before becoming ready: %w", waitErr)
}

// logBuffer synchronizes the shared stdout/stderr sink and readers.
type logBuffer struct {
	mu  sync.Mutex
	buf []byte
}

const maxCapturedLogBytes = 8 * 1024 * 1024

// Write satisfies io.Writer.
func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if len(b.buf) > maxCapturedLogBytes {
		copy(b.buf, b.buf[len(b.buf)-maxCapturedLogBytes:])
		b.buf = b.buf[:maxCapturedLogBytes]
	}
	return len(p), nil
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}

func (b *logBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = b.buf[:0]
}
