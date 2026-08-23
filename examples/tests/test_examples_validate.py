"""Tests that verify the example documents in examples/ are structurally valid.

examples/ is not an all-Agent directory. Alongside `kind: Agent` it carries a
Pipeline, two TestSuites, an MCPServer and an A2AServer, and the agent-shaped
invariants below (a non-empty scenario list, a catch-all default scenario) are
meaningless for those. So the parametrization is partitioned on each document's
top-level `kind`: empty or "Agent" is an agent — mirroring the Go loader, which
accepts an Agent document with `kind` unset (internal/config/loader.go,
LoadFile) — and every other kind is asserted against the shape that actually
applies to it, rather than being skipped.

Discovery recurses into subdirectories, so `frameworks/agents/support-agent.yaml`
is covered too. `mockagents validate` recurses as well, so both gates now see the
same corpus — when this suite was first written neither did, and that document
was asserted by nothing at all.

Cross-document invariants (a Pipeline node's `ref`, a TestSuite's `target`
resolving to a real agent) are deliberately NOT duplicated here: the Go
cross-document validator already covers them, and CI runs
`mockagents validate examples/` in the Go job.
"""

import json
import os

import pytest
import yaml

EXAMPLES_DIR = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))
REPO_ROOT = os.path.abspath(os.path.join(EXAMPLES_DIR, ".."))

AGENT_KIND = "Agent"

# Required spec fields per non-Agent kind — the minimum that makes each
# document mean anything to the server that consumes it.
NON_AGENT_REQUIRED_SPEC_FIELDS = {
    "A2AServer": ("card", "responses"),
    "MCPServer": ("capabilities",),
    "Pipeline": ("topology", "agents"),
    "TestSuite": ("target", "cases"),
    "VectorCollection": ("dimension", "metric"),
}

# Every kind internal/config/loader.go LoadDirectory dispatches on, plus the
# empty string for an Agent with `kind` unset. A document outside this set is a
# hard load error in Go ("unrecognized kind"), so it must fail here too instead
# of silently falling out of both partitions.
KNOWN_KINDS = ("", AGENT_KIND) + tuple(NON_AGENT_REQUIRED_SPEC_FIELDS)


# Directory names never descended into when looking for example documents.
# Dependency and build trees carry unrelated YAML — node_modules alone ships
# .travis.yml and FUNDING.yml — and they are untracked, so whether they exist
# depends on whether anyone has run `npm install` in this checkout. Walking
# them would make the collected corpus differ between machines and between CI
# jobs, which is how a gate turns flaky.
SKIP_DIRS = frozenset({
    ".git",
    ".pytest_cache",
    ".venv",
    "__pycache__",
    "build",
    "dist",
    "node_modules",
    "venv",
})


def get_example_files():
    """Find every example YAML under examples/, recursively.

    Returns paths relative to EXAMPLES_DIR with forward slashes, so parametrized
    test ids and failure messages read identically on Windows and Linux.
    """
    files = []
    for dirpath, dirnames, filenames in os.walk(EXAMPLES_DIR):
        # Prune in place so os.walk never descends into them.
        dirnames[:] = sorted(d for d in dirnames if d not in SKIP_DIRS and not d.startswith("."))
        for f in filenames:
            if f.endswith((".yaml", ".yml")) and not f.startswith("."):
                rel = os.path.relpath(os.path.join(dirpath, f), EXAMPLES_DIR)
                files.append(rel.replace(os.sep, "/"))
    return sorted(files)


def load_example(filename):
    """Parse one example document."""
    with open(os.path.join(EXAMPLES_DIR, filename), "r", encoding="utf-8") as f:
        return yaml.safe_load(f)


def kind_of(doc):
    """The document's top-level kind, or None if it is not even a mapping."""
    if not isinstance(doc, dict):
        return None
    return doc.get("kind", "")


def _partition_by_kind():
    """Split the examples into agent-shaped documents and everything else."""
    agents, others = [], []
    for filename in get_example_files():
        kind = kind_of(load_example(filename))
        target = agents if kind in ("", AGENT_KIND) else others
        target.append(filename)
    return agents, others


AGENT_EXAMPLES, NON_AGENT_EXAMPLES = _partition_by_kind()


def match_rule_keys():
    """The match-rule keys the published Agent JSON schema declares.

    Read from the schema rather than hard-coded so this test tracks the schema
    instead of drifting from it.
    """
    path = os.path.join(REPO_ROOT, "schema", "mockagents-v1-agent.json")
    with open(path, "r", encoding="utf-8") as f:
        schema = json.load(f)
    return set(schema["$defs"]["MatchRule"]["properties"])


def scenarios_of(doc):
    return doc["spec"]["behavior"]["scenarios"]


class TestExampleValidation:
    """Ensure all example definitions are structurally valid."""

    def test_examples_directory_exists(self):
        assert os.path.isdir(EXAMPLES_DIR), f"Examples directory not found: {EXAMPLES_DIR}"

    def test_has_minimum_examples(self):
        files = get_example_files()
        assert len(files) >= 3, f"Expected at least 3 example files, found {len(files)}: {files}"

    def test_partitions_cover_every_example(self):
        """Both partitions together must account for every file.

        Guards the parametrization itself: a bug that emptied one partition
        would otherwise turn its tests into a silent no-op.
        """
        assert sorted(AGENT_EXAMPLES + NON_AGENT_EXAMPLES) == get_example_files()
        assert AGENT_EXAMPLES, "no agent examples were collected"
        assert NON_AGENT_EXAMPLES, "no non-agent examples were collected"

    def test_discovery_reaches_subdirectories(self):
        """The frameworks recipe agent lives in a subdirectory and must be covered.

        `mockagents validate examples/` does not recurse, so if discovery here
        stopped recursing too, nothing would assert this document's shape.
        """
        assert "frameworks/agents/support-agent.yaml" in get_example_files()

    def test_discovery_excludes_dependency_trees(self):
        """No collected path may come out of a vendor or build directory.

        Those trees carry unrelated YAML and are untracked, so walking into them
        would collect a corpus that differs by machine — green on a clean
        checkout, red once someone has run `npm install`.
        """
        for filename in get_example_files():
            offending = set(filename.split("/")) & SKIP_DIRS
            assert not offending, f"{filename} was collected from {sorted(offending)}"

    def test_customer_support_agent_exists(self):
        path = os.path.join(EXAMPLES_DIR, "customer-support-agent.yaml")
        assert os.path.isfile(path)

    def test_code_assistant_exists(self):
        path = os.path.join(EXAMPLES_DIR, "code-assistant.yaml")
        assert os.path.isfile(path)

    def test_rag_agent_exists(self):
        path = os.path.join(EXAMPLES_DIR, "rag-agent.yaml")
        assert os.path.isfile(path)

    def test_weather_agent_exists(self):
        path = os.path.join(EXAMPLES_DIR, "weather-agent.yaml")
        assert os.path.isfile(path)

    def test_minimal_agent_exists(self):
        path = os.path.join(EXAMPLES_DIR, "minimal-agent.yaml")
        assert os.path.isfile(path)

    # --- invariants every document shares, whatever its kind ----------------

    @pytest.mark.parametrize("filename", get_example_files())
    def test_example_is_a_recognized_document(self, filename):
        """Each example is a mapping of a kind the loader knows, with a name."""
        doc = load_example(filename)

        assert isinstance(doc, dict), f"{filename} is not a YAML mapping"
        assert doc.get("apiVersion") == "mockagents/v1", f"{filename} missing apiVersion"
        assert kind_of(doc) in KNOWN_KINDS, (
            f"{filename}: unrecognized kind {doc.get('kind')!r} — the Go loader "
            f"rejects it. Add it to NON_AGENT_REQUIRED_SPEC_FIELDS with the "
            f"fields it requires."
        )
        assert "metadata" in doc, f"{filename} missing metadata"
        assert "name" in doc["metadata"], f"{filename} missing metadata.name"
        assert "spec" in doc, f"{filename} missing spec"

    # --- agent-shaped invariants -------------------------------------------

    @pytest.mark.parametrize("filename", AGENT_EXAMPLES)
    def test_example_yaml_is_valid(self, filename):
        """Each agent example should contain valid YAML with required fields."""
        doc = load_example(filename)

        assert "protocol" in doc["spec"], f"{filename} missing spec.protocol"
        assert "behavior" in doc["spec"], f"{filename} missing spec.behavior"
        assert "scenarios" in doc["spec"]["behavior"], f"{filename} missing scenarios"
        assert len(scenarios_of(doc)) > 0, f"{filename} has no scenarios"

    @pytest.mark.parametrize("filename", AGENT_EXAMPLES)
    def test_example_scenarios_have_names(self, filename):
        """Every scenario in every agent example should have a name."""
        for i, scenario in enumerate(scenarios_of(load_example(filename))):
            assert "name" in scenario, f"{filename}: scenario {i} missing name"
            assert scenario["name"], f"{filename}: scenario {i} has empty name"

    @pytest.mark.parametrize("filename", AGENT_EXAMPLES)
    def test_example_has_default_scenario(self, filename):
        """Every agent example should have a default (catch-all) scenario.

        This holds for a realtime agent too, and most of all for one: audio
        committed by server VAD always transcribes to the fixed "[audio input]"
        placeholder (the mock has no STT), so every voice turn that is not a
        text match lands on the default.
        """
        scenarios = scenarios_of(load_example(filename))
        has_default = any(
            "match" not in s or s.get("match") is None
            for s in scenarios
        )
        assert has_default, f"{filename} has no default scenario (scenario without match)"

    @pytest.mark.parametrize("filename", AGENT_EXAMPLES)
    def test_example_scenario_match_keys_are_declared_in_the_schema(self, filename):
        """A scenario's match block may only use keys the Agent schema declares.

        The Go YAML decoder silently drops unknown keys, so a typo does not fail
        the load — it yields an empty MatchRule, and an empty rule matches EVERY
        request. `match: {default: true}` here read like a catch-all and behaved
        like one by accident, while being rejected by the published schema
        (MatchRule sets additionalProperties: false) and never counting as the
        default scenario in the match-rate metric.
        """
        allowed = match_rule_keys()
        for scenario in scenarios_of(load_example(filename)):
            match = scenario.get("match")
            if not isinstance(match, dict):
                continue
            unknown = sorted(set(match) - allowed)
            assert not unknown, (
                f"{filename}: scenario {scenario.get('name')!r} uses match key(s) "
                f"{unknown}, which the schema does not declare. To make a "
                f"catch-all scenario, omit the match block entirely."
            )

    # --- non-agent kinds ----------------------------------------------------

    @pytest.mark.parametrize("filename", NON_AGENT_EXAMPLES)
    def test_non_agent_example_has_required_spec_fields(self, filename):
        """A Pipeline/TestSuite/MCPServer/A2AServer carries its own shape."""
        doc = load_example(filename)
        kind = kind_of(doc)
        spec = doc["spec"]

        assert kind in NON_AGENT_REQUIRED_SPEC_FIELDS, (
            f"{filename}: no required-field list for kind {kind!r} — add one to "
            f"NON_AGENT_REQUIRED_SPEC_FIELDS."
        )
        for field in NON_AGENT_REQUIRED_SPEC_FIELDS[kind]:
            assert field in spec, f"{filename} (kind: {kind}) missing spec.{field}"
            assert spec[field], f"{filename} (kind: {kind}) has empty spec.{field}"

    @pytest.mark.parametrize("filename", NON_AGENT_EXAMPLES)
    def test_test_suite_cases_have_names(self, filename):
        """Every TestSuite case needs a name — it labels the run's output."""
        doc = load_example(filename)
        if kind_of(doc) != "TestSuite":
            pytest.skip(f"{filename} is not a TestSuite")

        for i, case in enumerate(doc["spec"]["cases"]):
            assert case.get("name"), f"{filename}: case {i} missing name"

    @pytest.mark.parametrize("filename", NON_AGENT_EXAMPLES)
    def test_pipeline_nodes_have_id_and_ref(self, filename):
        """Every Pipeline node needs an id and a ref to an agent."""
        doc = load_example(filename)
        if kind_of(doc) != "Pipeline":
            pytest.skip(f"{filename} is not a Pipeline")

        for i, node in enumerate(doc["spec"]["agents"]):
            assert node.get("id"), f"{filename}: pipeline node {i} missing id"
            assert node.get("ref"), f"{filename}: pipeline node {i} missing ref"
