package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mockagents/mockagents/internal/types"
	"gopkg.in/yaml.v3"
)

// LoadResult holds the parsed agent definition alongside its YAML node tree
// for line-number-aware validation.
type LoadResult struct {
	Definition *types.AgentDefinition
	Node       *yaml.Node
	FilePath   string
}

// PipelineLoadResult holds a parsed pipeline definition.
type PipelineLoadResult struct {
	Definition *types.PipelineDefinition
	Node       *yaml.Node
	FilePath   string
}

// TestSuiteLoadResult holds a parsed test suite definition.
type TestSuiteLoadResult struct {
	Definition *types.TestSuiteDefinition
	Node       *yaml.Node
	FilePath   string
}

// MCPServerLoadResult holds a parsed mock MCP server definition.
type MCPServerLoadResult struct {
	Definition *types.MCPServerDefinition
	Node       *yaml.Node
	FilePath   string
}

// Documents is a bucketed collection of parsed mockagents YAML/JSON files.
type Documents struct {
	Agents     []*LoadResult
	Pipelines  []*PipelineLoadResult
	TestSuites []*TestSuiteLoadResult
	MCPServers []*MCPServerLoadResult
}

// readAndParse reads a file from disk and produces a yaml.Node tree,
// converting JSON bodies to YAML first so line numbers are consistent.
func readAndParse(path string) ([]byte, *yaml.Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil, fmt.Errorf("%s: file is empty", path)
	}
	if isJSON(path) {
		data, err = jsonToYAML(data)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: invalid JSON: %w", path, err)
		}
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, nil, &ParseError{File: path, Err: err}
	}
	return data, &doc, nil
}

// peekKind extracts the top-level `kind` field from a decoded yaml.Node.
// Returns an empty string if the kind is not set.
func peekKind(doc *yaml.Node) string {
	if doc == nil || len(doc.Content) == 0 {
		return ""
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "kind" {
			return root.Content[i+1].Value
		}
	}
	return ""
}

// LoadFile parses a single YAML or JSON agent definition file.
// It performs a two-pass decode: once into a yaml.Node tree (for line numbers)
// and once into the typed AgentDefinition struct.
func LoadFile(path string) (*LoadResult, error) {
	_, doc, err := readAndParse(path)
	if err != nil {
		return nil, err
	}

	var def types.AgentDefinition
	if err := doc.Decode(&def); err != nil {
		return nil, &ParseError{File: path, Err: err}
	}

	return &LoadResult{
		Definition: &def,
		Node:       doc,
		FilePath:   path,
	}, nil
}

// LoadPipelineFile parses a pipeline definition file.
func LoadPipelineFile(path string) (*PipelineLoadResult, error) {
	_, doc, err := readAndParse(path)
	if err != nil {
		return nil, err
	}
	var def types.PipelineDefinition
	if err := doc.Decode(&def); err != nil {
		return nil, &ParseError{File: path, Err: err}
	}
	return &PipelineLoadResult{Definition: &def, Node: doc, FilePath: path}, nil
}

// LoadTestSuiteFile parses a test suite definition file.
func LoadTestSuiteFile(path string) (*TestSuiteLoadResult, error) {
	_, doc, err := readAndParse(path)
	if err != nil {
		return nil, err
	}
	var def types.TestSuiteDefinition
	if err := doc.Decode(&def); err != nil {
		return nil, &ParseError{File: path, Err: err}
	}
	return &TestSuiteLoadResult{Definition: &def, Node: doc, FilePath: path}, nil
}

// LoadMCPServerFile parses a mock MCP server definition file.
func LoadMCPServerFile(path string) (*MCPServerLoadResult, error) {
	_, doc, err := readAndParse(path)
	if err != nil {
		return nil, err
	}
	var def types.MCPServerDefinition
	if err := doc.Decode(&def); err != nil {
		return nil, &ParseError{File: path, Err: err}
	}
	return &MCPServerLoadResult{Definition: &def, Node: doc, FilePath: path}, nil
}

// LoadDir scans a directory for agent definition files (.yaml, .yml, .json)
// and loads each one. Files whose `kind` is not "Agent" (or unset) are
// silently skipped so pipelines and test suites can live in the same
// directory without producing spurious validation errors.
func LoadDir(dir string) ([]*LoadResult, []error) {
	var results []*LoadResult
	var errs []error

	paths, err := listDocumentPaths(dir)
	if err != nil {
		return nil, []error{err}
	}

	for _, path := range paths {
		_, doc, err := readAndParse(path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		kind := peekKind(doc)
		if kind != "" && kind != types.AgentKind {
			continue
		}
		var def types.AgentDefinition
		if err := doc.Decode(&def); err != nil {
			errs = append(errs, &ParseError{File: path, Err: err})
			continue
		}
		results = append(results, &LoadResult{Definition: &def, Node: doc, FilePath: path})
	}

	return results, errs
}

// LoadAllDocuments loads every YAML/JSON file in dir and splits them into
// agents, pipelines, and test suites based on the top-level `kind` field.
// Files with an unrecognized or missing kind are reported as errors.
func LoadAllDocuments(dir string) (*Documents, []error) {
	docs := &Documents{}
	var errs []error

	paths, err := listDocumentPaths(dir)
	if err != nil {
		return docs, []error{err}
	}

	for _, path := range paths {
		_, doc, err := readAndParse(path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		switch peekKind(doc) {
		case types.AgentKind, "":
			var def types.AgentDefinition
			if err := doc.Decode(&def); err != nil {
				errs = append(errs, &ParseError{File: path, Err: err})
				continue
			}
			docs.Agents = append(docs.Agents, &LoadResult{Definition: &def, Node: doc, FilePath: path})
		case types.PipelineKind:
			var def types.PipelineDefinition
			if err := doc.Decode(&def); err != nil {
				errs = append(errs, &ParseError{File: path, Err: err})
				continue
			}
			docs.Pipelines = append(docs.Pipelines, &PipelineLoadResult{Definition: &def, Node: doc, FilePath: path})
		case types.TestSuiteKind:
			var def types.TestSuiteDefinition
			if err := doc.Decode(&def); err != nil {
				errs = append(errs, &ParseError{File: path, Err: err})
				continue
			}
			docs.TestSuites = append(docs.TestSuites, &TestSuiteLoadResult{Definition: &def, Node: doc, FilePath: path})
		case types.MCPServerKind:
			var def types.MCPServerDefinition
			if err := doc.Decode(&def); err != nil {
				errs = append(errs, &ParseError{File: path, Err: err})
				continue
			}
			docs.MCPServers = append(docs.MCPServers, &MCPServerLoadResult{Definition: &def, Node: doc, FilePath: path})
		default:
			errs = append(errs, fmt.Errorf("%s: unrecognized kind %q", path, peekKind(doc)))
		}
	}

	return docs, errs
}

func listDocumentPaths(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading directory %s: %w", dir, err)
	}
	var paths []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".yaml" && ext != ".yml" && ext != ".json" {
			continue
		}
		paths = append(paths, filepath.Join(dir, entry.Name()))
	}
	return paths, nil
}

// ParseError wraps a YAML/JSON parse error with file context.
type ParseError struct {
	File string
	Err  error
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("%s: parse error: %s", e.File, e.Err)
}

func (e *ParseError) Unwrap() error {
	return e.Err
}

func isJSON(path string) bool {
	return strings.ToLower(filepath.Ext(path)) == ".json"
}

func jsonToYAML(data []byte) ([]byte, error) {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	return yaml.Marshal(raw)
}
