package config

import (
	"fmt"
)

// ValidateDocuments runs cross-document reference checks across a
// collection of loaded Agent, Pipeline, TestSuite, and MCPServer
// definitions. It is the second pass after the per-document
// validators (ValidateAgent / ValidatePipeline / ValidateTestSuite /
// ValidateMCPServer) — it catches references that are syntactically
// valid within their own document but structurally impossible
// because the referenced peer does not exist in the same directory.
//
// Rules:
//
//   - Every Pipeline's spec.agents[].ref must name a loaded Agent.
//   - Every TestSuite's spec.target.agent must name a loaded Agent.
//   - Every TestSuite's spec.target.pipeline must name a loaded
//     Pipeline.
//   - Every TestSuite assertion with node_id set must target a
//     pipeline AND the node_id must exist in that pipeline's
//     spec.agents[].id list.
//
// Returns nil when the collection is internally consistent.
//
// ValidateDocuments does not modify its input. Errors are keyed on
// the file path + yaml.Node pair of the *referring* document (not
// the missing target), so an operator sees "test-suite.yaml:10:5
// references unknown agent support-agent" rather than a generic
// "unknown reference" message.
func ValidateDocuments(docs *Documents) *ValidationErrorList {
	if docs == nil {
		return nil
	}

	// Build name indexes once. Both Agents and Pipelines are
	// keyed by metadata.name — the same identifier the refs use.
	agentNames := make(map[string]struct{}, len(docs.Agents))
	for _, ar := range docs.Agents {
		if ar == nil || ar.Definition == nil {
			continue
		}
		if ar.Definition.Metadata.Name != "" {
			agentNames[ar.Definition.Metadata.Name] = struct{}{}
		}
	}
	// For pipelines we additionally record the node-id set so
	// assertions that carry a node_id can be checked against the
	// pipeline they target.
	pipelineNodeIDs := make(map[string]map[string]struct{}, len(docs.Pipelines))
	for _, pr := range docs.Pipelines {
		if pr == nil || pr.Definition == nil {
			continue
		}
		name := pr.Definition.Metadata.Name
		if name == "" {
			continue
		}
		nodes := make(map[string]struct{}, len(pr.Definition.Spec.Agents))
		for _, node := range pr.Definition.Spec.Agents {
			if node.ID != "" {
				nodes[node.ID] = struct{}{}
			}
		}
		pipelineNodeIDs[name] = nodes
	}

	var errs []*ValidationError

	// Pipeline → agent ref checks.
	for _, pr := range docs.Pipelines {
		if pr == nil || pr.Definition == nil {
			continue
		}
		ctx := &validationContext{file: pr.FilePath, node: pr.Node}
		for i, node := range pr.Definition.Spec.Agents {
			if node.Ref == "" {
				continue // per-doc validator already flagged this
			}
			if _, ok := agentNames[node.Ref]; !ok {
				ctx.addError(
					fmt.Sprintf("spec.agents[%d].ref", i),
					fmt.Sprintf("pipeline references unknown agent %q", node.Ref),
					fmt.Sprintf("Add an agent YAML with metadata.name: %s to the same directory, or fix the ref.", node.Ref),
				)
			}
		}
		errs = append(errs, ctx.errors...)
	}

	// TestSuite → agent / pipeline / node_id checks.
	for _, sr := range docs.TestSuites {
		if sr == nil || sr.Definition == nil {
			continue
		}
		ctx := &validationContext{file: sr.FilePath, node: sr.Node}
		target := sr.Definition.Spec.Target

		if target.Agent != "" {
			if _, ok := agentNames[target.Agent]; !ok {
				ctx.addError(
					"spec.target.agent",
					fmt.Sprintf("test suite references unknown agent %q", target.Agent),
					fmt.Sprintf("Add an agent YAML with metadata.name: %s to the same directory, or fix the target.", target.Agent),
				)
			}
		}

		var pipelineNodes map[string]struct{}
		if target.Pipeline != "" {
			nodes, ok := pipelineNodeIDs[target.Pipeline]
			if !ok {
				ctx.addError(
					"spec.target.pipeline",
					fmt.Sprintf("test suite references unknown pipeline %q", target.Pipeline),
					fmt.Sprintf("Add a pipeline YAML with metadata.name: %s to the same directory, or fix the target.", target.Pipeline),
				)
			} else {
				pipelineNodes = nodes
			}
		}

		// Assertions with node_id set are only meaningful under a
		// pipeline target AND the node_id must exist in that pipeline.
		// We iterate every case/assertion to collect the errors.
		for ci, tc := range sr.Definition.Spec.Cases {
			for ai, assertion := range tc.Assertions {
				if assertion.NodeID == "" {
					continue
				}
				field := fmt.Sprintf("spec.cases[%d].assertions[%d].node_id", ci, ai)
				if target.Pipeline == "" {
					ctx.addError(
						field,
						"assertion has node_id but the test suite targets an agent, not a pipeline",
						"Remove node_id (it only applies to pipeline targets) or switch the target to spec.target.pipeline.",
					)
					continue
				}
				if pipelineNodes == nil {
					// Pipeline reference was already flagged as
					// unknown above — skip so the report stays
					// focused on root causes.
					continue
				}
				if _, ok := pipelineNodes[assertion.NodeID]; !ok {
					ctx.addError(
						field,
						fmt.Sprintf("assertion node_id %q does not exist in pipeline %q", assertion.NodeID, target.Pipeline),
						"Use one of the node ids declared under the pipeline's spec.agents list, or fix the assertion.",
					)
				}
			}
		}

		errs = append(errs, ctx.errors...)
	}

	if len(errs) == 0 {
		return nil
	}
	return &ValidationErrorList{Errors: errs}
}
