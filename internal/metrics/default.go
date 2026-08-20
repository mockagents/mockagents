package metrics

import "time"

// defaultRegistry is the process-wide registry. A package-level default
// mirrors how internal/observability exposes tracing: the recording sites are
// deep in the engine's hot path (scenario matching, chaos injection) and
// threading a registry through every call site there would cost more in
// plumbing than the isolation is worth. Tests that need exact counts call
// Default().Reset() or build their own Registry with New.
var defaultRegistry = New("dev")

// Default returns the process-wide registry that the engine records into and
// that GET /metrics renders.
func Default() *Registry { return defaultRegistry }

// SetVersion re-stamps the default registry's build_info version label. The
// binary learns its version after package init (it comes from the CLI's
// ldflags-set variable), so New("dev") is the placeholder until then.
func SetVersion(version string) {
	if version == "" {
		return
	}
	defaultRegistry.RegisterGauge(Namespace+"_build_info",
		"Build metadata; always 1, carrying version and Go toolchain as labels.",
		[]string{"version", "go_version"}, []string{version, goVersion()},
		func() float64 { return 1 })
}

// RecordRequest records into the default registry. See Registry.RecordRequest.
func RecordRequest(protocol, agent string, status int, d time.Duration) {
	defaultRegistry.RecordRequest(protocol, agent, status, d)
}

// RecordScenarioMatch records into the default registry.
// See Registry.RecordScenarioMatch.
func RecordScenarioMatch(agent, scenario, kind string) {
	defaultRegistry.RecordScenarioMatch(agent, scenario, kind)
}

// RecordChaos records into the default registry. See Registry.RecordChaos.
func RecordChaos(agent, kind string) {
	defaultRegistry.RecordChaos(agent, kind)
}
