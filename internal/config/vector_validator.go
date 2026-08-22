package config

import (
	"fmt"
	"math"
	"strings"

	"github.com/mockagents/mockagents/internal/types"
	"github.com/mockagents/mockagents/internal/vector"
	"gopkg.in/yaml.v3"
)

func ValidateVectorCollection(def *types.VectorCollectionDefinition, filePath string, node *yaml.Node) *ValidationErrorList {
	ctx := &validationContext{file: filePath, node: node}
	if def.APIVersion != types.AgentAPIVersion {
		ctx.addError("apiVersion", fmt.Sprintf("unsupported version %q", def.APIVersion),
			fmt.Sprintf("Use apiVersion: %s", types.AgentAPIVersion))
	}
	if def.Kind != types.VectorCollectionKind {
		ctx.addError("kind", fmt.Sprintf("unsupported kind %q", def.Kind),
			"Use kind: VectorCollection")
	}
	if def.Metadata.Name == "" || !metadataNameRe.MatchString(def.Metadata.Name) {
		ctx.addError("metadata.name", "collection name must be lowercase kebab-case",
			"Use lowercase letters, numbers, and hyphens only.")
	}
	if def.Spec.Dimension <= 0 || def.Spec.Dimension > vector.MaxDimensions {
		ctx.addError("spec.dimension", fmt.Sprintf("dimension must be between 1 and %d", vector.MaxDimensions), "")
	}
	metric := strings.ToLower(def.Spec.Metric)
	if metric != string(vector.Cosine) && metric != string(vector.Dot) && metric != string(vector.Euclidean) {
		ctx.addError("spec.metric", fmt.Sprintf("unsupported metric %q", def.Spec.Metric),
			"Use cosine, dot, or euclidean.")
	}
	if len(def.Spec.Points) > vector.MaxPoints {
		ctx.addError("spec.points", fmt.Sprintf("point count exceeds maximum %d", vector.MaxPoints), "")
	}
	if partial := def.Spec.Faults.PartialResults; partial != nil && (partial.MaxResults < 0 || partial.MaxResults > vector.MaxTopK) {
		ctx.addError("spec.faults.partial_results.max_results",
			fmt.Sprintf("max_results must be between 0 and %d", vector.MaxTopK), "")
	}
	ids := make(map[string]struct{}, len(def.Spec.Points))
	for i, point := range def.Spec.Points {
		field := fmt.Sprintf("spec.points.%d", i)
		key, err := VectorPointKey(point.ID)
		if err != nil {
			ctx.addError(field+".id", err.Error(), "Use a non-empty string or non-negative integer.")
		} else if _, duplicate := ids[key]; duplicate {
			ctx.addError(field+".id", fmt.Sprintf("duplicate point id %v", point.ID), "Point ids must be unique.")
		} else {
			ids[key] = struct{}{}
		}
		if len(point.Vector) != def.Spec.Dimension {
			ctx.addError(field+".vector",
				fmt.Sprintf("dimension mismatch: got %d values, want %d", len(point.Vector), def.Spec.Dimension), "")
		}
		for _, value := range point.Vector {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				ctx.addError(field+".vector", "vector values must be finite", "")
				break
			}
		}
	}
	if len(ctx.errors) == 0 {
		return nil
	}
	return &ValidationErrorList{Errors: ctx.errors}
}

// VectorPointKey canonicalizes YAML/JSON point ids without losing the
// distinction between numeric 1 and string "1".
func VectorPointKey(id any) (string, error) {
	switch value := id.(type) {
	case string:
		if value == "" {
			return "", fmt.Errorf("point id must not be empty")
		}
		return "s:" + value, nil
	case int:
		if value < 0 {
			return "", fmt.Errorf("numeric point id must be non-negative")
		}
		return fmt.Sprintf("n:%d", value), nil
	case int64:
		if value < 0 {
			return "", fmt.Errorf("numeric point id must be non-negative")
		}
		return fmt.Sprintf("n:%d", value), nil
	case uint64:
		return fmt.Sprintf("n:%d", value), nil
	case float64:
		if value < 0 || value != math.Trunc(value) || value > math.MaxInt64 {
			return "", fmt.Errorf("numeric point id must be a non-negative integer")
		}
		return fmt.Sprintf("n:%.0f", value), nil
	default:
		return "", fmt.Errorf("point id must be a string or non-negative integer")
	}
}
