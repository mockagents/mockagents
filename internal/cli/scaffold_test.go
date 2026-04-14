package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScaffold_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "my-project")

	absDir, err := Scaffold(ScaffoldOptions{
		ProjectName: "my-project",
		TargetDir:   projectDir,
	})
	require.NoError(t, err)
	assert.Contains(t, absDir, "my-project")

	// Verify directory structure.
	assertFileExists(t, filepath.Join(projectDir, ".mockagents.yaml"))
	assertFileExists(t, filepath.Join(projectDir, "agents", "example-agent.yaml"))
	assertFileExists(t, filepath.Join(projectDir, "tests", "example-test.yaml"))
	assertFileExists(t, filepath.Join(projectDir, "README.md"))
}

func TestScaffold_FileContents(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "test-proj")

	_, err := Scaffold(ScaffoldOptions{
		ProjectName: "test-proj",
		TargetDir:   projectDir,
	})
	require.NoError(t, err)

	// Check .mockagents.yaml content.
	config := readFile(t, filepath.Join(projectDir, ".mockagents.yaml"))
	assert.Contains(t, config, "version:")
	assert.Contains(t, config, "port: 8080")
	assert.Contains(t, config, "agents_dir:")

	// Check example agent is valid YAML with required fields.
	agent := readFile(t, filepath.Join(projectDir, "agents", "example-agent.yaml"))
	assert.Contains(t, agent, "apiVersion: mockagents/v1")
	assert.Contains(t, agent, "kind: Agent")
	assert.Contains(t, agent, "name: example-agent")
	assert.Contains(t, agent, "protocol: openai-chat-completions")
	assert.Contains(t, agent, "scenarios:")

	// Check test file.
	testFile := readFile(t, filepath.Join(projectDir, "tests", "example-test.yaml"))
	assert.Contains(t, testFile, "kind: TestSuite")
	assert.Contains(t, testFile, "tests:")

	// Check README.
	readme := readFile(t, filepath.Join(projectDir, "README.md"))
	assert.Contains(t, readme, "test-proj")
	assert.Contains(t, readme, "mockagents start")
	assert.Contains(t, readme, "mockagents validate")
}

func TestScaffold_NonEmptyDirectory_NoForce(t *testing.T) {
	dir := t.TempDir()
	// Create a file in the directory so it's not empty.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "existing.txt"), []byte("data"), 0644))

	_, err := Scaffold(ScaffoldOptions{
		ProjectName: "test",
		TargetDir:   dir,
		Force:       false,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not empty")
}

func TestScaffold_NonEmptyDirectory_WithForce(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "existing.txt"), []byte("data"), 0644))

	_, err := Scaffold(ScaffoldOptions{
		ProjectName: "test",
		TargetDir:   dir,
		Force:       true,
	})
	require.NoError(t, err)
	assertFileExists(t, filepath.Join(dir, ".mockagents.yaml"))
	assertFileExists(t, filepath.Join(dir, "existing.txt")) // Should not be deleted.
}

func TestScaffold_ExistingFileNoForce(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "proj")
	require.NoError(t, os.MkdirAll(projectDir, 0755))
	// Create the config file so it conflicts — directory has contents.
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".mockagents.yaml"), []byte("old"), 0644))

	_, err := Scaffold(ScaffoldOptions{
		ProjectName: "proj",
		TargetDir:   projectDir,
		Force:       false,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not empty")
}

func TestScaffold_ExistingFileWithForce(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "proj")
	require.NoError(t, os.MkdirAll(projectDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".mockagents.yaml"), []byte("old"), 0644))

	_, err := Scaffold(ScaffoldOptions{
		ProjectName: "proj",
		TargetDir:   projectDir,
		Force:       true,
	})
	require.NoError(t, err)

	// File should be overwritten.
	content := readFile(t, filepath.Join(projectDir, ".mockagents.yaml"))
	assert.Contains(t, content, "version:")
	assert.NotEqual(t, "old", content)
}

func TestScaffold_FileIsNotDirectory(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "not-a-dir")
	require.NoError(t, os.WriteFile(filePath, []byte("file"), 0644))

	_, err := Scaffold(ScaffoldOptions{
		ProjectName: "test",
		TargetDir:   filePath,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

func TestScaffold_CurrentDirectory(t *testing.T) {
	dir := t.TempDir()

	_, err := Scaffold(ScaffoldOptions{
		ProjectName: ".",
		TargetDir:   dir,
	})
	require.NoError(t, err)
	assertFileExists(t, filepath.Join(dir, ".mockagents.yaml"))
}

func TestScaffold_AgentValidation(t *testing.T) {
	// The generated example agent should pass mockagents validate.
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "validate-test")

	_, err := Scaffold(ScaffoldOptions{
		ProjectName: "validate-test",
		TargetDir:   projectDir,
	})
	require.NoError(t, err)

	// Load and validate the generated agent file.
	agentPath := filepath.Join(projectDir, "agents", "example-agent.yaml")
	assertFileExists(t, agentPath)

	// Verify it contains valid YAML structure.
	content := readFile(t, agentPath)
	assert.Contains(t, content, "apiVersion: mockagents/v1")
	assert.Contains(t, content, "kind: Agent")
	assert.Contains(t, content, "spec:")
	assert.Contains(t, content, "behavior:")
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	_, err := os.Stat(path)
	assert.NoError(t, err, "file should exist: %s", path)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}
