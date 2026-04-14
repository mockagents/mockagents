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
	"strings"
	"sync"
	"time"
)

// ServerOptions configures a subprocess-based mock server.
type ServerOptions struct {
	AgentsDir  string
	Port       int    // 0 = auto-pick a free port
	BinaryPath string // empty = auto-detect via MOCKAGENTS_BIN / repo layout / PATH
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
	logs strings.Builder
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
// Start has allocated a port.
func (s *Server) URL() string {
	return fmt.Sprintf("http://localhost:%d", s.Port)
}

// Client returns a Client pre-configured for this server.
func (s *Server) Client() *Client {
	return NewClient(ClientOptions{BaseURL: s.URL()})
}

// Logs returns everything captured on the subprocess's stdout+stderr
// since the last Start call.
func (s *Server) Logs() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.logs.String()
}

// IsRunning reports whether the subprocess is alive.
func (s *Server) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cmd != nil && s.cmd.Process != nil && (s.cmd.ProcessState == nil || !s.cmd.ProcessState.Exited())
}

// Start spawns the subprocess, picking a free port if Port was zero, and
// blocks until /api/v1/health responds 200 or the timeout elapses.
func (s *Server) Start(ctx context.Context, timeout time.Duration) error {
	s.mu.Lock()
	if s.cmd != nil {
		s.mu.Unlock()
		return errors.New("server already started")
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
	cmd.Stdout = &teeWriter{buf: &s.logs}
	cmd.Stderr = &teeWriter{buf: &s.logs}

	if err := cmd.Start(); err != nil {
		s.mu.Unlock()
		return fmt.Errorf("starting mockagents binary %q: %w", s.BinaryPath, err)
	}
	s.cmd = cmd
	s.mu.Unlock()

	if err := waitForHealth(ctx, s.URL(), timeout); err != nil {
		// Tear down on failed startup so callers don't leak processes.
		_ = s.Stop(5 * time.Second)
		return fmt.Errorf("server did not become ready within %s: %w\nlogs:\n%s", timeout, err, s.Logs())
	}
	return nil
}

// Stop sends SIGTERM (SIGKILL fallback on Windows) and waits up to the
// given timeout for the process to exit. Safe to call on an un-started
// or already-stopped server.
func (s *Server) Stop(timeout time.Duration) error {
	s.mu.Lock()
	cmd := s.cmd
	s.cmd = nil
	s.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	// On Unix, SIGTERM for graceful; on Windows, Kill is the only
	// portable option via os/exec.
	if runtime.GOOS == "windows" {
		_ = cmd.Process.Kill()
	} else {
		_ = cmd.Process.Signal(os.Interrupt)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		<-done
		return fmt.Errorf("server did not exit within %s, killed", timeout)
	}
}

// FindFreePort asks the kernel for an unused TCP port.
func FindFreePort() (int, error) {
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

// FindBinary locates the mockagents binary. Honors MOCKAGENTS_BIN, then
// looks in the repo root relative to the working directory, then falls
// back to the bare binary name (which PATH lookup handles at spawn).
func FindBinary() string {
	if env := os.Getenv("MOCKAGENTS_BIN"); env != "" {
		if _, err := os.Stat(env); err == nil {
			return env
		}
	}
	name := "mockagents"
	if runtime.GOOS == "windows" {
		name = "mockagents.exe"
	}
	cwd, _ := os.Getwd()
	for _, rel := range []string{".", "..", filepath.Join("..", ".."), filepath.Join("..", "..", "..")} {
		candidate := filepath.Join(cwd, rel, name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return name
}

// waitForHealth polls /api/v1/health until it returns 200 or the
// timeout elapses.
func waitForHealth(ctx context.Context, baseURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
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
		time.Sleep(75 * time.Millisecond)
	}
	if lastErr == nil {
		lastErr = errors.New("timeout")
	}
	return lastErr
}

// teeWriter is a thread-safe io.Writer that appends everything to an
// underlying strings.Builder.
type teeWriter struct {
	mu  sync.Mutex
	buf *strings.Builder
}

// Write satisfies io.Writer.
func (t *teeWriter) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.buf.Write(p)
}
