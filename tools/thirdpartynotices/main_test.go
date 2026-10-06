package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClassify(t *testing.T) {
	for name, want := range map[string]string{
		"LICENSE": "licence", "LICENSE.md": "licence", "license.txt": "licence",
		"LICENSE-MIT": "licence", "LICENSE.APACHE2": "licence", "COPYING": "licence",
		"NOTICE": "notice", "NOTICE.txt": "notice",
		"README.md": "", "LICENSES.go": "", "license_test.go": "",
	} {
		if got := classify(name); got != want {
			t.Errorf("classify(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestParseModulesDeduplicatesAndSorts(t *testing.T) {
	out := []byte("golang.org/x/text v0.39.0 /m/text\ngithub.com/a/b v1.0.0 /m/b\ngolang.org/x/text v0.39.0 /m/text\n\n")
	mods := parseModules(out)
	if len(mods) != 2 || mods[0].Path != "github.com/a/b" || mods[1].Dir != "/m/text" {
		t.Fatalf("mods = %+v", mods)
	}
}

func TestRenderIncludesLicenceAndNoticeAndFailsWithoutLicence(t *testing.T) {
	root := t.TempDir()
	withNotice := filepath.Join(root, "a")
	bare := filepath.Join(root, "b")
	for _, d := range []string{withNotice, bare} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(withNotice, "LICENSE"), []byte("Apache License 2.0 text\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(withNotice, "NOTICE"), []byte("Example notice"), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := render(&buf, []module{{Path: "example.com/a", Version: "v1.2.3", Dir: withNotice}}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"## example.com/a v1.2.3", "### LICENSE", "Apache License 2.0 text", "### NOTICE", "Example notice"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("output missing %q", want)
		}
	}
	if strings.Contains(buf.String(), "\r") {
		t.Error("CRLF must be normalised")
	}

	err := render(&bytes.Buffer{}, []module{{Path: "example.com/b", Version: "v0.1.0", Dir: bare}})
	if !errors.Is(err, errNoLicence) || !strings.Contains(err.Error(), "example.com/b@v0.1.0") {
		t.Fatalf("err = %v, want errNoLicence naming the module", err)
	}
}
