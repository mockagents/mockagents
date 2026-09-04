package server

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/mockagents/mockagents/internal/config"
	"github.com/mockagents/mockagents/internal/types"
)

// UX-03 slice A: the agent revision contract.
//
// This is the ADDITIVE conditional-write route approved for UX-03 (epic §8.2).
// "Additive" is the whole point: a PUT that sends no If-Match behaves exactly
// as it did before, so every existing client — the CLI's `mockagents add`, the
// SDKs, anyone's script — keeps working unchanged. Only a caller that opts in
// by sending a precondition gets the new semantics.
//
// # Two revisions, not one
//
// An agent can be changed through the API *or* by editing its YAML on disk, and
// those two can disagree: the file may have moved on while the registry still
// serves what it loaded. Epic §8.2 requires the two be distinguishable, so the
// contract reports both:
//
//	effective — hash of the canonical definition the engine is serving now
//	source    — hash of the backing file's bytes, when the agent has one
//
// The ETag is an opaque hash of BOTH. That is deliberate: a precondition has to
// fail if either has moved, because a write replaces both. Reporting them
// separately in headers is what lets a UI say *which* one drifted instead of
// just "conflict".
//
// # The limit this does NOT close
//
// Conditional writes only order callers who send preconditions. A legacy
// unconditional PUT still overwrites whatever is there — that is exactly what
// keeping the route additive means. This is advertised, not hidden: it is
// stated under "Known limit" on the PUT operation in docs/api-spec.yaml, and
// TestAgentRevision_LegacyUnconditionalWriterCanStillClobber pins the
// behaviour so nobody mistakes it for a bug.

// wantsYAML reports whether the caller asked for the agent document as YAML.
// Only an explicit YAML media type counts: a browser's "*/*", or an Accept
// header that merely tolerates YAML alongside JSON, keeps the JSON default so
// existing clients are unaffected.
func wantsYAML(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept"), ",") {
		media, _, _ := strings.Cut(part, ";")
		switch strings.ToLower(strings.TrimSpace(media)) {
		case "application/yaml", "application/x-yaml", "text/yaml", "text/x-yaml":
			return true
		}
	}
	return false
}

// Response headers carrying the two revisions. The ETag stays opaque; these
// exist so a client can explain a conflict rather than merely report one.
const (
	headerRevisionEffective = "X-Mockagents-Revision-Effective"
	headerRevisionSource    = "X-Mockagents-Revision-Source"
)

// agentRevision describes the version of one agent at a moment in time.
type agentRevision struct {
	// Effective hashes the canonical YAML of the definition the engine holds.
	Effective string
	// Source hashes the backing file's RAW bytes. Empty when the agent has no
	// file (in-memory only), or the file has vanished from under the registry.
	//
	// Raw bytes, deliberately: a save rewrites the file canonically and would
	// destroy any comments or formatting a human put there, so a cosmetic edit
	// on disk should still block a conditional write rather than be silently
	// overwritten.
	Source string
	// SourceCanonical hashes the file after parsing and canonicalizing it the
	// same way Effective is computed. This is the only fair comparison against
	// Effective: hand-authored YAML never matches canonical output byte for
	// byte (comments, key order, defaults), so comparing raw Source to
	// Effective would report "edited on disk" for every file ever loaded.
	SourceCanonical string
	// SourceMissing is true when the agent claims a source path but the file
	// could not be read. The agent still serves — it is in memory — but a write
	// cannot honestly claim to be replacing a known file version.
	SourceMissing bool
	// SourceUnparseable is true when the file exists but no longer parses as an
	// agent. Its meaning cannot be compared, so drift is unknown, not absent.
	SourceUnparseable bool
}

// ETag returns the opaque validator a client echoes back as If-Match.
func (rev agentRevision) ETag() string {
	return hashHex([]byte(rev.Effective + "\x00" + rev.Source))
}

// drift reports whether the file on disk MEANS something different from what
// the engine is serving. Compares canonical forms, so formatting and comments
// do not count as drift; only a semantic difference does.
func (rev agentRevision) drift() bool {
	return rev.SourceCanonical != "" && rev.SourceCanonical != rev.Effective
}

// revisionFor computes the revision of an agent definition. src is the agent's
// tracked source path ("" when it has none).
func revisionFor(def *types.AgentDefinition, src string) (agentRevision, error) {
	canonical, err := yaml.Marshal(def)
	if err != nil {
		return agentRevision{}, fmt.Errorf("marshaling agent: %w", err)
	}
	rev := agentRevision{Effective: hashHex(canonical)}
	if src == "" {
		return rev, nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		// A missing or unreadable file is not fatal to a READ — the agent is
		// still being served from memory — but it must be visible, because a
		// caller cannot reason about a file version that cannot be read.
		rev.SourceMissing = true
		return rev, nil
	}
	rev.Source = hashHex(data)

	// Canonicalize the file the same way the effective revision is computed —
	// decode, apply defaults, marshal — so the two are comparable. A file that
	// no longer parses leaves SourceCanonical empty and is flagged, rather than
	// silently reported as "no drift".
	var fromDisk types.AgentDefinition
	if err := yaml.Unmarshal(data, &fromDisk); err != nil {
		rev.SourceUnparseable = true
		return rev, nil
	}
	config.ApplyDefaults(&fromDisk)
	if canonical, err := yaml.Marshal(&fromDisk); err == nil {
		rev.SourceCanonical = hashHex(canonical)
	} else {
		rev.SourceUnparseable = true
	}
	return rev, nil
}

// agentRevisionFor looks up an agent through the caller's tenant view and
// computes its revision. Returns ok=false when no such agent is visible.
func (h *Handlers) agentRevisionFor(name, tenantID string) (agentRevision, bool, error) {
	def := h.Engine.Registry.GetForTenant(name, tenantID)
	if def == nil {
		return agentRevision{}, false, nil
	}
	rev, err := revisionFor(def, h.Engine.Registry.Source(name, tenantID))
	return rev, true, err
}

// setRevisionHeaders writes the ETag plus the two informational revision
// headers onto a response.
func setRevisionHeaders(w http.ResponseWriter, rev agentRevision) {
	w.Header().Set("ETag", `"`+rev.ETag()+`"`)
	w.Header().Set(headerRevisionEffective, rev.Effective)
	if rev.Source != "" {
		w.Header().Set(headerRevisionSource, rev.Source)
	}
}

// preconditionResult is the outcome of evaluating If-Match / If-None-Match.
type preconditionResult int

const (
	preconditionPassed preconditionResult = iota
	// preconditionAbsent means the caller sent no precondition at all: the
	// legacy unconditional path, which must keep working.
	preconditionAbsent
	preconditionFailed
)

// checkAgentPrecondition evaluates the request's conditional headers against
// the current state of the caller's OWN agent of that name.
//
// Ownership matters here. PUT writes into the caller's tenant bucket, so the
// resource a precondition refers to is the tenant's own agent — not a global
// agent of the same name that the caller can merely read. A caller that read a
// global agent and tries to conditionally replace it is not replacing what it
// read; it is creating a tenant-owned agent, and gets a 412 that says so.
//
// Semantics follow RFC 9110:
//
//	If-None-Match: *      create-only; fails if the resource already exists
//	If-Match: *           requires the resource to exist
//	If-Match: "<etag>"    requires the resource to exist AND match
//
// It returns a human-readable reason when the precondition fails.
func (h *Handlers) checkAgentPrecondition(r *http.Request, name, tenantID string) (preconditionResult, string, error) {
	ifMatch := strings.TrimSpace(r.Header.Get("If-Match"))
	ifNoneMatch := strings.TrimSpace(r.Header.Get("If-None-Match"))
	if ifMatch == "" && ifNoneMatch == "" {
		return preconditionAbsent, "", nil
	}

	owned := h.Engine.Registry.GetOwnedForTenant(name, tenantID)

	// If-None-Match: * — "only create; do not overwrite".
	if ifNoneMatch != "" {
		if ifNoneMatch != "*" {
			return preconditionFailed,
				`If-None-Match supports only "*" here (create-only). Use If-Match to replace a known revision.`, nil
		}
		if owned != nil {
			return preconditionFailed,
				fmt.Sprintf("agent %q already exists; If-None-Match: * refuses to overwrite it", name), nil
		}
		// Not present: creating is exactly what was asked for.
		if ifMatch == "" {
			return preconditionPassed, "", nil
		}
	}

	if ifMatch == "" {
		return preconditionPassed, "", nil
	}

	if owned == nil {
		// A specific revision, or "*", cannot match something that is not there.
		// This is how a delete invalidates an in-flight edit.
		visible := h.Engine.Registry.GetForTenant(name, tenantID) != nil
		reason := fmt.Sprintf("agent %q does not exist in this tenant, so If-Match cannot be satisfied "+
			"(it may have been deleted since it was loaded)", name)
		if visible {
			reason = fmt.Sprintf("agent %q is visible but not owned by this tenant; writing it would CREATE a "+
				"tenant-owned agent rather than replace what was read, so If-Match cannot be satisfied. "+
				"Use If-None-Match: * to create it.", name)
		}
		return preconditionFailed, reason, nil
	}

	if ifMatch == "*" {
		return preconditionPassed, "", nil
	}

	rev, err := revisionFor(owned, h.Engine.Registry.Source(name, tenantID))
	if err != nil {
		return preconditionFailed, "", err
	}
	current := rev.ETag()
	for _, candidate := range strings.Split(ifMatch, ",") {
		if strings.Trim(strings.TrimSpace(candidate), `"`) == current {
			return preconditionPassed, "", nil
		}
	}

	// Say WHICH revision moved. "Someone else changed it" and "the file on disk
	// changed underneath the running server" need different fixes.
	reason := fmt.Sprintf("agent %q changed since it was loaded; reload and re-apply", name)
	if rev.SourceMissing {
		reason = fmt.Sprintf("agent %q no longer has a readable source file; reload before replacing it", name)
	} else if rev.SourceUnparseable {
		reason = fmt.Sprintf("agent %q has a source file that no longer parses, so its on-disk state "+
			"cannot be compared; fix or reload the file before replacing it", name)
	} else if rev.drift() {
		reason = fmt.Sprintf("agent %q was edited on disk since the running definition was loaded "+
			"(source and effective revisions differ); reload and re-apply", name)
	}
	return preconditionFailed, reason, nil
}
