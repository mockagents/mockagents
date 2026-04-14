package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBinaryBuild verifies the Go binary compiles successfully.
func TestBinaryBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping build test in short mode")
	}

	tmpDir := t.TempDir()
	binary := filepath.Join(tmpDir, "mockagents")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}

	cmd := exec.Command("go", "build", "-o", binary, "../../cmd/mockagents/")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "build failed: %s", string(output))

	// Verify binary exists.
	info, err := os.Stat(binary)
	require.NoError(t, err)
	assert.True(t, info.Size() > 0, "binary should not be empty")

	// Verify --version flag works.
	cmd = exec.Command(binary, "--version")
	output, err = cmd.CombinedOutput()
	require.NoError(t, err, "version flag failed: %s", string(output))
	assert.Contains(t, string(output), "mockagents")
}

// TestBinaryValidateExamples verifies the binary can validate example agent files.
func TestBinaryValidateExamples(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping build test in short mode")
	}

	tmpDir := t.TempDir()
	binary := filepath.Join(tmpDir, "mockagents")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}

	// Build.
	buildCmd := exec.Command("go", "build", "-o", binary, "../../cmd/mockagents/")
	buildCmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	output, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "build failed: %s", string(output))

	// Validate examples.
	examplesDir, err := filepath.Abs("../../examples")
	require.NoError(t, err)

	cmd := exec.Command(binary, "validate", examplesDir)
	output, err = cmd.CombinedOutput()
	require.NoError(t, err, "validate failed: %s", string(output))
	assert.Contains(t, string(output), "valid")
}

// TestBinaryHelpCommands verifies all CLI commands have help text.
func TestBinaryHelpCommands(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping build test in short mode")
	}

	tmpDir := t.TempDir()
	binary := filepath.Join(tmpDir, "mockagents")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}

	buildCmd := exec.Command("go", "build", "-o", binary, "../../cmd/mockagents/")
	buildCmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	output, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "build failed: %s", string(output))

	commands := []string{"start", "validate", "init", "logs"}
	for _, subcmd := range commands {
		t.Run(subcmd, func(t *testing.T) {
			cmd := exec.Command(binary, subcmd, "--help")
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s --help failed: %s", subcmd, string(output))
			assert.NotEmpty(t, string(output))
		})
	}
}

// TestBinaryInit verifies the init command scaffolds a project.
func TestBinaryInit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping build test in short mode")
	}

	tmpDir := t.TempDir()
	binary := filepath.Join(tmpDir, "mockagents")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}

	buildCmd := exec.Command("go", "build", "-o", binary, "../../cmd/mockagents/")
	buildCmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	output, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "build failed: %s", string(output))

	// Run init.
	projectDir := filepath.Join(tmpDir, "test-project")
	cmd := exec.Command(binary, "init", projectDir)
	output, err = cmd.CombinedOutput()
	require.NoError(t, err, "init failed: %s", string(output))

	// Verify scaffolded files exist.
	assertFileExists(t, filepath.Join(projectDir, ".mockagents.yaml"))
	assertFileExists(t, filepath.Join(projectDir, "agents", "example-agent.yaml"))
	assertFileExists(t, filepath.Join(projectDir, "tests", "example-test.yaml"))
	assertFileExists(t, filepath.Join(projectDir, "README.md"))

	// Verify the scaffolded agent validates.
	cmd = exec.Command(binary, "validate", filepath.Join(projectDir, "agents"))
	output, err = cmd.CombinedOutput()
	require.NoError(t, err, "validate scaffolded agents failed: %s", string(output))
}

// TestDockerfileExists verifies the Dockerfile is present.
func TestDockerfileExists(t *testing.T) {
	assertFileExists(t, "../../Dockerfile")
}

// TestDockerComposeExists verifies docker-compose.yml is present.
func TestDockerComposeExists(t *testing.T) {
	assertFileExists(t, "../../docker-compose.yml")
}

// TestGoreleaserExists verifies .goreleaser.yml is present.
func TestGoreleaserExists(t *testing.T) {
	assertFileExists(t, "../../.goreleaser.yml")
}

// TestCIWorkflowExists verifies CI workflow is present.
func TestCIWorkflowExists(t *testing.T) {
	assertFileExists(t, "../../.github/workflows/ci.yml")
}

// TestReleaseWorkflowExists verifies release workflow is present.
func TestReleaseWorkflowExists(t *testing.T) {
	assertFileExists(t, "../../.github/workflows/release.yml")
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	_, err := os.Stat(path)
	assert.NoError(t, err, "file should exist: %s", path)
}
