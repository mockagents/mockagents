package cli

import (
	"path/filepath"
	"testing"

	"github.com/mockagents/mockagents/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScaffoldedAgent_PassesValidation(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "int-test")

	_, err := Scaffold(ScaffoldOptions{
		ProjectName: "int-test",
		TargetDir:   projectDir,
	})
	require.NoError(t, err)

	// Load the generated agent file.
	agentPath := filepath.Join(projectDir, "agents", "example-agent.yaml")
	result, err := config.LoadFile(agentPath)
	require.NoError(t, err, "generated agent YAML should parse")

	// Apply defaults and validate.
	config.ApplyDefaults(result.Definition)
	v := &config.Validator{}
	errList := v.Validate(result.Definition, result.FilePath, result.Node)
	assert.Nil(t, errList, "generated agent should pass validation, got: %v", errList)
}

func TestScaffoldedAgent_LoadableByEngine(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "engine-test")

	_, err := Scaffold(ScaffoldOptions{
		ProjectName: "engine-test",
		TargetDir:   projectDir,
	})
	require.NoError(t, err)

	// Load all agents from the scaffolded directory.
	agentsDir := filepath.Join(projectDir, "agents")
	results, errs := config.LoadDir(agentsDir)
	assert.Empty(t, errs, "no load errors expected")
	require.Len(t, results, 1, "should have one agent")

	agent := results[0].Definition
	assert.Equal(t, "example-agent", agent.Metadata.Name)
	assert.Equal(t, "openai-chat-completions", agent.Spec.Protocol)
	assert.GreaterOrEqual(t, len(agent.Spec.Behavior.Scenarios), 2)
	assert.GreaterOrEqual(t, len(agent.Spec.Tools), 1)
}
