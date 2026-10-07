package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
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

type VectorCollectionLoadResult struct {
	Definition *types.VectorCollectionDefinition
	Node       *yaml.Node
	FilePath   string
}

type SearchServiceLoadResult struct {
	Definition *types.SearchServiceDefinition
	Node       *yaml.Node
	FilePath   string
}

// Documents is a bucketed collection of parsed mockagents YAML/JSON files.
type Documents struct {
	Agents         []*LoadResult
	Pipelines      []*PipelineLoadResult
	TestSuites     []*TestSuiteLoadResult
	MCPServers     []*MCPServerLoadResult
	A2AServers     []*A2AServerLoadResult
	Vectors        []*VectorCollectionLoadResult
	SearchServices []*SearchServiceLoadResult
}

// A2AServerLoadResult is a parsed kind:A2AServer document (NF-04).
type A2AServerLoadResult struct {
	Definition *types.A2AServerDefinition
	Node       *yaml.Node
	FilePath   string
}

// readAndParse reads a file from disk and produces a yaml.Node tree,
// converting JSON bodies to YAML first so line numbers are consistent.
func readAndParse(path string) ([]byte, *yaml.Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("reading %s: %w", path, err)
	}
	// A UTF-8 byte-order mark (Windows PowerShell 5's `-Encoding utf8`, older
	// Notepad) is valid in YAML but made encoding/json reject a .json file.
	data = bytes.TrimPrefix(data, utf8BOM)
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil, fmt.Errorf("%s: file is empty", path)
	}
	if isJSON(path) {
		data, err = jsonToYAML(data)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: invalid JSON: %w", path, err)
		}
	}
	doc, err := parseSingleDocument(data)
	if err != nil {
		return nil, nil, &ParseError{File: path, Err: err}
	}
	return data, doc, nil
}

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// errMultipleDocuments reports a file that holds more than one YAML document.
// Only the first document used to be decoded, so everything after the first
// `---` was silently never validated or served: `validate` passed a file whose
// second agent was broken, and `start` never served it. Multi-document files
// are rejected rather than supported because every write path (the agent
// write API, MCP management, --watch reload) owns one source file per
// definition and would destroy sibling documents on the next save.
type errMultipleDocuments struct{ line int }

func (e errMultipleDocuments) Error() string {
	return fmt.Sprintf("line %d: a second YAML document starts here; put each definition in its own file", e.line)
}

// parseSingleDocument parses data into a yaml.Node tree and fails when the
// input holds more than one non-empty document.
func parseSingleDocument(data []byte) (*yaml.Node, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return &doc, nil
		}
		return nil, err
	}
	for {
		var next yaml.Node
		err := dec.Decode(&next)
		if errors.Is(err, io.EOF) {
			return &doc, nil
		}
		if err != nil {
			return nil, err
		}
		if len(next.Content) > 0 && next.Content[0].Kind != 0 &&
			!(next.Content[0].Kind == yaml.ScalarNode && next.Content[0].Tag == "!!null") {
			return nil, errMultipleDocuments{line: secondDocumentLine(data, next.Content[0].Line)}
		}
	}
}

// secondDocumentLine returns the line of the `---` separator that opens the
// document whose first node is at contentLine, falling back to contentLine.
func secondDocumentLine(data []byte, contentLine int) int {
	lines := strings.Split(string(data), "\n")
	for i := min(contentLine-1, len(lines)) - 1; i >= 0; i-- {
		if strings.HasPrefix(strings.TrimRight(lines[i], "\r \t"), "---") {
			return i + 1
		}
	}
	return contentLine
}

// peekKind extracts the top-level `kind` field from a decoded yaml.Node.
// Returns an empty string if the kind is not set.
func peekKind(doc *yaml.Node) string {
	return peekField(doc, "kind")
}

// peekField returns the value of a top-level scalar field without decoding the
// whole document, or "" when the document is not a mapping or lacks the field.
func peekField(doc *yaml.Node, field string) string {
	if doc == nil || len(doc.Content) == 0 {
		return ""
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == field {
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

	// LoadFile accepts only Agent documents (kind: Agent or unset). Reject other
	// known kinds so single-file callers (e.g. `validate`) can fall through to the
	// matching loader instead of mis-parsing, say, an A2AServer as an Agent.
	if k := peekKind(doc); k != "" && k != types.AgentKind {
		return nil, fmt.Errorf("%s: kind %q is not an Agent", path, k)
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
	if k := peekKind(doc); k != types.PipelineKind {
		return nil, fmt.Errorf("%s: kind %q is not %s", path, k, types.PipelineKind)
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
	if k := peekKind(doc); k != types.TestSuiteKind {
		return nil, fmt.Errorf("%s: kind %q is not %s", path, k, types.TestSuiteKind)
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
	if k := peekKind(doc); k != types.MCPServerKind {
		return nil, fmt.Errorf("%s: kind %q is not %s", path, k, types.MCPServerKind)
	}
	var def types.MCPServerDefinition
	if err := doc.Decode(&def); err != nil {
		return nil, &ParseError{File: path, Err: err}
	}
	return &MCPServerLoadResult{Definition: &def, Node: doc, FilePath: path}, nil
}

// LoadA2AServerFile parses a mock A2A server definition file (NF-04).
func LoadA2AServerFile(path string) (*A2AServerLoadResult, error) {
	_, doc, err := readAndParse(path)
	if err != nil {
		return nil, err
	}
	if k := peekKind(doc); k != types.A2AServerKind {
		return nil, fmt.Errorf("%s: kind %q is not %s", path, k, types.A2AServerKind)
	}
	var def types.A2AServerDefinition
	if err := doc.Decode(&def); err != nil {
		return nil, &ParseError{File: path, Err: err}
	}
	return &A2AServerLoadResult{Definition: &def, Node: doc, FilePath: path}, nil
}

func LoadVectorCollectionFile(path string) (*VectorCollectionLoadResult, error) {
	_, doc, err := readAndParse(path)
	if err != nil {
		return nil, err
	}
	if k := peekKind(doc); k != types.VectorCollectionKind {
		return nil, fmt.Errorf("%s: kind %q is not %s", path, k, types.VectorCollectionKind)
	}
	var def types.VectorCollectionDefinition
	if err := doc.Decode(&def); err != nil {
		return nil, &ParseError{File: path, Err: err}
	}
	return &VectorCollectionLoadResult{Definition: &def, Node: doc, FilePath: path}, nil
}

func LoadSearchServiceFile(path string) (*SearchServiceLoadResult, error) {
	_, doc, err := readAndParse(path)
	if err != nil {
		return nil, err
	}
	if k := peekKind(doc); k != types.SearchServiceKind {
		return nil, fmt.Errorf("%s: kind %q is not %s", path, k, types.SearchServiceKind)
	}
	var def types.SearchServiceDefinition
	if err := doc.Decode(&def); err != nil {
		return nil, &ParseError{File: path, Err: err}
	}
	return &SearchServiceLoadResult{Definition: &def, Node: doc, FilePath: path}, nil
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
		if err := appendDocument(docs, path); err != nil {
			errs = append(errs, err)
		}
	}

	return docs, errs
}

// LoadDocumentFile loads one YAML/JSON file of any kind into a Documents
// bucket set. It reads and parses the file once and dispatches on `kind`, so a
// decode error is reported for the kind the file declares rather than as
// "not an Agent" from whichever loader happened to be tried first.
func LoadDocumentFile(path string) (*Documents, error) {
	docs := &Documents{}
	if err := appendDocument(docs, path); err != nil {
		return docs, err
	}
	return docs, nil
}

// appendDocument parses path and appends it to the bucket matching its kind.
func appendDocument(docs *Documents, path string) error {
	_, doc, err := readAndParse(path)
	if err != nil {
		return err
	}
	switch kind := peekKind(doc); kind {
	case types.AgentKind, "":
		var def types.AgentDefinition
		if err := doc.Decode(&def); err != nil {
			return &ParseError{File: path, Err: err}
		}
		docs.Agents = append(docs.Agents, &LoadResult{Definition: &def, Node: doc, FilePath: path})
	case types.PipelineKind:
		var def types.PipelineDefinition
		if err := doc.Decode(&def); err != nil {
			return &ParseError{File: path, Err: err}
		}
		docs.Pipelines = append(docs.Pipelines, &PipelineLoadResult{Definition: &def, Node: doc, FilePath: path})
	case types.TestSuiteKind:
		var def types.TestSuiteDefinition
		if err := doc.Decode(&def); err != nil {
			return &ParseError{File: path, Err: err}
		}
		docs.TestSuites = append(docs.TestSuites, &TestSuiteLoadResult{Definition: &def, Node: doc, FilePath: path})
	case types.MCPServerKind:
		var def types.MCPServerDefinition
		if err := doc.Decode(&def); err != nil {
			return &ParseError{File: path, Err: err}
		}
		docs.MCPServers = append(docs.MCPServers, &MCPServerLoadResult{Definition: &def, Node: doc, FilePath: path})
	case types.A2AServerKind:
		var def types.A2AServerDefinition
		if err := doc.Decode(&def); err != nil {
			return &ParseError{File: path, Err: err}
		}
		docs.A2AServers = append(docs.A2AServers, &A2AServerLoadResult{Definition: &def, Node: doc, FilePath: path})
	case types.VectorCollectionKind:
		var def types.VectorCollectionDefinition
		if err := doc.Decode(&def); err != nil {
			return &ParseError{File: path, Err: err}
		}
		docs.Vectors = append(docs.Vectors, &VectorCollectionLoadResult{Definition: &def, Node: doc, FilePath: path})
	case types.SearchServiceKind:
		var def types.SearchServiceDefinition
		if err := doc.Decode(&def); err != nil {
			return &ParseError{File: path, Err: err}
		}
		docs.SearchServices = append(docs.SearchServices, &SearchServiceLoadResult{Definition: &def, Node: doc, FilePath: path})
	default:
		suggestion := ""
		known := make([]string, 0, len(documentKinds))
		for k := range documentKinds {
			known = append(known, k)
		}
		if best := closestName(kind, known); best != "" {
			suggestion = fmt.Sprintf(" (did you mean %q?)", best)
		}
		return fmt.Errorf("%s: unrecognized kind %q%s", path, kind, suggestion)
	}
	return nil
}

// Count returns the total number of documents across every bucket.
func (d *Documents) Count() int {
	if d == nil {
		return 0
	}
	return len(d.Agents) + len(d.Pipelines) + len(d.TestSuites) + len(d.MCPServers) +
		len(d.A2AServers) + len(d.Vectors) + len(d.SearchServices)
}

// Merge appends every bucket of other to d.
func (d *Documents) Merge(other *Documents) {
	if other == nil {
		return
	}
	d.Agents = append(d.Agents, other.Agents...)
	d.Pipelines = append(d.Pipelines, other.Pipelines...)
	d.TestSuites = append(d.TestSuites, other.TestSuites...)
	d.MCPServers = append(d.MCPServers, other.MCPServers...)
	d.A2AServers = append(d.A2AServers, other.A2AServers...)
	d.Vectors = append(d.Vectors, other.Vectors...)
	d.SearchServices = append(d.SearchServices, other.SearchServices...)
}

// ListDocumentPaths returns every document path a directory scan would load,
// in lexical order (the same set LoadAllDocuments reads).
func ListDocumentPaths(dir string) ([]string, error) {
	return listDocumentPaths(dir)
}

// skipDirs are directory names never descended into when scanning an agents
// directory. Dependency and build trees carry YAML that is not ours —
// node_modules alone ships .travis.yml and FUNDING.yml — and they are normally
// untracked, so descending into them would make the loaded set depend on
// whether someone had run a package manager in the tree: a server that starts
// on a fresh clone and fails after `npm install`.
var skipDirs = map[string]struct{}{
	"__pycache__":  {},
	"build":        {},
	"dist":         {},
	"node_modules": {},
	"target":       {},
	"vendor":       {},
	"venv":         {},
}

// listDocumentPaths returns every document (.yaml, .yml, .json) under dir,
// recursively, in lexical order.
//
// Recursion lets an agents directory be organized into subdirectories and still
// be fully loaded, and makes `mockagents validate ./agents` check exactly the
// set the server will serve. The scan used to stop at the top level, which left
// documents in subdirectories — examples/frameworks/agents/support-agent.yaml
// among them — served by nobody and validated by nothing.
//
// Two kinds of directory are skipped: dependency/build trees (skipDirs) and
// dotted directories such as .git. Symlinked directories are not followed —
// filepath.WalkDir does not descend into them — which also stops a symlink from
// walking a scan outside the directory the caller named.
func listDocumentPaths(dir string) ([]string, error) {
	root := filepath.Clean(dir)
	var paths []string
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == root {
				return nil // never skip the root the caller named
			}
			name := d.Name()
			if _, skip := skipDirs[name]; skip || strings.HasPrefix(name, ".") {
				return fs.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if ext != ".yaml" && ext != ".yml" && ext != ".json" {
			return nil
		}
		// Top-level files keep the old contract: everything with a document
		// extension is meant to be a document, and anything that is not says so
		// as a load error. Only nested files have to identify themselves.
		if filepath.Dir(path) != root && !nestedFileIsDocument(path, d) {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("reading directory %s: %w", dir, walkErr)
	}
	sort.Strings(paths)
	return paths, nil
}

// maxNestedDocumentSize bounds the file a nested scan will parse to identify.
// Agent documents are kilobytes; a megabyte-scale JSON in a subdirectory is
// something like package-lock.json, and reading it only to reject it would make
// every scan pay for it.
const maxNestedDocumentSize = 1 << 20 // 1 MiB

// nestedFileIsDocument reports whether a file found BELOW the top level of an
// agents directory should be loaded as a document.
//
// The top level of an agents directory belongs to us by convention, so anything
// there that is not a document is a mistake worth reporting. A subdirectory is
// different: it can belong to a project that merely happens to contain agents —
// examples/frameworks/typescript holds package.json and package-lock.json next
// to the recipes — so a nested file is collected only when it identifies itself
// as ours, by apiVersion or by a kind we recognize.
//
// A nested file that is malformed is still collected, so a real document with a
// syntax error surfaces as an error rather than silently vanishing. Files that
// are empty, unreadable, or oversized are skipped: they carry no claim to be
// ours, and at the top level they would still be reported as before.
func nestedFileIsDocument(path string, d fs.DirEntry) bool {
	if info, err := d.Info(); err == nil && info.Size() > maxNestedDocumentSize {
		return false
	}
	_, doc, err := readAndParse(path)
	if err != nil {
		// Malformed content is surfaced; empty or unreadable is not ours.
		var parseErr *ParseError
		return errors.As(err, &parseErr)
	}
	if strings.HasPrefix(peekField(doc, "apiVersion"), apiVersionPrefix) {
		return true
	}
	_, known := documentKinds[peekKind(doc)]
	return known
}

// apiVersionPrefix is what every document of ours declares.
const apiVersionPrefix = "mockagents/"

// documentKinds are the kinds LoadAllDocuments dispatches on, used to recognize
// a nested document whose apiVersion is missing or misspelled — it is ours, and
// broken, which is worth an error rather than a silent skip.
var documentKinds = map[string]struct{}{
	types.AgentKind:            {},
	types.PipelineKind:         {},
	types.TestSuiteKind:        {},
	types.MCPServerKind:        {},
	types.A2AServerKind:        {},
	types.VectorCollectionKind: {},
	types.SearchServiceKind:    {},
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

// EncodeForPath returns the canonical YAML encoding of a definition in the
// format the file at path is read back with: JSON for a .json path, YAML
// otherwise. The write paths (agent write API, MCP management) overwrite an
// agent's existing source file in place, and writing YAML into a .json source
// made it unloadable on the next start or reload (review P-10).
func EncodeForPath(path string, canonicalYAML []byte) ([]byte, error) {
	if !isJSON(path) {
		return canonicalYAML, nil
	}
	var doc any
	if err := yaml.Unmarshal(canonicalYAML, &doc); err != nil {
		return nil, err
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}
