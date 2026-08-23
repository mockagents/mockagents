package adapter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mockagents/mockagents/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModerationsCommonServiceFaults(t *testing.T) {
	h := &ModerationsHandler{Faults: types.SearchFaults{PartialResults: &types.SearchPartialResultsFault{MaxResults: 1}}}
	rec := doModerations(t, h, `{"input":["hello","attack"]}`)
	var got ModerationsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got.Results, 1)
	h.Faults = types.SearchFaults{StatusCode: 429}
	rec = doModerations(t, h, `{"input":"hello"}`)
	require.Equal(t, 429, rec.Code)
	var errBody map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errBody))
	require.NotNil(t, errBody["error"])
	h.Faults = types.SearchFaults{MalformedJSON: true}
	rec = doModerations(t, h, `{"input":"hello"}`)
	require.False(t, json.Valid(rec.Body.Bytes()))
}

func doModerations(t *testing.T, h *ModerationsHandler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/v1/moderations", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.HandleModerations(rec, req)
	return rec
}

// --- scoring (white-box) ---

func TestModeration_Determinism(t *testing.T) {
	a := moderateText("kill the process")
	b := moderateText("kill the process")
	assert.Equal(t, a.CategoryScores, b.CategoryScores, "scores must be stable across calls")
	assert.Equal(t, a.Flagged, b.Flagged)
}

func TestModeration_EachCategoryFlaggable(t *testing.T) {
	for _, c := range allModerationCategories {
		kw := moderationKeywords[c][0]
		res := moderateText("please " + kw + " right now")
		assert.Truef(t, res.Categories[c], "category %q should flag on keyword %q (scores=%v)", c, kw, res.CategoryScores)
		assert.Truef(t, res.CategoryScores[c] > moderationFlagThreshold, "category %q score should exceed threshold", c)
		assert.True(t, res.Flagged)
	}
}

func TestModeration_BenignNotFlagged(t *testing.T) {
	benign := []string{
		"the weather is nice today",
		"please help me with my math homework",
		"i really love my cat",
		"great skill and craftsmanship",       // "skill" must NOT trip "kill" (word boundary)
		"the bombe au chocolat was tasty",     // "bombe" must NOT trip "bomb"
		"i went to the black marketing class", // "black marketing" must NOT trip the phrase "black market"
		"how to cheating is bad grammar",      // must NOT trip "how to cheat"
	}
	for _, txt := range benign {
		res := moderateText(txt)
		assert.Falsef(t, res.Flagged, "benign text flagged: %q (scores=%v)", txt, res.CategoryScores)
		for _, c := range allModerationCategories {
			assert.Lessf(t, res.CategoryScores[c], moderationFlagThreshold, "%q category %q", txt, c)
		}
	}
}

func TestModeration_ResponseShapeInvariants(t *testing.T) {
	// All THREE maps must carry every one of the 13 categories, for both benign
	// and harmful input (the OpenAI SDK requires every category_applied_input_types
	// field — a partial/empty map raises a Pydantic ValidationError).
	for _, txt := range []string{"hello there", "kill attack bomb"} {
		res := moderateText(txt)
		assert.Lenf(t, res.Categories, 13, "categories for %q", txt)
		assert.Lenf(t, res.CategoryScores, 13, "category_scores for %q", txt)
		assert.Lenf(t, res.CategoryAppliedInputTypes, 13, "category_applied_input_types for %q", txt)
		for _, c := range allModerationCategories {
			_, inCats := res.Categories[c]
			_, inScores := res.CategoryScores[c]
			assert.Truef(t, inCats && inScores, "category %q missing for %q", c, txt)
			assert.Equalf(t, []string{"text"}, res.CategoryAppliedInputTypes[c], "applied[%q] for %q", c, txt)
		}
	}
	assert.True(t, moderateText("kill attack bomb").Flagged)
	assert.False(t, moderateText("hello there").Flagged)
}

func TestModeration_ScoresInRange(t *testing.T) {
	for _, txt := range []string{"hello", "kill myself", "how to steal a car", "decapitated body"} {
		res := moderateText(txt)
		for c, s := range res.CategoryScores {
			assert.GreaterOrEqualf(t, s, 0.0, "%q cat %q", txt, c)
			assert.LessOrEqualf(t, s, 1.0, "%q cat %q", txt, c)
		}
	}
}

func TestContainsTerm_WordBoundary(t *testing.T) {
	assert.True(t, containsTerm("i will kill now", "kill"))
	assert.False(t, containsTerm("great skill", "kill"), "skill must not match kill")
	assert.True(t, containsTerm("how to build a bomb today", "build a bomb"), "phrase at word boundary")
	assert.False(t, containsTerm("bomber jacket", "bomb"))
	// phrases are boundary-anchored at their outer edges too
	assert.False(t, containsTerm("black marketing seminar", "black market"), "phrase must not match a longer word")
	assert.True(t, containsTerm("the black market is shady", "black market"))
	assert.False(t, containsTerm("anything", ""), "empty term must not match (and must not panic)")
}

// --- input parsing ---

func TestParseModerationInputs(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []string
		err  bool
	}{
		{"string", `"hello"`, []string{"hello"}, false},
		{"string array", `["a","b","c"]`, []string{"a", "b", "c"}, false},
		{"text content part", `[{"type":"text","text":"hi there"}]`, []string{"hi there"}, false},
		{"image part benign", `[{"type":"image_url","image_url":{"url":"http://x"}}]`, []string{""}, false},
		{"mixed parts", `[{"type":"text","text":"a"},{"type":"image_url","image_url":{"url":"u"}}]`, []string{"a", ""}, false},
		{"empty string", `""`, nil, true},
		{"empty array", `[]`, nil, true},
		{"empty array element", `["a",""]`, nil, true},
		{"null array element", `[null]`, nil, true},
		{"empty text part", `[{"type":"text","text":""}]`, nil, true},
		{"missing", ``, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseModerationInputs(json.RawMessage(tc.raw))
			if tc.err {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestParseModerationInputs_BatchTooLarge(t *testing.T) {
	var sb strings.Builder
	sb.WriteByte('[')
	for i := 0; i <= maxModerationInputs; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(`"x"`)
	}
	sb.WriteByte(']')
	_, err := parseModerationInputs(json.RawMessage(sb.String()))
	assert.Error(t, err)
}

// --- HTTP handler ---

func TestHandleModerations_DefaultModelAndID(t *testing.T) {
	rec := doModerations(t, &ModerationsHandler{}, `{"input":"hello world"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp ModerationsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "omni-moderation-latest", resp.Model)
	assert.True(t, strings.HasPrefix(resp.ID, "modr-"), "id: %s", resp.ID)
	require.Len(t, resp.Results, 1)
	assert.False(t, resp.Results[0].Flagged)
}

func TestHandleModerations_ModelEchoed(t *testing.T) {
	rec := doModerations(t, &ModerationsHandler{}, `{"input":"hi","model":"omni-moderation-2024-09-26"}`)
	var resp ModerationsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "omni-moderation-2024-09-26", resp.Model)
}

func TestHandleModerations_HarmfulFlaggedMultiInput(t *testing.T) {
	rec := doModerations(t, &ModerationsHandler{}, `{"input":["have a nice day","i will kill and attack you"]}`)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp ModerationsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Results, 2)
	assert.False(t, resp.Results[0].Flagged, "benign input 0")
	assert.True(t, resp.Results[1].Flagged, "harmful input 1")
	assert.True(t, resp.Results[1].Categories["violence"])
}

func TestHandleModerations_Errors(t *testing.T) {
	cases := []struct {
		name string
		body string
		code int
	}{
		{"missing input", `{}`, http.StatusBadRequest},
		{"empty string", `{"input":""}`, http.StatusBadRequest},
		{"empty array", `{"input":[]}`, http.StatusBadRequest},
		{"malformed json", `{"input":`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doModerations(t, &ModerationsHandler{}, tc.body)
			assert.Equal(t, tc.code, rec.Code)
			var errResp map[string]any
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errResp))
			assert.NotNil(t, errResp["error"])
		})
	}
}

func TestHandleModerations_OversizeBody413(t *testing.T) {
	huge := `{"input":"` + strings.Repeat("a", maxDecodeBodyBytes+1024) + `"}`
	rec := doModerations(t, &ModerationsHandler{}, huge)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

func TestModerationsRoute_MountedInRegistry(t *testing.T) {
	mux := http.NewServeMux()
	for _, a := range DefaultRegistry(testEngine()).Adapters() {
		for _, route := range a.Routes() {
			mux.HandleFunc(route.Pattern, route.Handler)
		}
	}
	req := httptest.NewRequest("POST", "/v1/moderations", strings.NewReader(`{"input":"mounted check"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp ModerationsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.True(t, strings.HasPrefix(resp.ID, "modr-"))
	require.Len(t, resp.Results, 1)
}
