package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRunDriftCriticalAndCompatible(t *testing.T) {
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
