package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// This file is the shared harness for driving the CLI end to end in-process:
// rootCmd runs with real argv, stdout/stderr are captured, os.Exit calls are
// intercepted (osExit seam), and serve loops are stopped with a fake signal
// (notifySignals seam). Tests that use it must not call t.Parallel: they swap
// process-wide state.

// cliResult is what one CLI invocation produced. Code is the process exit
// code main would have returned: the code passed to osExit, else 2 for a
// RunE error (main's mapping), else 0.
type cliResult struct {
	Stdout string
	Stderr string
	Code   int
	Err    error
}

// exitPanic carries an intercepted osExit code up to execCLI.
type exitPanic struct{ code int }

// resetFlags restores every flag in the command tree to its default. Cobra
// keeps flag values in package variables across Execute calls, so without
// this one test's --format json leaks into the next.
func resetFlags(c *cobra.Command) {
	reset := func(fs *pflag.FlagSet) {
		fs.VisitAll(func(f *pflag.Flag) {
			if sv, ok := f.Value.(pflag.SliceValue); ok {
				_ = sv.Replace(nil)
			} else {
				_ = f.Value.Set(f.DefValue)
			}
			f.Changed = false
		})
	}
	reset(c.Flags())
	reset(c.PersistentFlags())
	for _, sub := range c.Commands() {
		resetFlags(sub)
	}
}

// captureStd runs fn with os.Stdout and os.Stderr redirected to pipes and
// returns what was written to each. Both pipes are drained concurrently so a
// large write cannot deadlock.
func captureStd(fn func()) (string, string) {
	ro, wo, err := os.Pipe()
	if err != nil {
		panic(err)
	}
	re, we, err := os.Pipe()
	if err != nil {
		panic(err)
	}
	var outBuf, errBuf bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = io.Copy(&outBuf, ro) }()
	go func() { defer wg.Done(); _, _ = io.Copy(&errBuf, re) }()

	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = wo, we
	func() {
		defer func() { os.Stdout, os.Stderr = origOut, origErr }()
		fn()
	}()
	_ = wo.Close()
	_ = we.Close()
	wg.Wait()
	_ = ro.Close()
	_ = re.Close()
	return outBuf.String(), errBuf.String()
}

// execCLI runs `mockagents <args...>` in-process. It does not touch *testing.T
// so it can run on a background goroutine for the serve commands.
func execCLI(args ...string) cliResult {
	resetFlags(rootCmd)
	prevExit := osExit
	osExit = func(code int) { panic(exitPanic{code}) }
	defer func() { osExit = prevExit }()
	prevLogger := slog.Default()
	defer slog.SetDefault(prevLogger)

	var res cliResult
	exited := false
	res.Stdout, res.Stderr = captureStd(func() {
		defer func() {
			if r := recover(); r != nil {
				if e, ok := r.(exitPanic); ok {
					res.Code, exited = e.code, true
					return
				}
				panic(r)
			}
		}()
		rootCmd.SetArgs(args)
		defer rootCmd.SetArgs(nil)
		res.Err = rootCmd.ExecuteContext(context.Background())
	})
	if !exited && res.Err != nil {
		res.Code = 2
	}
	return res
}

// runCLI is execCLI for the common synchronous case.
func runCLI(t *testing.T, args ...string) cliResult {
	t.Helper()
	return execCLI(args...)
}

// freePort returns a loopback TCP port that was free a moment ago.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return p
}

// servingCLI is a long-running command started by serveCLI.
type servingCLI struct {
	t    *testing.T
	sigs chan chan<- os.Signal
	done chan cliResult
}

// serveCLI starts a serve command (record, replay, mcp, start) on a
// goroutine and waits until healthURL answers 200. If the command exits
// before it is healthy, the result is returned as early with ok=false.
func serveCLI(t *testing.T, healthURL string, args ...string) (*servingCLI, cliResult, bool) {
	t.Helper()
	s := &servingCLI{t: t, sigs: make(chan chan<- os.Signal, 1), done: make(chan cliResult, 1)}
	prevNotify := notifySignals
	notifySignals = func(c chan<- os.Signal, _ ...os.Signal) { s.sigs <- c }
	t.Cleanup(func() { notifySignals = prevNotify })

	go func() { s.done <- execCLI(args...) }()

	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case res := <-s.done:
			return nil, res, false
		default:
		}
		resp, err := client.Get(healthURL)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return s, cliResult{}, true
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%v: server never became healthy at %s", args, healthURL)
	return nil, cliResult{}, false
}

// stop delivers a shutdown signal and returns the command's result.
func (s *servingCLI) stop(sig os.Signal) cliResult {
	s.t.Helper()
	select {
	case c := <-s.sigs:
		c <- sig
	case res := <-s.done:
		return res
	case <-time.After(10 * time.Second):
		s.t.Fatal("serve loop never registered for signals")
	}
	select {
	case res := <-s.done:
		return res
	case <-time.After(30 * time.Second):
		s.t.Fatal("serve loop did not stop after the signal")
	}
	return cliResult{}
}

// httpDo issues a request and returns status and body.
func httpDo(t *testing.T, method, url, contentType, body string, headers ...string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func loopbackURL(port int, path string) string {
	return fmt.Sprintf("http://127.0.0.1:%d%s", port, path)
}
