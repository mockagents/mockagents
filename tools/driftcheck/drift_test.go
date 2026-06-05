package main

import (
	"testing"

	"gopkg.in/yaml.v3"
)

const sampleSpec = `
openapi: 3.1.0
info:
  license:
    name: Apache-2.0
paths:
  /x:
    get:
      responses:
        "200":
          $ref: '#/components/responses/OK'
        "400":
          $ref: '#/components/responses/Missing'
components:
  responses:
    OK:
      description: fine
`

func decode(t *testing.T, s string) any {
	t.Helper()
	var root any
	if err := yaml.Unmarshal([]byte(s), &root); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return root
}

func TestCollectRefs(t *testing.T) {
	refs := collectRefs(decode(t, sampleSpec))
	if len(refs) != 2 {
		t.Fatalf("collected %d refs, want 2: %v", len(refs), refs)
	}
}

func TestResolveRef(t *testing.T) {
	root := decode(t, sampleSpec)
	cases := []struct {
		ref  string
		want bool
	}{
		{"#/components/responses/OK", true},
		{"#/components/responses/Missing", false}, // referenced but not defined
		{"#/info/license/name", true},
		{"#/components/schemas/Nope", false}, // whole branch absent
		{"#/", true},
	}
	for _, c := range cases {
		if got := resolveRef(root, c.ref); got != c.want {
			t.Errorf("resolveRef(%q) = %v, want %v", c.ref, got, c.want)
		}
	}
}

// TestCheckAPIRefs_RealSpec is the integration guard: the checked-in
// docs/api-spec.yaml must have zero unresolved internal refs.
func TestCheckAPIRefs_RealSpec(t *testing.T) {
	problems := checkAPIRefs("../../docs/api-spec.yaml")
	if len(problems) != 0 {
		t.Errorf("real api-spec.yaml has unresolved refs:\n%v", problems)
	}
}

// TestCheckLicenses_RealRepo guards the live license agreement from the repo root.
func TestCheckLicenses_RealRepo(t *testing.T) {
	problems := checkLicenses("../..")
	if len(problems) != 0 {
		t.Errorf("license drift detected:\n%v", problems)
	}
}
