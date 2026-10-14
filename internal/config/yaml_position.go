package config

import (
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// NodeAt traverses a yaml.Node document tree along a dot-separated path
// and returns the value node at that path. Sequence indexes may be written
// either as dot segments ("spec.tools.0.name") or in the bracket form the
// validators build with "%s[%d]" ("spec.tools[0].name", "matrix[1][0]").
// Returns nil if the path cannot be resolved.
func NodeAt(root *yaml.Node, path string) *yaml.Node {
	if root == nil || path == "" {
		return root
	}

	// Unwrap document node.
	node := root
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}

	for _, seg := range strings.Split(path, ".") {
		name, indexes := splitIndexes(seg)
		// A bare "[N]" segment has no key; any other segment (even an empty
		// one) resolves as a key exactly as before.
		if name != "" || len(indexes) == 0 {
			if node = resolveSegment(node, name); node == nil {
				return nil
			}
		}
		for _, idx := range indexes {
			if node = resolveSegment(node, idx); node == nil {
				return nil
			}
		}
	}
	return node
}

// splitIndexes peels the trailing "[N]" groups off a path segment, returning
// the key before them and the indexes in order: "agents[0]" → ("agents",
// ["0"]). A segment whose brackets do not form such a suffix is returned whole
// as a literal key, so an unbalanced or non-numeric bracket never resolves.
func splitIndexes(seg string) (string, []string) {
	var indexes []string
	for strings.HasSuffix(seg, "]") {
		open := strings.LastIndexByte(seg, '[')
		if open < 0 {
			break
		}
		idx := seg[open+1 : len(seg)-1]
		if _, err := strconv.Atoi(idx); err != nil {
			break
		}
		indexes = append([]string{idx}, indexes...)
		seg = seg[:open]
	}
	return seg, indexes
}

func resolveSegment(node *yaml.Node, segment string) *yaml.Node {
	switch node.Kind {
	case yaml.MappingNode:
		// Mapping nodes alternate key, value in Content.
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Value == segment {
				return node.Content[i+1]
			}
		}
	case yaml.SequenceNode:
		idx, err := strconv.Atoi(segment)
		if err != nil || idx < 0 || idx >= len(node.Content) {
			return nil
		}
		return node.Content[idx]
	}
	return nil
}

// LineColOf returns the line and column for a given field path in the YAML tree.
// Returns (0, 0) if the path cannot be resolved.
func LineColOf(root *yaml.Node, path string) (line, col int) {
	node := NodeAt(root, path)
	if node == nil {
		return 0, 0
	}
	return node.Line, node.Column
}
