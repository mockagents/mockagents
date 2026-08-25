package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRunDriftCriticalAndCompatible(t *testing.T) {
	driftIgnorePaths = nil
	dir := t.TempDir()
	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	driftSDKPath = write("sdk.json", `{"id":"x","count":1}`)
	driftProviderPath = write("provider.json", `{"id":"x","count":1}`)
	driftMockPath = write("mock.json", `{"id":"x","count":"1"}`)
	driftOperation, driftFormat, driftOutput = "test.operation", "json", ""
	var out bytes.Buffer
	driftCmd.SetOut(&out)
	if err := runDrift(driftCmd, nil); !errors.Is(err, errCriticalDrift) {
		t.Fatalf("err=%v output=%s", err, out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte(`"severity": "critical"`)) {
		t.Fatalf("output=%s", out.String())
	}
	driftMockPath = driftProviderPath
	driftFormat = "markdown"
	out.Reset()
	if err := runDrift(driftCmd, nil); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out.Bytes(), []byte("No drift detected")) {
		t.Fatalf("output=%s", out.String())
	}
}

func TestRunDriftSARIF(t *testing.T) {
	driftIgnorePaths = nil
	dir := t.TempDir()
	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	driftSDKPath = write("sdk.json", `{"id":"x"}`)
	driftProviderPath = write("provider.json", `{"id":"x"}`)
	driftMockPath = write("mock.json", `{"id":1}`)
	driftOperation, driftAdapter, driftFormat, driftOutput = "test.operation", "internal/adapter/openai.go", "sarif", ""
	var out bytes.Buffer
	driftCmd.SetOut(&out)
	if err := runDrift(driftCmd, nil); !errors.Is(err, errCriticalDrift) {
		t.Fatalf("err=%v output=%s", err, out.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil || doc["version"] != "2.1.0" {
		t.Fatalf("SARIF output=%s err=%v", out.String(), err)
	}
}

func TestRunDriftJUnit(t *testing.T) {
	driftIgnorePaths = nil
	dir := t.TempDir()
	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	driftSDKPath = write("sdk.json", `{"id":"x"}`)
	driftProviderPath = write("provider.json", `{"id":"x"}`)
	driftMockPath = write("mock.json", `{"id":1}`)
	driftOperation, driftAdapter, driftFormat, driftOutput = "test.operation", "internal/adapter/openai.go", "junit", ""
	var out bytes.Buffer
	driftCmd.SetOut(&out)
	if err := runDrift(driftCmd, nil); !errors.Is(err, errCriticalDrift) {
		t.Fatalf("err=%v output=%s", err, out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte(`<testsuite name="MockAgents provider drift: test.operation"`)) ||
		!bytes.Contains(out.Bytes(), []byte(`<failure message="critical provider drift`)) {
		t.Fatalf("JUnit output=%s", out.String())
	}
}

func TestRunDriftIgnoresConfiguredVolatilePaths(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	driftSDKPath = write("sdk.json", `{"id":"x","created":1}`)
	driftProviderPath = write("provider.json", `{"id":"x","created":"now"}`)
	driftMockPath = write("mock.json", `{"id":"x"}`)
	driftOperation, driftFormat, driftOutput = "test.operation", "json", ""
	driftIgnorePaths = []string{"$.created"}
	t.Cleanup(func() { driftIgnorePaths = nil })
	var out bytes.Buffer
	driftCmd.SetOut(&out)
	if err := runDrift(driftCmd, nil); err != nil {
		t.Fatalf("ignored volatile drift failed: %v output=%s", err, out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte(`"findings": []`)) {
		t.Fatalf("output=%s", out.String())
	}
}
