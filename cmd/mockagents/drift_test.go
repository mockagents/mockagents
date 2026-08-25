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
	driftExceptions = ""
	driftIgnorePaths = nil
	driftSDKHeaders, driftProvHeaders, driftMockHeaders = "", "", ""
	driftSDKEnums, driftProvEnums, driftMockEnums = "", "", ""
	driftSDKEvents, driftProvEvents, driftMockEvents = "", "", ""
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

func TestRunDriftAppliesExpiringExceptions(t *testing.T) {
	driftIgnorePaths = nil
	driftSDKHeaders, driftProvHeaders, driftMockHeaders = "", "", ""
	driftSDKEnums, driftProvEnums, driftMockEnums = "", "", ""
	driftSDKEvents, driftProvEvents, driftMockEvents = "", "", ""
	driftSDKErrors, driftProvErrors, driftMockErrors = "", "", ""
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
	driftExceptions = write("exceptions.json", `{"version":"mockagents-drift-exceptions/v1","exceptions":[{"operation":"test.operation","path":"$.id","rule":"sdk-mock-type-mismatch","owner":"sdk-team","expires":"2099-01-01"}]}`)
	driftOperation, driftFormat, driftOutput = "test.operation", "json", ""
	t.Cleanup(func() { driftExceptions = "" })
	var out bytes.Buffer
	driftCmd.SetOut(&out)
	if err := runDrift(driftCmd, nil); err != nil {
		t.Fatalf("err=%v output=%s", err, out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte(`"applied_exceptions"`)) || !bytes.Contains(out.Bytes(), []byte(`"owner": "sdk-team"`)) {
		t.Fatalf("output=%s", out.String())
	}
}

func TestRunDriftSARIF(t *testing.T) {
	driftIgnorePaths = nil
	driftSDKHeaders, driftProvHeaders, driftMockHeaders = "", "", ""
	driftSDKEnums, driftProvEnums, driftMockEnums = "", "", ""
	driftSDKEvents, driftProvEvents, driftMockEvents = "", "", ""
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
	driftSDKHeaders, driftProvHeaders, driftMockHeaders = "", "", ""
	driftSDKEnums, driftProvEnums, driftMockEnums = "", "", ""
	driftSDKEvents, driftProvEvents, driftMockEvents = "", "", ""
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
	driftSDKHeaders, driftProvHeaders, driftMockHeaders = "", "", ""
	driftSDKEnums, driftProvEnums, driftMockEnums = "", "", ""
	driftSDKEvents, driftProvEvents, driftMockEvents = "", "", ""
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

func TestRunDriftComparesCaseInsensitiveHeaders(t *testing.T) {
	driftSDKEnums, driftProvEnums, driftMockEnums = "", "", ""
	driftSDKEvents, driftProvEvents, driftMockEvents = "", "", ""
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
	driftMockPath = write("mock.json", `{"id":"x"}`)
	driftSDKHeaders = write("sdk-headers.json", `{"Content-Type":"application/json"}`)
	driftProvHeaders = write("provider-headers.json", `{"content-type":"application/json"}`)
	driftMockHeaders = write("mock-headers.json", `{"CONTENT-TYPE":["application/json"]}`)
	driftOperation, driftFormat, driftOutput, driftIgnorePaths = "test.operation", "json", "", nil
	t.Cleanup(func() { driftSDKHeaders, driftProvHeaders, driftMockHeaders = "", "", "" })
	var out bytes.Buffer
	driftCmd.SetOut(&out)
	if err := runDrift(driftCmd, nil); !errors.Is(err, errCriticalDrift) {
		t.Fatalf("err=%v output=%s", err, out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte(`"path": "$headers.content-type"`)) {
		t.Fatalf("output=%s", out.String())
	}
}

func TestRunDriftComparesEnumInventories(t *testing.T) {
	driftSDKEvents, driftProvEvents, driftMockEvents = "", "", ""
	dir := t.TempDir()
	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	driftSDKPath = write("sdk.json", `{"status":"queued"}`)
	driftProviderPath = write("provider.json", `{"status":"queued"}`)
	driftMockPath = write("mock.json", `{"status":"queued"}`)
	driftSDKEnums = write("sdk-enums.json", `{"$.status":["queued","done"]}`)
	driftProvEnums = write("provider-enums.json", `{"$.status":["queued","done","running"]}`)
	driftMockEnums = write("mock-enums.json", `{"$.status":["queued"]}`)
	driftSDKHeaders, driftProvHeaders, driftMockHeaders = "", "", ""
	driftOperation, driftFormat, driftOutput, driftIgnorePaths = "test.operation", "json", "", nil
	t.Cleanup(func() { driftSDKEnums, driftProvEnums, driftMockEnums = "", "", "" })
	var out bytes.Buffer
	driftCmd.SetOut(&out)
	if err := runDrift(driftCmd, nil); !errors.Is(err, errCriticalDrift) {
		t.Fatalf("err=%v output=%s", err, out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte(`"rule": "sdk-required-enum-missing"`)) ||
		!bytes.Contains(out.Bytes(), []byte(`"rule": "provider-only-enum-value"`)) {
		t.Fatalf("output=%s", out.String())
	}
}

func TestRunDriftComparesStreamEventOrder(t *testing.T) {
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
	driftMockPath = write("mock.json", `{"id":"x"}`)
	driftSDKEvents = write("sdk-events.json", `["created","delta","done"]`)
	driftProvEvents = write("provider-events.json", `["created","delta","done"]`)
	driftMockEvents = write("mock-events.json", `["created","done","delta"]`)
	driftSDKHeaders, driftProvHeaders, driftMockHeaders = "", "", ""
	driftSDKEnums, driftProvEnums, driftMockEnums = "", "", ""
	driftOperation, driftFormat, driftOutput, driftIgnorePaths = "test.operation", "json", "", nil
	t.Cleanup(func() { driftSDKEvents, driftProvEvents, driftMockEvents = "", "", "" })
	var out bytes.Buffer
	driftCmd.SetOut(&out)
	if err := runDrift(driftCmd, nil); !errors.Is(err, errCriticalDrift) {
		t.Fatalf("err=%v output=%s", err, out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte(`"path": "$events"`)) ||
		!bytes.Contains(out.Bytes(), []byte(`"rule": "sdk-mock-event-order-mismatch"`)) {
		t.Fatalf("output=%s", out.String())
	}
}

func TestRunDriftComparesErrorContracts(t *testing.T) {
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
	driftMockPath = write("mock.json", `{"id":"x"}`)
	driftSDKErrors = write("sdk-errors.json", `{"rate_limit":{"status":429,"code":"rate_limit","body":{"error":{"message":"wait"}}}}`)
	driftProvErrors = write("provider-errors.json", `{"rate_limit":{"status":429,"code":"rate_limit","body":{"error":{"message":"wait"}}}}`)
	driftMockErrors = write("mock-errors.json", `{"rate_limit":{"status":400,"code":"bad_request","body":{"error":{"message":12}}}}`)
	driftSDKHeaders, driftProvHeaders, driftMockHeaders = "", "", ""
	driftSDKEnums, driftProvEnums, driftMockEnums = "", "", ""
	driftSDKEvents, driftProvEvents, driftMockEvents = "", "", ""
	driftOperation, driftFormat, driftOutput, driftIgnorePaths = "test.operation", "json", "", nil
	t.Cleanup(func() { driftSDKErrors, driftProvErrors, driftMockErrors = "", "", "" })
	var out bytes.Buffer
	driftCmd.SetOut(&out)
	if err := runDrift(driftCmd, nil); !errors.Is(err, errCriticalDrift) {
		t.Fatalf("err=%v output=%s", err, out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte(`"path": "$errors.rate_limit"`)) ||
		!bytes.Contains(out.Bytes(), []byte(`"rule": "sdk-mock-error-status-mismatch"`)) ||
		!bytes.Contains(out.Bytes(), []byte(`"path": "$errors.rate_limit.body.error.message"`)) {
		t.Fatalf("output=%s", out.String())
	}
}
