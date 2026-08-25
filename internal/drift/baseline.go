package drift

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

const BaselineVersion = "mockagents-drift-baseline/v1"

// Baseline is a versioned, reviewable set of inputs for one drift comparison.
type Baseline struct {
	Version     string   `json:"version"`
	Name        string   `json:"name"`
	Revision    int      `json:"revision"`
	Operation   string   `json:"operation"`
	Adapter     string   `json:"adapter,omitempty"`
	SDK         string   `json:"sdk"`
	Provider    string   `json:"provider"`
	Mock        string   `json:"mock"`
	IgnorePaths []string `json:"ignore_paths,omitempty"`
}

type BaselineReference struct {
	Name     string `json:"name"`
	Revision int    `json:"revision"`
}

func ParseBaseline(data []byte) (Baseline, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var baseline Baseline
	if err := decoder.Decode(&baseline); err != nil {
		return Baseline{}, fmt.Errorf("invalid drift baseline: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return Baseline{}, fmt.Errorf("invalid drift baseline: %w", err)
	}
	if baseline.Version != BaselineVersion {
		return Baseline{}, fmt.Errorf("unsupported drift baseline version %q (want %s)", baseline.Version, BaselineVersion)
	}
	if strings.TrimSpace(baseline.Name) == "" || baseline.Revision < 1 {
		return Baseline{}, fmt.Errorf("drift baseline requires a name and revision >= 1")
	}
	if strings.TrimSpace(baseline.Operation) == "" || strings.TrimSpace(baseline.SDK) == "" || strings.TrimSpace(baseline.Provider) == "" || strings.TrimSpace(baseline.Mock) == "" {
		return Baseline{}, fmt.Errorf("drift baseline requires operation, sdk, provider, and mock")
	}
	if _, err := normalizeIgnoredPaths(baseline.IgnorePaths); err != nil {
		return Baseline{}, err
	}
	return baseline, nil
}
