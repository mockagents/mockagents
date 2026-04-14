package config

import (
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// NodeAt traverses a yaml.Node document tree along a dot-separated path
// (e.g., "spec.tools.0.name") and returns the value node at that path.
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

	segments := strings.Split(path, ".")
	for _, seg := range segments {
		node = resolveSegment(node, seg)
		if node == nil {
			return nil
		}
	}
	return node
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
