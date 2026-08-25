package drift

import (
	"strings"
	"testing"
)

func TestParseBaseline(t *testing.T) {
	baseline, err := ParseBaseline([]byte(`{"version":"mockagents-drift-baseline/v1","name":"cohere-rerank","revision":2,"operation":"cohere.rerank","sdk":"sdk.json","provider":"provider.json","mock":"mock.json","ignore_paths":["$.id"]}`))
	if err != nil || baseline.Name != "cohere-rerank" || baseline.Revision != 2 {
		t.Fatalf("baseline=%+v err=%v", baseline, err)
	}
}

func TestParseBaselineRejectsUnknownVersionAndFields(t *testing.T) {
	for _, document := range []string{
		`{"version":"v2","name":"x","revision":1,"operation":"op","sdk":"s","provider":"p","mock":"m"}`,
		`{"version":"mockagents-drift-baseline/v1","name":"x","revision":1,"operation":"op","sdk":"s","provider":"p","mock":"m","unexpected":true}`,
	} {
		if _, err := ParseBaseline([]byte(document)); err == nil || !strings.Contains(err.Error(), "drift baseline") {
			t.Fatalf("document=%s err=%v", document, err)
		}
	}
}
